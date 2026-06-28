// decline_test.go - Decline a cross-repo proposal: clears the proposed-edit marker
// without changing resolved state, publishes a durable marker, is idempotent and
// owner-only; accept takes precedence over a prior decline.
package proposals

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
)

func TestDecline_clearsMarkerPublishesIdempotent(t *testing.T) {
	setupCache(t)
	bob := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")
	alice := cloneAs(t, initBareOrigin(t), "alice", "alice@test.com")

	created := pm.CreateIssue(bob, "Crash", "", pm.CreateIssueOptions{})
	if !created.Success {
		t.Fatalf("CreateIssue: %s", created.Error.Message)
	}
	issueRef := created.Data.ID
	if res := pm.CloseIssue(alice, issueRef); !res.Success {
		t.Fatalf("CloseIssue: %s", res.Error.Message)
	}
	if got := pm.GetIssue(issueRef); !got.Data.HasProposedEdits {
		t.Fatal("pre-decline: want HasProposedEdits=true")
	}

	proposalRef := crossRepoProposal(t, issueRef)
	if out := Decline(bob, proposalRef); !out.Success {
		t.Fatalf("Decline: %s (%s)", out.Error.Message, out.Error.Code)
	}

	// Decline clears the proposed-edit marker without changing resolved state.
	got := pm.GetIssue(issueRef)
	if got.Data.HasProposedEdits {
		t.Error("post-decline: HasProposedEdits should clear")
	}
	if got.Data.State != pm.StateOpen {
		t.Errorf("post-decline: state = %q, want open (decline applies nothing)", got.Data.State)
	}

	// A durable marker is published under refs/gitmsg/core/declines/*.
	refs, err := git.ExecGit(bob, []string{"for-each-ref", "--format=%(refname)", gitmsg.DeclinesRefPrefix})
	if err != nil {
		t.Fatalf("for-each-ref: %v", err)
	}
	if strings.TrimSpace(refs.Stdout) == "" {
		t.Error("Decline should publish a marker under refs/gitmsg/core/declines/")
	}

	// Idempotent: re-declining is a no-op signaled by ALREADY_DECLINED.
	if out := Decline(bob, proposalRef); out.Success || out.Error.Code != "ALREADY_DECLINED" {
		t.Errorf("re-Decline: want ALREADY_DECLINED, got success=%v code=%q", out.Success, out.Error.Code)
	}
}

func TestDecline_notOwner(t *testing.T) {
	setupCache(t)
	bob := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")
	alice := cloneAs(t, initBareOrigin(t), "alice", "alice@test.com")

	created := pm.CreateIssue(bob, "Bug", "", pm.CreateIssueOptions{})
	if !created.Success {
		t.Fatalf("CreateIssue: %s", created.Error.Message)
	}
	if res := pm.CloseIssue(alice, created.Data.ID); !res.Success {
		t.Fatalf("CloseIssue: %s", res.Error.Message)
	}
	proposalRef := crossRepoProposal(t, created.Data.ID)

	// Alice is the proposer, not the canonical's owner, so she cannot decline it.
	if out := Decline(alice, proposalRef); out.Success || out.Error.Code != "NOT_OWNER" {
		t.Errorf("Decline by non-owner: want NOT_OWNER, got success=%v code=%q", out.Success, out.Error.Code)
	}
}

// TestDecline_acceptDominates: a declined proposal can still be accepted, and the
// accept wins (resolved state changes, the proposed-edit marker stays clear).
func TestDecline_acceptDominates(t *testing.T) {
	setupCache(t)
	bob := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")
	alice := cloneAs(t, initBareOrigin(t), "alice", "alice@test.com")

	created := pm.CreateIssue(bob, "Crash", "", pm.CreateIssueOptions{})
	if !created.Success {
		t.Fatalf("CreateIssue: %s", created.Error.Message)
	}
	issueRef := created.Data.ID
	if res := pm.CloseIssue(alice, issueRef); !res.Success {
		t.Fatalf("CloseIssue: %s", res.Error.Message)
	}
	proposalRef := crossRepoProposal(t, issueRef)

	if out := Decline(bob, proposalRef); !out.Success {
		t.Fatalf("Decline: %s", out.Error.Message)
	}
	if out := Accept(bob, proposalRef); !out.Success {
		t.Fatalf("Accept after decline should still succeed: %s (%s)", out.Error.Message, out.Error.Code)
	}

	got := pm.GetIssue(issueRef)
	if got.Data.State != pm.StateClosed {
		t.Errorf("accept must win over a prior decline: state = %q, want closed", got.Data.State)
	}
	if got.Data.HasProposedEdits {
		t.Error("accept-after-decline should leave the marker clear")
	}
}
