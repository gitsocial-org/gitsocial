// testutil.go - Shared test fixtures: temp cache lifecycle and clonable git repos.
//
// Placement: this package sits at library/internal rather than under
// library/core because Go's internal rule would otherwise hide it from
// library/extensions. It imports core packages only, so it never pulls an
// extension into a core test binary and adds no layer inversion. The two
// packages it cannot serve are core/cache and core/git themselves, whose
// in-package tests would form an import cycle; those keep local helpers.
package testutil

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// OpenTempCache points the process cache at a fresh temp directory for one
// test. At cleanup it closes that cache and reopens restoreDir, which is the
// suite-wide cache a package TestMain opened; pass "" to leave it closed.
func OpenTempCache(t *testing.T, restoreDir string) {
	t.Helper()
	cache.Reset()
	if err := cache.Open(t.TempDir()); err != nil {
		t.Fatalf("cache.Open() error = %v", err)
	}
	t.Cleanup(func() {
		cache.Reset()
		if restoreDir != "" {
			_ = cache.Open(restoreDir)
		}
	})
}

// NewRepoTemplate creates a git repo in a fresh temp directory with a test
// identity and one empty commit on `main`. Package TestMains build one and hand
// it to CopyRepo per test; the caller removes the directory when the suite ends.
func NewRepoTemplate() (string, error) {
	dir, err := os.MkdirTemp("", "gitsocial-test-repo-*")
	if err != nil {
		return "", err
	}
	if err := git.Init(dir, "main"); err != nil {
		return "", err
	}
	if _, err := git.ExecGit(dir, []string{"config", "user.email", "test@test.com"}); err != nil {
		return "", err
	}
	if _, err := git.ExecGit(dir, []string{"config", "user.name", "Test User"}); err != nil {
		return "", err
	}
	if _, err := git.CreateCommit(dir, git.CommitOptions{Message: "Initial commit", AllowEmpty: true}); err != nil {
		return "", err
	}
	return dir, nil
}

// CopyRepo copies a template repo into a fresh t.TempDir() and returns it.
// Copying is much cheaper than re-running git init for every test.
func CopyRepo(t *testing.T, template string) string {
	t.Helper()
	dst := t.TempDir()
	if err := copyTree(template, dst); err != nil {
		t.Fatalf("CopyRepo(%s): %v", template, err)
	}
	return dst
}

// copyTree recursively copies the contents of src into dst.
func copyTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				return err
			}
			if err := copyTree(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies one regular file, preserving its mode.
func copyFile(srcPath, dstPath string) error {
	info, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	dstFile, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()
	_, err = io.Copy(dstFile, srcFile)
	return err
}
