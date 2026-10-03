// worktree_test.go - The status of a working tree, the diff of one file, the index writes and the commit of the index
package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// worktreeFixture commits tracked.txt and returns the repository.
func worktreeFixture(t *testing.T) string {
	t.Helper()
	dir := initTestRepo(t)
	os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("one\n"), 0644)
	ExecGit(dir, []string{"add", "tracked.txt"})
	ExecGit(dir, []string{"commit", "-m", "tracked"})
	return dir
}

// statusOf returns the entry of a path, or false.
func statusOf(t *testing.T, dir, path string) (StatusEntry, bool) {
	t.Helper()
	entries, err := WorkingStatus(dir)
	if err != nil {
		t.Fatalf("WorkingStatus: %v", err)
	}
	for _, e := range entries {
		if e.Path == path {
			return e, true
		}
	}
	return StatusEntry{}, false
}

// TestWorkingStatus reads an untracked file, a staged new file, a file staged and changed again, and a staged rename.
func TestWorkingStatus(t *testing.T) {
	t.Parallel()
	dir := worktreeFixture(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0644)
	os.WriteFile(filepath.Join(dir, "added.txt"), []byte("added\n"), 0644)
	ExecGit(dir, []string{"add", "added.txt"})
	os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("one\ntwo\n"), 0644)
	ExecGit(dir, []string{"add", "tracked.txt"})
	os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("one\ntwo\nthree\n"), 0644)

	want := map[string][2]byte{"new.txt": {'?', '?'}, "added.txt": {'A', ' '}, "tracked.txt": {'M', 'M'}}
	for path, cols := range want {
		e, ok := statusOf(t, dir, path)
		if !ok || e.Index != cols[0] || e.Worktree != cols[1] {
			t.Errorf("%s = %+v, want columns %q%q", path, e, cols[0], cols[1])
		}
	}
	if e, _ := statusOf(t, dir, "new.txt"); !e.Untracked() || e.Staged() {
		t.Errorf("new.txt flags = staged %v untracked %v", e.Staged(), e.Untracked())
	}
	if e, _ := statusOf(t, dir, "tracked.txt"); !e.Staged() || !e.Unstaged() {
		t.Errorf("tracked.txt should be staged and unstaged: %+v", e)
	}
	ExecGit(dir, []string{"commit", "-am", "second"})
	ExecGit(dir, []string{"mv", "tracked.txt", "moved.txt"})
	if e, ok := statusOf(t, dir, "moved.txt"); !ok || e.Index != 'R' || e.OrigPath != "tracked.txt" {
		t.Errorf("rename = %+v, want R with the original path", e)
	}
}

// TestGetWorkingFileDiff_andUntracked reads the staged and the unstaged diff of one file and the content of an untracked one.
func TestGetWorkingFileDiff_andUntracked(t *testing.T) {
	t.Parallel()
	dir := worktreeFixture(t)
	os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("one\ntwo\n"), 0644)
	ExecGit(dir, []string{"add", "tracked.txt"})
	os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("one\ntwo\nthree\n"), 0644)
	staged, err := GetWorkingFileDiff(dir, "tracked.txt", true)
	if err != nil || staged == nil || len(staged.Hunks) != 1 || staged.Hunks[0].Lines[len(staged.Hunks[0].Lines)-1].Content != "two" {
		t.Fatalf("staged = %+v, %v, want the second line", staged, err)
	}
	unstaged, err := GetWorkingFileDiff(dir, "tracked.txt", false)
	if err != nil || unstaged == nil || unstaged.Hunks[0].Lines[len(unstaged.Hunks[0].Lines)-1].Content != "three" {
		t.Fatalf("unstaged = %+v, %v, want the third line", unstaged, err)
	}
	if none, err := GetWorkingFileDiff(dir, "missing.txt", false); err != nil || none != nil {
		t.Errorf("a path with no diff = %+v, %v, want nil", none, err)
	}
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("a\nb\n"), 0644)
	added, err := UntrackedFileDiff(dir, "new.txt")
	if err != nil || added.Status != DiffStatusAdded || len(added.Hunks) != 1 || len(added.Hunks[0].Lines) != 2 || added.Hunks[0].Lines[1].Type != LineAdded {
		t.Errorf("untracked = %+v, %v, want two added lines", added, err)
	}
	os.WriteFile(filepath.Join(dir, "bin"), []byte("a\x00b"), 0644)
	if binary, err := UntrackedFileDiff(dir, "bin"); err != nil || !binary.Binary {
		t.Errorf("binary = %+v, %v", binary, err)
	}
}

// TestStageAndUnstageFiles: s then u leaves the index as it was, for an untracked and for a tracked file; no path is no work.
func TestStageAndUnstageFiles(t *testing.T) {
	t.Parallel()
	dir := worktreeFixture(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0644)
	os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("one\ntwo\n"), 0644)
	if err := StageFiles(dir, []string{"new.txt", "tracked.txt"}); err != nil {
		t.Fatalf("StageFiles: %v", err)
	}
	if e, _ := statusOf(t, dir, "new.txt"); e.Index != 'A' {
		t.Errorf("new.txt after stage = %+v, want A", e)
	}
	if e, _ := statusOf(t, dir, "tracked.txt"); e.Index != 'M' || e.Worktree != ' ' {
		t.Errorf("tracked.txt after stage = %+v, want M in the index", e)
	}
	if err := UnstageFiles(dir, []string{"new.txt", "tracked.txt"}); err != nil {
		t.Fatalf("UnstageFiles: %v", err)
	}
	if e, _ := statusOf(t, dir, "new.txt"); !e.Untracked() {
		t.Errorf("new.txt after unstage = %+v, want untracked", e)
	}
	if e, _ := statusOf(t, dir, "tracked.txt"); e.Index != ' ' || e.Worktree != 'M' {
		t.Errorf("tracked.txt after unstage = %+v, want M in the working tree only", e)
	}
	if err := StageFiles(dir, nil); err != nil {
		t.Errorf("StageFiles(nil) = %v", err)
	}
	if err := UnstageFiles(dir, nil); err != nil {
		t.Errorf("UnstageFiles(nil) = %v", err)
	}
}

// TestCommitIndex commits the staged files and no other, and refuses an empty index.
func TestCommitIndex(t *testing.T) {
	t.Parallel()
	dir := worktreeFixture(t)
	if _, err := CommitIndex(dir, "nothing"); err != ErrNoChanges {
		t.Fatalf("CommitIndex on an empty index = %v, want ErrNoChanges", err)
	}
	os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("staged\n"), 0644)
	os.WriteFile(filepath.Join(dir, "loose.txt"), []byte("loose\n"), 0644)
	ExecGit(dir, []string{"add", "staged.txt"})
	hash, err := CommitIndex(dir, "Add staged\n\nOnly the index.")
	if err != nil || len(hash) != 12 {
		t.Fatalf("CommitIndex = %q, %v", hash, err)
	}
	files, _ := execGitSimple(dir, []string{"show", "--name-only", "--format=", "HEAD"})
	if strings.TrimSpace(files) != "staged.txt" {
		t.Errorf("HEAD files = %q, want staged.txt only", files)
	}
	if e, ok := statusOf(t, dir, "loose.txt"); !ok || !e.Untracked() {
		t.Errorf("loose.txt after the commit = %+v, want still untracked", e)
	}
	message, _ := GetCommitMessage(dir, "HEAD")
	if !strings.HasPrefix(message, "Add staged") {
		t.Errorf("HEAD message = %q", message)
	}
}
