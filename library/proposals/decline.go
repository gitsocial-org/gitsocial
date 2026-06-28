// decline.go - Decline a cross-repo proposal: publish a durable decline marker so
// the proposer learns it was declined and the owner's choice survives a re-clone.
// A decline applies nothing (it is the absence of acceptance, recorded durably);
// accept takes precedence over a decline for the same proposal (GITMSG.md §1.5).
package proposals

import (
	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
)

// Decline publishes a decline marker for a cross-repo proposal whose canonical
// this workspace owns. Refuses if the proposal was already accepted.
func Decline(workdir, proposalRef string) Result[Outcome] {
	parsed := protocol.ParseRef(proposalRef)
	if parsed.Value == "" || parsed.Branch == "" {
		return result.Err[Outcome]("INVALID_REF", "proposal ref must be a fully-qualified commit ref")
	}
	editRepo := protocol.NormalizeURL(parsed.Repository)
	ver, err := cache.GetCanonical(editRepo, parsed.Value, parsed.Branch)
	if err != nil {
		return result.Err[Outcome]("LOOKUP_FAILED", err.Error())
	}
	if ver == nil || ver.EditRepoURL == ver.CanonicalRepoURL {
		return result.Err[Outcome]("NOT_A_PROPOSAL", "ref is not a cross-repo proposal")
	}
	if ver.CanonicalRepoURL != protocol.NormalizeURL(gitmsg.ResolveRepoURL(workdir)) {
		return result.Err[Outcome]("NOT_OWNER", "you do not own this item's canonical")
	}
	if accepted, derr := cache.HasAcceptance(editRepo, parsed.Value, parsed.Branch); derr == nil && accepted {
		return result.Err[Outcome]("ALREADY_ACCEPTED", "this proposal has already been accepted")
	}
	if declined, derr := cache.HasDecline(editRepo, parsed.Value, parsed.Branch); derr == nil && declined {
		return result.Err[Outcome]("ALREADY_DECLINED", "this proposal has already been declined")
	}
	pubRef := protocol.CreateRef(protocol.RefTypeCommit, parsed.Value, editRepo, parsed.Branch)
	if derr := gitmsg.AddDecline(workdir, pubRef); derr != nil {
		return result.Err[Outcome]("DECLINE_FAILED", derr.Error())
	}
	_ = cache.RecordDecline(editRepo, parsed.Value, parsed.Branch)
	return result.Ok(Outcome{
		Ext:          ver.CanonicalRepoURL,
		CanonicalRef: protocol.CreateRef(protocol.RefTypeCommit, ver.CanonicalHash, ver.CanonicalRepoURL, ver.CanonicalBranch),
	})
}
