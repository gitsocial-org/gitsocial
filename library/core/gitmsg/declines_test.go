// declines_test.go - Published decline marker round-trip.
package gitmsg

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

func TestAddDecline(t *testing.T) {
	dir := t.TempDir()
	mustGit(t, dir, "init")
	mustGit(t, dir, "config", "user.email", "t@test.com")
	mustGit(t, dir, "config", "user.name", "T")

	ref := "https://github.com/bob/repo#commit:abc123def456@gitmsg/pm"
	if err := AddDecline(dir, ref); err != nil {
		t.Fatalf("AddDecline: %v", err)
	}
	out, err := git.ExecGit(dir, []string{"for-each-ref", "--format=%(contents:subject)", DeclinesRefPrefix})
	if err != nil {
		t.Fatalf("for-each-ref: %v", err)
	}
	if got := strings.TrimSpace(out.Stdout); got != ref {
		t.Errorf("published subject = %q, want %q", got, ref)
	}
	// Idempotent: re-publishing is a no-op (no error, no duplicate).
	if err := AddDecline(dir, ref); err != nil {
		t.Fatalf("re-AddDecline: %v", err)
	}
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := git.ExecGit(dir, args); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}
