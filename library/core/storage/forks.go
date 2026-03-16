// forks.go - Isolated bare repos for cross-repository diffs
package storage

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gitsocial-org/gitsocial/core/git"
)

// EnsureForkRepository creates a minimal bare repo for diff operations.
// Lives under forks/ — not registered in cache DB, invisible to fetch pipeline.
// Remotes are added on-demand by callers (one per fork URL).
func EnsureForkRepository(cacheDir, repoURL string) (string, error) {
	name := urlToDirectoryName(repoURL)
	dir := filepath.Join(cacheDir, "forks", name)
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
