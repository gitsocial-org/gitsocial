// accept_test.go - Manual accept records an acceptance and clears the proposed-edit
// marker, and is idempotent (re-accepting is a no-op).
package proposals

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
)

func TestAccept_recordsAcceptanceAndClearsMarker(t *testing.T) {
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
		t.Fatal("pre-accept: want HasProposedEdits=true")
	}

	proposalRef := crossRepoProposal(t, issueRef)
	if out := Accept(bob, proposalRef); !out.Success {
		t.Fatalf("Accept: %s (%s)", out.Error.Message, out.Error.Code)
	}

	got := pm.GetIssue(issueRef)
	if got.Data.State != pm.StateClosed {
		t.Errorf("state = %q, want closed", got.Data.State)
	}
	if got.Data.HasProposedEdits {
		t.Error("post-accept: HasProposedEdits should clear once the proposal is accepted")
	}

	// Idempotent: re-accepting the same proposal is a no-op (no duplicate mirror).
	if out := Accept(bob, proposalRef); out.Success || out.Error.Code != "ALREADY_ACCEPTED" {
		t.Errorf("re-Accept: want ALREADY_ACCEPTED, got success=%v code=%q", out.Success, out.Error.Code)
	}
}
