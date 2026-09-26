// adopt_test.go - Tests for adopting a registered fork's pull request (GITMSG.md 1.5)
package review

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// registeredForkPR opens a pull request from a fork the upstream registers, returning the upstream workdir and the pull request.
func registeredForkPR(t *testing.T, register bool) (string, PullRequest) {
	t.Helper()
	setupTestDB(t)
	alice, bob, upstreamURL, forkURL := forkPRFixture(t)
	created := CreatePR(bob, "Fix the bug", "the body", CreatePROptions{Base: upstreamURL + "#branch:main", Head: "feature"})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}
	if register {
		if err := gitmsg.AddFork(alice, forkURL); err != nil {
			t.Fatalf("AddFork: %v", err)
		}
	}
	return alice, created.Data
}

// adoptionCount counts the copies on the upstream's review branch that adopt a pull request.
func adoptionCount(t *testing.T, workdir string) int {
	t.Helper()
	out, err := git.ExecGit(workdir, []string{"log", gitmsg.GetExtBranch(workdir, "review"), "--format=%B"})
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	n := 0
	for _, line := range strings.Split(out.Stdout, "\n") {
		if strings.HasPrefix(line, "GitMsg: ") && strings.Contains(line, `adopts="`) {
			n++
		}
	}
	return n
}

// TestUpdatePR_AdoptsForkPR asserts the first change to a registered fork's pull request adopts it and lands on the copy.
func TestUpdatePR_AdoptsForkPR(t *testing.T) {
	alice, pr := registeredForkPR(t, true)
	labels := []string{"kind/bug"}
	res := UpdatePR(alice, pr.ID, UpdatePROptions{Labels: &labels})
	if !res.Success {
		t.Fatalf("UpdatePR: %s", res.Error.Message)
	}
	if res.Data.Repository != gitmsg.ResolveRepoURL(alice) || len(res.Data.Labels) != 1 {
		t.Errorf("result = %s with labels %v, want the copy with its label in the upstream", res.Data.Repository, res.Data.Labels)
	}
	if res.Data.OriginalAuthor == nil || res.Data.OriginalAuthor.Name != "bob" {
		t.Errorf("copy original author = %+v, want bob", res.Data.OriginalAuthor)
	}
	if n := adoptionCount(t, alice); n != 1 {
		t.Errorf("adopting copies = %d, want 1", n)
	}
}

// TestClosePR_ReusesAdoptedCopy asserts a close after an adoption, by either ref, reuses the copy and writes no second one.
func TestClosePR_ReusesAdoptedCopy(t *testing.T) {
	alice, pr := registeredForkPR(t, true)
	adopted := AdoptPR(alice, pr.ID)
	if !adopted.Success {
		t.Fatalf("AdoptPR: %s", adopted.Error.Message)
	}
	closed := ClosePR(alice, pr.ID)
	if !closed.Success || closed.Data.State != PRStateClosed || protocol.ParseRef(closed.Data.ID).Value != protocol.ParseRef(adopted.Data.ID).Value {
		t.Fatalf("ClosePR = %+v, %+v, want the adopted copy closed", closed.Data, closed.Error)
	}
	if again := ClosePR(alice, pr.ID); again.Success {
		t.Error("a second close by the fork ref must see the closed copy and refuse")
	}
	if n := adoptionCount(t, alice); n != 1 {
		t.Errorf("adopting copies = %d, want 1", n)
	}
}

// TestAdoptPR asserts adopting writes an unchanged copy once, and a second call returns it.
func TestAdoptPR(t *testing.T) {
	alice, pr := registeredForkPR(t, true)
	first := AdoptPR(alice, pr.ID)
	if !first.Success || first.Data.Repository != gitmsg.ResolveRepoURL(alice) || first.Data.State != PRStateOpen || first.Data.Subject != pr.Subject {
		t.Fatalf("AdoptPR = %+v, %+v, want an unchanged open copy in the upstream", first.Data, first.Error)
	}
	if again := AdoptPR(alice, pr.ID); !again.Success || again.Data.ID != first.Data.ID {
		t.Errorf("a second adoption returned %+v, want the first copy %s", again.Data, first.Data.ID)
	}
	if n := adoptionCount(t, alice); n != 1 {
		t.Errorf("adopting copies = %d, want 1", n)
	}
}

// TestAdoptPR_Refusals asserts adoption refuses an unregistered fork and a workspace pull request, and a draft change stays the author's.
func TestAdoptPR_Refusals(t *testing.T) {
	alice, pr := registeredForkPR(t, false)
	if res := AdoptPR(alice, pr.ID); res.Success || res.Error.Code != "NOT_A_FORK" {
		t.Errorf("adopting an unregistered fork's pull request = %+v, want NOT_A_FORK", res.Error)
	}
	labels := []string{"kind/bug"}
	if res := UpdatePR(alice, pr.ID, UpdatePROptions{Labels: &labels}); !res.Success || res.Data.Repository == gitmsg.ResolveRepoURL(alice) {
		t.Errorf("a change to an unregistered fork's pull request must stay a proposal, got %+v, %+v", res.Data, res.Error)
	}
	if n := adoptionCount(t, alice); n != 0 {
		t.Errorf("adopting copies = %d, want 0", n)
	}
	if err := gitmsg.AddFork(alice, pr.Repository); err != nil {
		t.Fatalf("AddFork: %v", err)
	}
	if res := MarkReady(alice, pr.ID); res.Success || res.Error.Code != "NOT_AUTHOR" {
		t.Errorf("marking a fork's pull request ready = %+v, want NOT_AUTHOR", res.Error)
	}
	own := CreatePR(alice, "Own change", "", CreatePROptions{Base: "main", Head: "main"})
	if !own.Success {
		t.Fatalf("CreatePR: %s", own.Error.Message)
	}
	if res := AdoptPR(alice, own.Data.ID); res.Success || res.Error.Code != "NOT_FOREIGN" {
		t.Errorf("adopting a workspace pull request = %+v, want NOT_FOREIGN", res.Error)
	}
}

// TestAdoptPR_KeepsState asserts the copy carries the original's draft flag, labels and closed state.
func TestAdoptPR_KeepsState(t *testing.T) {
	setupTestDB(t)
	alice, bob, upstreamURL, forkURL := forkPRFixture(t)
	if err := gitmsg.AddFork(alice, forkURL); err != nil {
		t.Fatalf("AddFork: %v", err)
	}
	draft := CreatePR(bob, "Work in progress", "", CreatePROptions{Base: upstreamURL + "#branch:main", Head: "feature", Draft: true, Labels: []string{"kind/bug"}})
	if !draft.Success {
		t.Fatalf("CreatePR: %s", draft.Error.Message)
	}
	copied := AdoptPR(alice, draft.Data.ID)
	if !copied.Success || !copied.Data.IsDraft || len(copied.Data.Labels) != 1 {
		t.Errorf("adopted draft = %+v, %+v, want a draft copy keeping its label", copied.Data, copied.Error)
	}
	withdrawn := CreatePR(bob, "Withdrawn", "", CreatePROptions{Base: upstreamURL + "#branch:main", Head: "feature"})
	closed := PRStateClosed
	if res := UpdatePR(bob, withdrawn.Data.ID, UpdatePROptions{State: &closed}); !res.Success {
		t.Fatalf("UpdatePR by its author: %s", res.Error.Message)
	}
	if res := AdoptPR(alice, withdrawn.Data.ID); !res.Success || res.Data.State != PRStateClosed {
		t.Errorf("adopted closed pull request = %+v, %+v, want a closed copy", res.Data, res.Error)
	}
}

// TestUpdatePRTips_ChecksAdoptedCopy asserts a tips update by the fork ref sees the closed copy and refuses.
func TestUpdatePRTips_ChecksAdoptedCopy(t *testing.T) {
	alice, pr := registeredForkPR(t, true)
	if res := ClosePR(alice, pr.ID); !res.Success {
		t.Fatalf("ClosePR: %s", res.Error.Message)
	}
	if res := UpdatePRTips(alice, pr.ID); res.Success || res.Error.Code != "INVALID_STATE" {
		t.Errorf("UpdatePRTips on an adopted, closed pull request = %+v, want INVALID_STATE", res.Error)
	}
}

// TestGetReviewSummary_CountsAdoptedOriginal asserts an approval given on the fork original counts for the copy.
func TestGetReviewSummary_CountsAdoptedOriginal(t *testing.T) {
	alice, pr := registeredForkPR(t, true)
	if res := CreateFeedback(alice, "Looks good", CreateFeedbackOptions{PullRequest: pr.ID, ReviewState: ReviewStateApproved}); !res.Success {
		t.Fatalf("CreateFeedback: %s", res.Error.Message)
	}
	copied := AdoptPR(alice, pr.ID)
	if !copied.Success {
		t.Fatalf("AdoptPR: %s", copied.Error.Message)
	}
	ref := protocol.ParseRef(copied.Data.ID)
	if got := GetReviewSummary(copied.Data.Repository, ref.Value, ref.Branch, nil); got.Approved != 1 {
		t.Errorf("copy approvals = %d, want the original's 1", got.Approved)
	}
}

// TestGetDependents_CollapsesAdopted asserts a dependent pull request and its adopted copy are found once.
func TestGetDependents_CollapsesAdopted(t *testing.T) {
	setupTestDB(t)
	alice, bob, upstreamURL, forkURL := forkPRFixture(t)
	if err := gitmsg.AddFork(alice, forkURL); err != nil {
		t.Fatalf("AddFork: %v", err)
	}
	first := CreatePR(bob, "First", "", CreatePROptions{Base: upstreamURL + "#branch:main", Head: "feature"})
	second := CreatePR(bob, "Second", "", CreatePROptions{Base: upstreamURL + "#branch:main", Head: "feature", DependsOn: []string{first.Data.ID}})
	if !first.Success || !second.Success {
		t.Fatalf("CreatePR: %+v %+v", first.Error, second.Error)
	}
	copied := AdoptPR(alice, second.Data.ID)
	if !copied.Success {
		t.Fatalf("AdoptPR: %s", copied.Error.Message)
	}
	deps := getDependents(protocol.ParseRef(first.Data.ID).Value)
	if len(deps) != 1 || deps[0].ID != copied.Data.ID {
		t.Errorf("dependents = %d (%v), want the copy alone", len(deps), deps)
	}
}
