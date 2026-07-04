// tuiproposal.go - Adapter from the proposals engine to tuicore's history view.
// Lives outside tuicore because proposals imports the extension packages, which
// import tuicore; a direct tuicore->proposals edge would be an import cycle.
package tuiproposal

import "github.com/gitsocial-org/gitsocial/library/proposals"

// Accept applies an accepted cross-repo proposal and reports the outcome as the
// primitive triple tuicore.ProposalActionFn expects.
func Accept(workdir, ref string) (ok bool, errMsg, canonicalRef string) {
	out := proposals.Accept(workdir, ref)
	return out.Success, out.Error.Message, out.Data.CanonicalRef
}

// Decline publishes a durable decline for a cross-repo proposal and reports the
// outcome as the primitive triple tuicore.ProposalActionFn expects.
func Decline(workdir, ref string) (ok bool, errMsg, canonicalRef string) {
	out := proposals.Decline(workdir, ref)
	return out.Success, out.Error.Message, out.Data.CanonicalRef
}
