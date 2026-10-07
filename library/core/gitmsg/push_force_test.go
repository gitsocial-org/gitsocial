// push_force_test.go - Tests for the forced push of a remote that is a copy of the workspace
package gitmsg

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// rewriteExtBranch pushes two commits on gitmsg/social, then replaces the second locally, and returns the new tip.
func rewriteExtBranch(t *testing.T, work string) string {
	t.Helper()
	git.ExecGit(work, []string{"push", "origin", "main"})
	first, err := git.CreateCommitOnBranch(work, "gitmsg/social", "first")
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if _, err := git.CreateCommitOnBranch(work, "gitmsg/social", "second"); err != nil {
		t.Fatalf("second commit: %v", err)
	}
	if _, err := Push(work, false, nil, "origin", false); err != nil {
		t.Fatalf("first push: %v", err)
	}
	if _, err := git.ExecGit(work, []string{"update-ref", "refs/heads/gitmsg/social", first}); err != nil {
		t.Fatalf("rewind: %v", err)
	}
	if _, err := git.CreateCommitOnBranch(work, "gitmsg/social", "rewritten"); err != nil {
		t.Fatalf("rewritten commit: %v", err)
	}
	return localRef(t, work, "refs/heads/gitmsg/social")
}

// TestPush_forceWritesNoMerge: a forced push of a rewritten extension branch replaces the remote branch and leaves the workspace branch as it is.
func TestPush_forceWritesNoMerge(t *testing.T) {
	work, remote := setupWorkWithRemote(t)
	rewritten := rewriteExtBranch(t, work)
	if _, err := PushWithProgress(work, false, nil, "origin", false, true, nil); err != nil {
		t.Fatalf("forced push: %v", err)
	}
	if tip := remoteRef(t, remote, "refs/heads/gitmsg/social"); tip != rewritten {
		t.Errorf("remote gitmsg/social = %q, want the rewritten tip %s", tip, rewritten)
	}
	if tip := localRef(t, work, "refs/heads/gitmsg/social"); tip != rewritten {
		t.Errorf("workspace gitmsg/social = %q after a forced push, want %s with no merge", tip, rewritten)
	}
}

// TestPush_nonFastForwardStillRefused: without force, a rewritten code branch is refused and the remote branch stays.
func TestPush_nonFastForwardStillRefused(t *testing.T) {
	work, remote := setupWorkWithRemote(t)
	git.ExecGit(work, []string{"push", "origin", "main"})
	pushed := remoteRef(t, remote, "refs/heads/main")
	if _, err := git.ExecGit(work, []string{"commit", "--amend", "--allow-empty", "-m", "amended"}); err != nil {
		t.Fatalf("amend: %v", err)
	}
	if _, err := Push(work, false, map[string]int{"main": 1}, "origin", false); err == nil {
		t.Fatal("push of a rewritten branch without force succeeded, want a refusal")
	}
	if tip := remoteRef(t, remote, "refs/heads/main"); tip != pushed {
		t.Errorf("remote main = %q after a refused push, want %s", tip, pushed)
	}
	if _, err := PushWithProgress(work, false, map[string]int{"main": 1}, "origin", false, true, nil); err != nil {
		t.Fatalf("forced push: %v", err)
	}
	if tip := remoteRef(t, remote, "refs/heads/main"); tip != localRef(t, work, "refs/heads/main") {
		t.Errorf("remote main = %q after a forced push, want the amended tip", tip)
	}
}
