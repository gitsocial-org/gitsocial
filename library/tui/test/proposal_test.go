// proposal_test.go - Cross-repo proposal display and accept/decline flow
package test

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestProposalDisplay checks that the fork's inert cross-repo edit of the
// workspace issue is surfaced everywhere the user should see it.
func TestProposalDisplay(t *testing.T) {
	f := getFixture(t)
	h := New(t, f.Workdir, f.CacheDir)

	t.Run("IssueListMarker", func(t *testing.T) {
		assertRendersItem(t, h, tuicore.Location{Path: "/pm/issues"}, f.IssueSubject+" ✎")
	})
	t.Run("IssueDetailBanner", func(t *testing.T) {
		assertRendersItem(t, h, tuicore.LocPMIssueDetail(f.IssueID),
			"✎  Proposed edits from another repo")
	})
	t.Run("HistoryRow", func(t *testing.T) {
		// The row tag is "✎ proposal · bob/repo", truncated to the panel width.
		assertRendersItem(t, h, tuicore.LocPMIssueHistory(f.IssueID),
			"2 version(s)", "Bob", "✎ proposal")
	})
	t.Run("HistoryFooterOffersAcceptAndDecline", func(t *testing.T) {
		assertRendersItem(t, h, tuicore.LocPMIssueHistory(f.IssueID), "A:accept", "X:decline")
	})
}

// TestProposalAccept drives the accept key on the history picker and verifies
// the proposed close is applied to the workspace's own copy of the issue.
// Uses an isolated fixture: accepting writes a mirror edit to the repo.
func TestProposalAccept(t *testing.T) {
	f := SetupFixture(t)
	h := New(t, f.Workdir, f.CacheDir)

	if got := pm.GetIssue(f.IssueID); got.Data.State != pm.StateOpen {
		t.Fatalf("pre-accept state = %q, want open (a cross-repo edit must stay inert)", got.Data.State)
	}
	h.NavigateTo(tuicore.LocPMIssueHistory(f.IssueID))
	// The proposal is the newest version, so it is the selected row.
	if out := rendered(h); !strings.Contains(out, "✎ proposal") {
		t.Fatalf("expected an open proposal in the history picker, got:\n%s", out)
	}
	h.SendKey("A")

	out := rendered(h)
	if !strings.Contains(out, "Proposal accepted") {
		t.Errorf("expected \"Proposal accepted\" status, got:\n%s", out)
	}
	got := pm.GetIssue(f.IssueID)
	if got.Data.State != pm.StateClosed {
		t.Errorf("post-accept state = %q, want closed", got.Data.State)
	}
	if got.Data.HasProposedEdits {
		t.Error("post-accept: the proposed-edit marker should clear")
	}
}

// TestProposalDecline drives the decline key and verifies the marker clears
// without applying the proposed change.
func TestProposalDecline(t *testing.T) {
	f := SetupFixture(t)
	h := New(t, f.Workdir, f.CacheDir)

	h.NavigateTo(tuicore.LocPMIssueHistory(f.IssueID))
	h.SendKey("X")

	out := rendered(h)
	if !strings.Contains(out, "Proposal declined") {
		t.Errorf("expected \"Proposal declined\" status, got:\n%s", out)
	}
	got := pm.GetIssue(f.IssueID)
	if got.Data.State != pm.StateOpen {
		t.Errorf("post-decline state = %q, want open (a declined edit must not apply)", got.Data.State)
	}
	if got.Data.HasProposedEdits {
		t.Error("post-decline: the proposed-edit marker should clear")
	}
}
