// derive_test.go - The acceptance is derived from a mirror edit's accepts= header
// when the mirror is processed (the fetch/sync path), independent of the owner's
// explicit accept record. This is the followed-but-not-fork marker-clearing path.
package proposals

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
)

func TestDerivedAcceptance_fromAcceptsHeader(t *testing.T) {
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

	// Author the mirror via applyProposal (not Accept), so no explicit acceptance is
	// recorded: the state a proposer is in after fetching the mirror but before the
	// mirror has been processed.
	out := applyProposal(bob, proposalRef)
	if !out.Success {
		t.Fatalf("applyProposal: %s (%s)", out.Error.Message, out.Error.Code)
	}
	if got := pm.GetIssue(issueRef); !got.Data.HasProposedEdits {
		t.Fatal("pre-derivation: the mirror alone must not clear the marker (no acceptance recorded yet)")
	}

	// Processing the mirror (what fetch/sync does) reads accepts= and derives the
	// acceptance, clearing the marker without any published acceptance marker.
	mirror, err := cache.GetCommit(out.Data.MirrorRepoURL, out.Data.MirrorHash, out.Data.MirrorBranch)
	if err != nil {
		t.Fatalf("get mirror: %v", err)
	}
	cache.ProcessVersionFromHeader(protocol.ParseMessage(mirror.Message), out.Data.MirrorHash, out.Data.MirrorRepoURL, out.Data.MirrorBranch)

	if got := pm.GetIssue(issueRef); got.Data.HasProposedEdits {
		t.Error("post-derivation: processing the mirror's accepts= should clear the marker")
	}
}
