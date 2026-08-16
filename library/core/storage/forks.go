// forks.go - Isolated bare repos for cross-repository diffs
package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// missingObjectPhrases are git's wordings for an object the repo cannot read:
// the donor workspace moved or gc'd out from under a borrowed fork repo, or the
// fork repo's own objects were pruned.
var missingObjectPhrases = []string{
	"bad object",
	"missing object",
	"nonexistent object",
	"could not get object info",
	"unable to normalize alternate object path",
}

// forkRepositoryDir returns the fork network repo path for a base repo URL.
func forkRepositoryDir(cacheDir, repoURL string) string {
	return filepath.Join(cacheDir, "forks", urlToDirectoryName(repoURL))
}

// EnsureForkRepository creates a minimal bare repo for diff operations.
// Lives under forks/ — not registered in cache DB, invisible to fetch pipeline.
// Remotes are added on-demand by callers (one per fork URL).
func EnsureForkRepository(cacheDir, repoURL string) (string, error) {
	dir := forkRepositoryDir(cacheDir, repoURL)
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create fork dir: %w", err)
	}
	if _, err := git.ExecGit(dir, []string{"init", "--bare"}); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("init fork repo: %w", err)
	}
	return dir, nil
}

// RepairForkRepository deletes a fork repo and re-creates it empty. The fork repo
// borrows objects from the workspace, so a donor that moved or gc'd leaves it
// naming objects nobody has; the borrower is a disposable cache, so rebuilding is
// the repair rather than preventing the borrow in the first place.
func RepairForkRepository(cacheDir, repoURL string) (string, error) {
	if err := os.RemoveAll(forkRepositoryDir(cacheDir, repoURL)); err != nil {
		return "", fmt.Errorf("remove fork repo: %w", err)
	}
	return EnsureForkRepository(cacheDir, repoURL)
}

// SetAlternate lends a workspace's object database to a fork repo, so cross-repo
// diffs read objects the workspace already holds instead of fetching a second
// copy of them. Borrowing is one-directional: only the fork repo ever reads the
// workspace, never the reverse. A blobless workspace cannot lend (an alternate
// supplies objects, not lazy fetching, so the borrower would hit missing blobs no
// rebuild can fix), so it is a no-op that drops any alternate written earlier.
func SetAlternate(forkDir, workdir string) error {
	donor, err := objectsDir(workdir)
	if err != nil {
		return err
	}
	entries := make([]string, 0, 2)
	for _, existing := range readAlternates(forkDir) {
		// Drop this donor's own entry (rewritten below) and donors that no longer
		// exist, which git reports on every read in the fork repo.
		if existing == donor {
			continue
		}
		if _, statErr := os.Stat(existing); statErr != nil {
			continue
		}
		entries = append(entries, existing)
	}
	if !IsPartialClone(workdir) {
		entries = append(entries, donor)
	}
	return writeAlternates(forkDir, entries)
}

// IsPartialClone reports whether a repo fetches objects lazily from a promisor
// remote, which disqualifies it as an alternates donor.
func IsPartialClone(workdir string) bool {
	result, err := git.ExecGit(workdir, []string{"config", "--get-regexp", `^remote\..*\.partialclonefilter$`})
	if err == nil && strings.TrimSpace(result.Stdout) != "" {
		return true
	}
	return git.GetGitConfig(workdir, "extensions.partialClone") != ""
}

// IsMissingObjectError reports whether a git failure names a missing or bad
// object, the signal that a fork repo needs rebuilding rather than retrying.
func IsMissingObjectError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, phrase := range missingObjectPhrases {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

// objectsDir resolves a repo's object database as an absolute path. The donor is
// the user's workspace, which does not move in lockstep with the cache, so a
// relative path would be wrong the moment either side is relocated.
func objectsDir(workdir string) (string, error) {
	result, err := git.ExecGit(workdir, []string{"rev-parse", "--git-path", "objects"})
	if err != nil {
		return "", fmt.Errorf("resolve objects dir: %w", err)
	}
	path := strings.TrimSpace(result.Stdout)
	if path == "" {
		return "", fmt.Errorf("resolve objects dir: empty path for %q", workdir)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workdir, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("absolute objects dir: %w", err)
	}
	return abs, nil
}

// readAlternates returns the donor object dirs a fork repo currently borrows from.
func readAlternates(forkDir string) []string {
	data, err := os.ReadFile(alternatesPath(forkDir))
	if err != nil {
		return nil
	}
	var entries []string
	for _, line := range strings.Split(string(data), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			entries = append(entries, trimmed)
		}
	}
	return entries
}

// writeAlternates records the donor object dirs, writing only when they differ.
func writeAlternates(forkDir string, entries []string) error {
	path := alternatesPath(forkDir)
	if len(entries) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove alternates: %w", err)
		}
		return nil
	}
	content := strings.Join(entries, "\n") + "\n"
	if current, err := os.ReadFile(path); err == nil && string(current) == content {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create alternates dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("write alternates: %w", err)
	}
	return nil
}

// alternatesPath returns the alternates file location inside a bare fork repo.
func alternatesPath(forkDir string) string {
	return filepath.Join(forkDir, "objects", "info", "alternates")
}
