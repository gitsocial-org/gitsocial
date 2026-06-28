// fork_pr_lifecycle_test.go - Two-repo (distinct repo_url) fork-PR lifecycle.
// A base owner closing a fork PR adopts it onto their own review branch (a
// self-contained, same-repo record) and collapses the fork original in the PR
// list, while the fork's own canonical is left untouched — it reconciles later
// via the role/acceptance machinery, not by the base owner writing to it.
package review

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

func TestForkPRClose_adoptsAndCollapses(t *testing.T) {
	setupTestDB(t)

	upstreamOrigin := initBareOrigin(t)
	forkOrigin := initBareOrigin(t)
	alice := cloneAs(t, upstreamOrigin, "alice", "alice@test.com")
	bob := cloneAs(t, forkOrigin, "bob", "bob@test.com")

	upstreamURL := gitmsg.ResolveRepoURL(alice)
	forkURL := gitmsg.ResolveRepoURL(bob)
	if upstreamURL == forkURL {
		t.Fatalf("upstream and fork must have distinct repo_urls: %q", upstreamURL)
	}

	// Bob pushes a feature branch and opens a PR targeting the upstream.
	if _, err := git.ExecGit(bob, []string{"checkout", "-b", "feature"}); err != nil {
		t.Fatalf("bob checkout: %v", err)
	}
	if _, err := git.CreateCommit(bob, git.CommitOptions{Message: "bob feature", AllowEmpty: true}); err != nil {
		t.Fatalf("bob commit: %v", err)
	}
	if _, err := git.ExecGit(bob, []string{"push", "origin", "feature"}); err != nil {
		t.Fatalf("bob push: %v", err)
	}
	if _, err := git.ExecGit(bob, []string{"fetch", "origin"}); err != nil {
		t.Fatalf("bob fetch: %v", err)
	}
	created := CreatePR(bob, "Fix the bug", "", CreatePROptions{
		Base: upstreamURL + "#branch:main",
		Head: "feature",
	})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}
	if created.Data.Repository != forkURL {
		t.Fatalf("PR should live on the fork: got %q want %q", created.Data.Repository, forkURL)
	}

	branch := gitmsg.GetExtBranch(alice, "review")

	// Before close: Alice sees one open fork PR.
	before := GetPullRequestsWithForks(upstreamURL, branch, []string{forkURL}, nil, "", 0)
	if !before.Success {
		t.Fatalf("GetPullRequestsWithForks (before): %s", before.Error.Message)
	}
	if len(before.Data) != 1 || before.Data[0].State != PRStateOpen || before.Data[0].Repository != forkURL {
		t.Fatalf("before close: want 1 open PR on fork, got %+v", before.Data)
	}

	// Alice closes the fork PR: it is adopted onto her own review branch.
	closed := ClosePR(alice, created.Data.ID)
	if !closed.Success {
		t.Fatalf("ClosePR: %s", closed.Error.Message)
	}
	if closed.Data.State != PRStateClosed {
		t.Errorf("closed PR state = %q, want closed", closed.Data.State)
	}
	if closed.Data.Repository != upstreamURL {
		t.Errorf("closed record should be adopted onto upstream: got %q want %q", closed.Data.Repository, upstreamURL)
	}

	// After close: still one PR — the adopted copy (closed), with the fork
	// original collapsed into it.
	after := GetPullRequestsWithForks(upstreamURL, branch, []string{forkURL}, nil, "", 0)
	if !after.Success {
		t.Fatalf("GetPullRequestsWithForks (after): %s", after.Error.Message)
	}
	if len(after.Data) != 1 {
		t.Fatalf("after close: want 1 PR (original collapsed into adopted copy), got %d: %+v", len(after.Data), after.Data)
	}
	if after.Data[0].State != PRStateClosed || after.Data[0].Repository != upstreamURL {
		t.Errorf("after close: want closed PR on upstream, got state=%q repo=%q", after.Data[0].State, after.Data[0].Repository)
	}

	// The adopted copy links back to the fork original (author identity preserved).
	item, err := GetReviewItemByRef(after.Data[0].ID, upstreamURL)
	if err != nil {
		t.Fatalf("GetReviewItemByRef adopted copy: %v", err)
	}
	linked := false
	for _, ref := range item.References {
		if protocol.ParseRef(ref.Ref).Repository == forkURL {
			linked = true
		}
	}
	if !linked {
		t.Error("adopted copy should carry a GitMsg-Ref back to the fork original")
	}

	// Reciprocal: the fork's own canonical is untouched (still open). Gating keeps
	// Alice's close self-contained; the fork reconciles via role/acceptance.
	forkCanonical := GetPR(created.Data.ID)
	if !forkCanonical.Success {
		t.Fatalf("GetPR fork canonical: %s", forkCanonical.Error.Message)
	}
	if forkCanonical.Data.State != PRStateOpen {
		t.Errorf("fork canonical state = %q, want still open", forkCanonical.Data.State)
	}
}

// TestForkPRReadiness_authorOnly asserts that draft/mark-ready/retract on a fork
// PR are rejected for a base owner — readiness and withdrawal are the author's.
func TestForkPRReadiness_authorOnly(t *testing.T) {
	setupTestDB(t)

	upstreamOrigin := initBareOrigin(t)
	forkOrigin := initBareOrigin(t)
	alice := cloneAs(t, upstreamOrigin, "alice", "alice@test.com")
	bob := cloneAs(t, forkOrigin, "bob", "bob@test.com")

	upstreamURL := gitmsg.ResolveRepoURL(alice)

	if _, err := git.ExecGit(bob, []string{"checkout", "-b", "feature"}); err != nil {
		t.Fatalf("bob checkout: %v", err)
	}
	if _, err := git.CreateCommit(bob, git.CommitOptions{Message: "bob feature", AllowEmpty: true}); err != nil {
		t.Fatalf("bob commit: %v", err)
	}
	if _, err := git.ExecGit(bob, []string{"push", "origin", "feature"}); err != nil {
		t.Fatalf("bob push: %v", err)
	}
	if _, err := git.ExecGit(bob, []string{"fetch", "origin"}); err != nil {
		t.Fatalf("bob fetch: %v", err)
	}
	created := CreatePR(bob, "WIP fix", "", CreatePROptions{
		Base:  upstreamURL + "#branch:main",
		Head:  "feature",
		Draft: true,
	})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}

	if res := MarkReady(alice, created.Data.ID); res.Success || res.Error.Code != "NOT_AUTHOR" {
		t.Errorf("MarkReady on fork PR: want NOT_AUTHOR, got success=%v code=%q", res.Success, res.Error.Code)
	}
	if res := ConvertToDraft(alice, created.Data.ID); res.Success || res.Error.Code != "NOT_AUTHOR" {
		t.Errorf("ConvertToDraft on fork PR: want NOT_AUTHOR, got success=%v code=%q", res.Success, res.Error.Code)
	}
	if res := RetractPR(alice, created.Data.ID); res.Success || res.Error.Code != "NOT_AUTHOR" {
		t.Errorf("RetractPR on fork PR: want NOT_AUTHOR, got success=%v code=%q", res.Success, res.Error.Code)
	}
}
