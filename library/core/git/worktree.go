// worktree.go - The working tree of a workspace: its status, the diff of one file, the index writes and the commit of the index
package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StatusEntry is one row of git status: the path, the original path of a rename, and the index and working tree columns.
type StatusEntry struct {
	Path     string
	OrigPath string
	Index    byte
	Worktree byte
}

// Untracked reports whether git does not track the file.
func (e StatusEntry) Untracked() bool { return e.Index == '?' }

// Staged reports whether the index holds a change of the file.
func (e StatusEntry) Staged() bool { return e.Index != ' ' && e.Index != '?' }

// Unstaged reports whether the working tree differs from the index for a tracked file.
func (e StatusEntry) Unstaged() bool { return e.Worktree != ' ' && e.Worktree != '?' }

// WorkingStatus returns the changed and untracked files of the working tree, as git status lists them; the porcelain v2 format keeps a leading space of the index column, which the trimmed output of v1 would lose.
func WorkingStatus(workdir string) ([]StatusEntry, error) {
	output, err := execGitSimple(workdir, []string{"status", "--porcelain=v2", "-z", "--untracked-files=all"})
	if err != nil {
		return nil, fmt.Errorf("working status: %w", err)
	}
	var entries []StatusEntry
	tokens := strings.Split(output, "\x00")
	for i := 0; i < len(tokens); i++ {
		fields := strings.SplitN(tokens[i], " ", 9)
		switch {
		case fields[0] == "?" && len(fields) == 2:
			entries = append(entries, StatusEntry{Path: fields[1], Index: '?', Worktree: '?'})
		case fields[0] == "1" && len(fields) == 9:
			entries = append(entries, StatusEntry{Path: fields[8], Index: statusColumn(fields[1][0]), Worktree: statusColumn(fields[1][1])})
		case fields[0] == "2" && len(fields) == 9 && i+1 < len(tokens):
			// A rename or a copy: the path field carries a score before it, and the original path is the next record.
			pathFields := strings.SplitN(fields[8], " ", 2)
			i++
			entries = append(entries, StatusEntry{Path: pathFields[len(pathFields)-1], OrigPath: tokens[i], Index: statusColumn(fields[1][0]), Worktree: statusColumn(fields[1][1])})
		case fields[0] == "u" && len(fields) >= 2:
			// An unmerged path shows its two columns and waits for the resolution.
			entries = append(entries, StatusEntry{Path: fields[len(fields)-1], Index: statusColumn(fields[1][0]), Worktree: statusColumn(fields[1][1])})
		}
	}
	return entries, nil
}

// statusColumn maps the unchanged mark of porcelain v2 to the space of the short format.
func statusColumn(c byte) byte {
	if c == '.' {
		return ' '
	}
	return c
}

// GetWorkingFileDiff returns the diff of one file, the working tree against the index, or the index against HEAD when staged is set; nil when the file has no such diff.
func GetWorkingFileDiff(workdir, path string, staged bool) (*FileDiff, error) {
	args := []string{"diff", "--no-color", "--unified=3"}
	if staged {
		args = append(args, "--cached")
	}
	output, err := execGitSimple(workdir, append(args, "--", path))
	if err != nil {
		return nil, fmt.Errorf("get working file diff: %w", err)
	}
	diffs := parseDiff(output)
	if len(diffs) == 0 {
		return nil, nil
	}
	return &diffs[0], nil
}

// UntrackedFileDiff reads an untracked file as a diff that adds every line of it; a binary file gives a binary diff.
func UntrackedFileDiff(workdir, path string) (*FileDiff, error) {
	data, err := os.ReadFile(filepath.Join(workdir, path))
	if err != nil {
		return nil, fmt.Errorf("read untracked file: %w", err)
	}
	fileDiff := &FileDiff{OldPath: "/dev/null", NewPath: path, Status: DiffStatusAdded}
	if IsBinaryContent(string(data)) {
		fileDiff.Binary = true
		return fileDiff, nil
	}
	content := strings.TrimSuffix(string(data), "\n")
	if content == "" {
		return fileDiff, nil
	}
	lines := strings.Split(content, "\n")
	hunk := Hunk{NewStart: 1, NewCount: len(lines), Header: fmt.Sprintf("@@ -0,0 +1,%d @@", len(lines))}
	for i, line := range lines {
		hunk.Lines = append(hunk.Lines, DiffLine{Type: LineAdded, Content: line, NewNum: i + 1})
	}
	fileDiff.Hunks = []Hunk{hunk}
	return fileDiff, nil
}

// StageFiles adds the paths to the index, untracked files included; no path is no work.
func StageFiles(workdir string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	if _, err := ExecGit(workdir, append([]string{"add", "--"}, paths...)); err != nil {
		return fmt.Errorf("stage: %w", err)
	}
	return nil
}

// UnstageFiles restores the paths in the index from HEAD, which takes a new file out of it; no path is no work.
func UnstageFiles(workdir string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	if _, err := ExecGit(workdir, append([]string{"restore", "--staged", "--"}, paths...)); err != nil {
		return fmt.Errorf("unstage: %w", err)
	}
	return nil
}

// CommitIndex commits the index as it is, with no add, and returns the short hash; an index equal to HEAD is ErrNoChanges.
func CommitIndex(workdir, message string) (string, error) {
	if _, err := ExecGit(workdir, []string{"diff", "--cached", "--quiet"}); err == nil {
		return "", ErrNoChanges
	} else {
		var gitErr *GitError
		if !errors.As(err, &gitErr) || gitErr.Code != 1 {
			return "", fmt.Errorf("read the index: %w", err)
		}
	}
	if _, err := ExecGit(workdir, []string{"commit", "-m", message}); err != nil {
		return "", err
	}
	hash, err := execGitSimple(workdir, []string{"rev-parse", "--short=12", "HEAD"})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(hash), nil
}
