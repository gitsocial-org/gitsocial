// fork_pr_lifecycle_test.go - Two-repo (distinct repo_url) fork-PR lifecycle.
// A base owner closing a fork PR adopts it onto their own review branch (a
// self-contained, same-repo record) and collapses the fork original in the PR
// list, while the fork's own canonical is left untouched — it reconciles later
// via the role/acceptance machinery, not by the base owner writing to it.
package review

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// pushBranch creates a branch with one commit, pushes it to origin and refreshes tracking refs.
func pushBranch(t *testing.T, workdir, name string) {
	t.Helper()
	if _, err := git.ExecGit(workdir, []string{"checkout", "-b", name}); err != nil {
		t.Fatalf("checkout %s: %v", name, err)
	}
	if _, err := git.CreateCommit(workdir, git.CommitOptions{Message: name + " work", AllowEmpty: true}); err != nil {
		t.Fatalf("commit on %s: %v", name, err)
	}
	if _, err := git.ExecGit(workdir, []string{"push", "origin", name}); err != nil {
		t.Fatalf("push %s: %v", name, err)
	}
	if _, err := git.ExecGit(workdir, []string{"fetch", "origin"}); err != nil {
		t.Fatalf("fetch after pushing %s: %v", name, err)
	}
}

// forkPRFixture builds an upstream clone and a fork clone, sharing history, whose `feature` branch is pushed.
func forkPRFixture(t *testing.T) (upstream, fork, upstreamURL, forkURL string) {
	t.Helper()
	upstreamOrigin := initBareOrigin(t)
	forkOrigin := t.TempDir()
	if _, err := git.ExecGit(forkOrigin, []string{"clone", "--bare", upstreamOrigin, "."}); err != nil {
		t.Fatalf("clone the fork origin: %v", err)
	}
	upstream = cloneAs(t, upstreamOrigin, "alice", "alice@test.com")
	fork = cloneAs(t, forkOrigin, "bob", "bob@test.com")
	upstreamURL = gitmsg.ResolveRepoURL(upstream)
	forkURL = gitmsg.ResolveRepoURL(fork)
	if upstreamURL == forkURL {
		t.Fatalf("upstream and fork must have distinct repo_urls: %q", upstreamURL)
	}
	pushBranch(t, fork, "feature")
	return upstream, fork, upstreamURL, forkURL
}

func TestForkPRClose_adoptsAndCollapses(t *testing.T) {
	setupTestDB(t)

	alice, bob, upstreamURL, forkURL := forkPRFixture(t)

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

	alice, bob, upstreamURL, _ := forkPRFixture(t)

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

// TestForkPRMerge_relativeHead merges a fork PR written in the relative form the
// fork-discovery flow shows: the head names the fork, not the upstream.
func TestForkPRMerge_relativeHead(t *testing.T) {
	setupTestDB(t)
	alice, bob, upstreamURL, forkURL := forkPRFixture(t)

	// The upstream has a branch of the same name, which must not be merged.
	pushBranch(t, alice, "feature")
	if _, err := git.ExecGit(alice, []string{"checkout", "main"}); err != nil {
		t.Fatalf("alice checkout main: %v", err)
	}
	decoyTip, err := git.ReadRef(alice, "feature")
	if err != nil {
		t.Fatalf("read the upstream feature tip: %v", err)
	}
	forkTip, err := git.ReadRef(bob, "feature")
	if err != nil {
		t.Fatalf("read the fork feature tip: %v", err)
	}

	created := CreatePR(bob, "Fix the bug", "", CreatePROptions{
		Base: "#branch:main",
		Head: "feature",
	})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}
	if created.Data.Head != "#branch:feature" {
		t.Fatalf("the fork PR should keep a relative head, got %q", created.Data.Head)
	}

	merged := MergePR(alice, created.Data.ID, MergeStrategyFF)
	if !merged.Success {
		t.Fatalf("MergePR: %s", merged.Error.Message)
	}
	if merged.Data.Repository != upstreamURL {
		t.Errorf("merge record repository = %q, want %q", merged.Data.Repository, upstreamURL)
	}
	if got := protocol.ParseRef(merged.Data.Head).Repository; got != forkURL {
		t.Errorf("homed copy head = %q, want a head naming %s", merged.Data.Head, forkURL)
	}
	mainTip, err := git.ReadRef(alice, "main")
	if err != nil {
		t.Fatalf("read the upstream main tip: %v", err)
	}
	if mainTip != forkTip {
		t.Errorf("upstream main = %s, want the fork's feature tip %s", mainTip, forkTip)
	}
	if mainTip == decoyTip {
		t.Error("upstream main took the upstream's own feature branch")
	}
}

// TestForkPRClose_relativeHead closes a fork PR whose head is relative.
func TestForkPRClose_relativeHead(t *testing.T) {
	setupTestDB(t)
	alice, bob, upstreamURL, forkURL := forkPRFixture(t)

	created := CreatePR(bob, "Fix the bug", "", CreatePROptions{Base: "#branch:main", Head: "feature"})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}
	closed := ClosePR(alice, created.Data.ID)
	if !closed.Success {
		t.Fatalf("ClosePR: %s", closed.Error.Message)
	}
	if closed.Data.State != PRStateClosed || closed.Data.Repository != upstreamURL {
		t.Errorf("closed record = state %q repo %q, want closed on %s", closed.Data.State, closed.Data.Repository, upstreamURL)
	}
	if got := protocol.ParseRef(closed.Data.Head).Repository; got != forkURL {
		t.Errorf("homed copy head = %q, want a head naming %s", closed.Data.Head, forkURL)
	}
}

// TestCountPullRequests_matchesList counts only the repository the list shows.
func TestCountPullRequests_matchesList(t *testing.T) {
	setupTestDB(t)
	alice, bob, upstreamURL, _ := forkPRFixture(t)

	pushBranch(t, alice, "feature")
	if _, err := git.ExecGit(alice, []string{"checkout", "main"}); err != nil {
		t.Fatalf("alice checkout main: %v", err)
	}
	if res := CreatePR(alice, "Upstream work", "", CreatePROptions{Base: "#branch:main", Head: "feature"}); !res.Success {
		t.Fatalf("CreatePR upstream: %s", res.Error.Message)
	}
	for _, subject := range []string{"Fork one", "Fork two"} {
		if res := CreatePR(bob, subject, "", CreatePROptions{Base: "#branch:main", Head: "feature"}); !res.Success {
			t.Fatalf("CreatePR %s: %s", subject, res.Error.Message)
		}
	}

	branch := gitmsg.GetExtBranch(alice, "review")
	listed := GetPullRequests(upstreamURL, branch, []string{"open"}, "", 0)
	if !listed.Success {
		t.Fatalf("GetPullRequests: %s", listed.Error.Message)
	}
	count, err := CountPullRequests(upstreamURL, []string{"open"})
	if err != nil {
		t.Fatalf("CountPullRequests: %v", err)
	}
	if count != len(listed.Data) {
		t.Errorf("count = %d, want %d, the length of the list it totals", count, len(listed.Data))
	}
	if count != 1 {
		t.Errorf("count = %d, want 1, the workspace's own pull request", count)
	}
}

// TestSyncPRBranch_forkHeadRefused keeps `pr sync` off a branch the workspace does not own.
func TestSyncPRBranch_forkHeadRefused(t *testing.T) {
	setupTestDB(t)
	alice, bob, _, _ := forkPRFixture(t)

	pushBranch(t, alice, "feature")
	if _, err := git.ExecGit(alice, []string{"checkout", "main"}); err != nil {
		t.Fatalf("alice checkout main: %v", err)
	}
	before, err := git.ReadRef(alice, "feature")
	if err != nil {
		t.Fatalf("read the upstream feature tip: %v", err)
	}

	created := CreatePR(bob, "Fix the bug", "", CreatePROptions{Base: "#branch:main", Head: "feature"})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}

	res := SyncPRBranch(alice, created.Data.ID, "rebase")
	if res.Success || res.Error.Code != "INVALID_TARGET" {
		t.Fatalf("SyncPRBranch on a fork head: want INVALID_TARGET, got success=%v code=%q", res.Success, res.Error.Code)
	}
	after, err := git.ReadRef(alice, "feature")
	if err != nil {
		t.Fatalf("read the upstream feature tip after sync: %v", err)
	}
	if after != before {
		t.Errorf("the upstream feature branch moved: %s to %s", before, after)
	}
}

// TestRebaseStack_forkDependentRefused reports the dependent a refused sync names.
func TestRebaseStack_forkDependentRefused(t *testing.T) {
	setupTestDB(t)
	alice, bob, upstreamURL, _ := forkPRFixture(t)

	pushBranch(t, alice, "middleware")
	if _, err := git.ExecGit(alice, []string{"checkout", "main"}); err != nil {
		t.Fatalf("alice checkout main: %v", err)
	}
	root := CreatePR(alice, "Add middleware", "", CreatePROptions{Base: "#branch:main", Head: "middleware"})
	if !root.Success {
		t.Fatalf("CreatePR root: %s", root.Error.Message)
	}
	dependent := CreatePR(bob, "Add routes", "", CreatePROptions{
		Base:      upstreamURL + "#branch:middleware",
		Head:      "feature",
		DependsOn: []string{root.Data.ID},
	})
	if !dependent.Success {
		t.Fatalf("CreatePR dependent: %s", dependent.Error.Message)
	}

	res := RebaseStack(alice, root.Data.ID)
	if res.Success || res.Error.Code != "REBASE_FAILED" {
		t.Fatalf("RebaseStack over a fork dependent: want REBASE_FAILED, got success=%v code=%q", res.Success, res.Error.Code)
	}
	if !strings.Contains(res.Error.Message, "Add routes") {
		t.Errorf("the error should name the dependent, got %q", res.Error.Message)
	}
}
