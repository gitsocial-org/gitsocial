// push_remote_test.go - Tests that Push honors an explicit remote parameter.
package gitmsg

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// TestPush_explicitRemoteWins sets up a workspace with two s3-shaped remotes
// (the heuristic would prefer "origin") plus a configured gitsocial.pushRemote,
// then pushes with an explicit remote name. The explicit param must win over
// both: gitmsg/* branches land on the named remote and the tracking mirror is
// written under refs/remotes/<name>/gitmsg/*, not the heuristic's or config's.
func TestPush_explicitRemoteWins(t *testing.T) {
	originRemote := t.TempDir()
	backupRemote := t.TempDir()
	for _, r := range []string{originRemote, backupRemote} {
		if err := git.EnsureBareRepo(r); err != nil {
			t.Fatalf("init remote: %v", err)
		}
	}

	work := t.TempDir()
	if err := git.Init(work, "main"); err != nil {
		t.Fatalf("init work: %v", err)
	}
	for _, kv := range [][2]string{{"user.name", "Tester"}, {"user.email", "t@example.com"}} {
		git.ExecGit(work, []string{"config", kv[0], kv[1]})
	}
	git.CreateCommit(work, git.CommitOptions{Message: "init", AllowEmpty: true})
	if _, err := git.ExecGit(work, []string{"remote", "add", "origin", originRemote}); err != nil {
		t.Fatalf("add origin: %v", err)
	}
	if _, err := git.ExecGit(work, []string{"remote", "add", "backup", backupRemote}); err != nil {
		t.Fatalf("add backup: %v", err)
	}
	// Configure a different remote so we prove the explicit param beats config too.
	git.ExecGit(work, []string{"config", "gitsocial.pushRemote", "origin"})
	git.ExecGit(work, []string{"push", "origin", "main"})
	git.ExecGit(work, []string{"push", "backup", "main"})

	// Create a gitmsg/social commit and a list ref (a refs/gitmsg/* state ref).
	if _, err := git.CreateCommitOnBranch(work, "gitmsg/social", "a post"); err != nil {
		t.Fatalf("commit on branch: %v", err)
	}
	socialTip := fullHash(work, "refs/heads/gitmsg/social")
	// A per-element state ref under refs/gitmsg/core/* (does not collide with any
	// pushed branch name, so its remote-tracking mirror can be written).
	stateRef := "refs/gitmsg/core/forks/deadbeef"
	if err := git.WriteRef(work, stateRef, socialTip); err != nil {
		t.Fatalf("write state ref: %v", err)
	}

	result, err := Push(work, false, nil, "backup", false)
	if err != nil {
		t.Fatalf("Push(backup): %v", err)
	}
	if result.Remote != "backup" {
		t.Errorf("result.Remote = %q, want backup", result.Remote)
	}
	if result.RemoteURL != backupRemote {
		t.Errorf("result.RemoteURL = %q, want %q", result.RemoteURL, backupRemote)
	}

	// gitmsg/social must have landed on backup, NOT origin.
	if tip := remoteRef(t, backupRemote, "refs/heads/gitmsg/social"); tip != socialTip {
		t.Errorf("backup gitmsg/social = %q, want %q", tip, socialTip)
	}
	if tip := remoteRef(t, originRemote, "refs/heads/gitmsg/social"); tip != "" {
		t.Errorf("origin gitmsg/social = %q, want empty (nothing pushed to origin)", tip)
	}

	// State refs must have landed on backup.
	if tip := remoteRef(t, backupRemote, stateRef); tip != socialTip {
		t.Errorf("backup state ref = %q, want %q", tip, socialTip)
	}

	// The tracking mirror must be written under refs/remotes/backup/gitmsg/*,
	// not refs/remotes/origin/gitmsg/*.
	if tip := localRef(t, work, "refs/remotes/backup/gitmsg/core/forks/deadbeef"); tip != socialTip {
		t.Errorf("tracking mirror under backup = %q, want %q", tip, socialTip)
	}
	if tip := localRef(t, work, "refs/remotes/origin/gitmsg/core/forks/deadbeef"); tip != "" {
		t.Errorf("tracking mirror under origin = %q, want empty", tip)
	}
}

// remoteRef reads a full ref hash from a bare repo, or "" if absent.
func remoteRef(t *testing.T, bareRepo, ref string) string {
	t.Helper()
	out, err := git.ExecGit(bareRepo, []string{"rev-parse", "--verify", "--quiet", ref})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out.Stdout)
}

// localRef reads a full ref hash from a working repo, or "" if absent.
func localRef(t *testing.T, workdir, ref string) string {
	t.Helper()
	out, err := git.ExecGit(workdir, []string{"rev-parse", "--verify", "--quiet", ref})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out.Stdout)
}
