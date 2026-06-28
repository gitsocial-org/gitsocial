// accept.go - Accept a proposal: apply it as the owner's same-repo mirror edit.
// The mirror (authored via the extension Update*/Edit*) carries accepts=, so its
// processing records the acceptance (clearing the proposer's pending marker) and
// publishes the outcome on the gitmsg data branch the proposer fetches; no
// separate marker is needed. Idempotent: an already-accepted proposal is a no-op.
package proposals

import (
	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
)

// Accept applies a single proposal as the owner's same-repo mirror, idempotently:
// an already-accepted proposal returns ALREADY_ACCEPTED without re-mirroring.
func Accept(workdir, proposalRef string) Result[Outcome] {
	parsed := protocol.ParseRef(proposalRef)
	editRepo := protocol.NormalizeURL(parsed.Repository)
	if accepted, derr := cache.HasAcceptance(editRepo, parsed.Value, parsed.Branch); derr == nil && accepted {
		return result.Err[Outcome]("ALREADY_ACCEPTED", "this proposal has already been accepted")
	}
	out := applyProposal(workdir, proposalRef)
	if !out.Success {
		return out
	}
	_ = cache.RecordAcceptance(editRepo, parsed.Value, parsed.Branch)
	return out
}
