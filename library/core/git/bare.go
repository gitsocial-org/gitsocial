// bare.go - Bare-repo lifecycle helpers (init, existence check) and a global
// git config reader. Used by code that needs to manage long-lived bare repos
// outside the workspace — e.g., the personal-repo backend (core/settings) and
// the memo extension's session repos.
package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BareRepoExists reports whether path contains a bare git repo, identified by
// a HEAD file at the root (not a directory).
func BareRepoExists(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(filepath.Join(path, "HEAD"))
	if err != nil {
		return false
	}
	return !st.IsDir()
}

// EnsureBareRepo creates a bare git repo at path if missing, seeding
// user.name / user.email from the global git config (or generic GitSocial
// defaults when the global config is unset). Idempotent.
func EnsureBareRepo(path string) error {
	if BareRepoExists(path) {
		return nil
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create bare repo dir: %w", err)
	}
	if _, err := ExecGit(path, []string{"init", "--bare", path}); err != nil {
		return fmt.Errorf("git init --bare %s: %w", path, err)
	}
	name := GlobalConfig("user.name")
	if name == "" {
		name = "GitSocial"
	}
	_, _ = ExecGit(path, []string{"config", "user.name", name})
	email := GlobalConfig("user.email")
	if email == "" {
		email = "gitsocial@local"
	}
	_, _ = ExecGit(path, []string{"config", "user.email", email})
	return nil
}

// GlobalConfig reads a key from the global git config. Returns "" when the
// key isn't set or the call fails.
func GlobalConfig(key string) string {
	out, err := ExecGit("", []string{"config", "--global", "--get", key})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out.Stdout)
}
