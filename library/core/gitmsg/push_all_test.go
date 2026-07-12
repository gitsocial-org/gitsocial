// push_all_test.go - Tests for --all (wholesale branch publish) and empty-remote
// (first-publish bootstrap) detection.
package gitmsg

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// setupWorkWithRemote creates a working repo with one commit on main and an
// origin bare remote. Returns the work dir and the bare remote path.
func setupWorkWithRemote(t *testing.T) (work, remote string) {
	t.Helper()
	remote = t.TempDir()
	if err := git.EnsureBareRepo(remote); err != nil {
		t.Fatalf("init remote: %v", err)
	}
	work = t.TempDir()
	if err := git.Init(work, "main"); err != nil {
		t.Fatalf("init work: %v", err)
	}
	for _, kv := range [][2]string{{"user.name", "Tester"}, {"user.email", "t@example.com"}} {
		git.ExecGit(work, []string{"config", kv[0], kv[1]})
	}
	git.CreateCommit(work, git.CommitOptions{Message: "init", AllowEmpty: true})
	if _, err := git.ExecGit(work, []string{"remote", "add", "origin", remote}); err != nil {
		t.Fatalf("add origin: %v", err)
	}
	return work, remote
}

// TestGetPushPreview_allListsExtraBranches: with --all, every local non-gitmsg
// branch beyond the reason-based code set appears in preview.All. Without --all
// it stays empty.
func TestGetPushPreview_allListsExtraBranches(t *testing.T) {
	work, _ := setupWorkWithRemote(t)
	// Two WIP branches with no PR — never in the reason-based set.
	for _, b := range []string{"wip/experiment", "feature/x"} {
		if _, err := git.ExecGit(work, []string{"branch", b}); err != nil {
			t.Fatalf("branch %s: %v", b, err)
		}
	}

	preview, err := GetPushPreview(work, nil, "origin", false)
	if err != nil {
		t.Fatalf("preview (no --all): %v", err)
	}
	if len(preview.All) != 0 {
		t.Errorf("preview.All without --all = %v, want empty", preview.All)
	}

	preview, err = GetPushPreview(work, nil, "origin", true)
	if err != nil {
		t.Fatalf("preview (--all): %v", err)
	}
	got := map[string]bool{}
	for _, b := range preview.All {
		got[b] = true
	}
	for _, want := range []string{"main", "wip/experiment", "feature/x"} {
		if !got[want] {
			t.Errorf("preview.All = %v, missing %q", preview.All, want)
		}
	}
}

// TestGetPushPreview_allExcludesCodeBranches: a branch already in the
// reason-based code set is not duplicated into preview.All.
func TestGetPushPreview_allExcludesCodeBranches(t *testing.T) {
	work, _ := setupWorkWithRemote(t)
	if _, err := git.ExecGit(work, []string{"branch", "feature/pr-head"}); err != nil {
		t.Fatalf("branch: %v", err)
	}

	preview, err := GetPushPreview(work, map[string]int{"feature/pr-head": 3}, "origin", true)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	for _, b := range preview.All {
		if b == "feature/pr-head" {
			t.Errorf("preview.All contains code branch %q, want it excluded", b)
		}
	}
}

// TestPush_allPushesExtraBranches: --all publishes WIP branches that a normal
// push (reason-based) would never touch, and reports the count.
func TestPush_allPushesExtraBranches(t *testing.T) {
	work, remote := setupWorkWithRemote(t)
	for _, b := range []string{"wip/experiment", "feature/x"} {
		if _, err := git.ExecGit(work, []string{"branch", b}); err != nil {
			t.Fatalf("branch %s: %v", b, err)
		}
	}

	// Establish the remote (a tags-only push to a truly empty bare remote errors
	// in git), then a normal push (no --all, no code branches) must NOT publish
	// the WIP branches.
	git.ExecGit(work, []string{"push", "origin", "main"})
	if _, err := Push(work, false, nil, "origin", false); err != nil {
		t.Fatalf("normal push: %v", err)
	}
	if tip := remoteRef(t, remote, "refs/heads/wip/experiment"); tip != "" {
		t.Errorf("normal push published wip/experiment (%q), want it skipped", tip)
	}

	// --all publishes every local branch.
	result, err := Push(work, false, nil, "origin", true)
	if err != nil {
		t.Fatalf("--all push: %v", err)
	}
	if result.AllBranches == 0 {
		t.Errorf("result.AllBranches = 0, want > 0")
	}
	for _, b := range []string{"main", "wip/experiment", "feature/x"} {
		if tip := remoteRef(t, remote, "refs/heads/"+b); tip == "" {
			t.Errorf("--all did not publish %q", b)
		}
	}
}

// TestRemoteIsEmpty detects the first-publish (bootstrap) state: a bare remote
// with zero refs is empty; after a push it is not.
func TestRemoteIsEmpty(t *testing.T) {
	work, _ := setupWorkWithRemote(t)

	if !RemoteIsEmpty(work, "origin") {
		t.Error("fresh bare remote should report empty")
	}

	git.ExecGit(work, []string{"push", "origin", "main"})
	if RemoteIsEmpty(work, "origin") {
		t.Error("remote with a pushed branch should report non-empty")
	}

	// A remote name that isn't configured is treated as non-empty (bad probe
	// must never trigger bootstrap).
	if RemoteIsEmpty(work, "ghost") {
		t.Error("nonexistent remote should not report empty")
	}
}
