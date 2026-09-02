// tuiproposal.go - Adapter from the proposals engine to tuicore's history view.
// Lives outside tuicore because proposals imports the extension packages, which
// import tuicore; a direct tuicore->proposals edge would be an import cycle.
package tuiproposal

import "github.com/gitsocial-org/gitsocial/library/proposals"

// Accept applies an accepted cross-repo proposal and reports the outcome as the
// primitive triple tuicore.ProposalActionFn expects.
func Accept(workdir, ref string) (ok bool, errMsg, canonicalRef string) {
	return flatten(proposals.Accept(workdir, ref))
}

// Decline publishes a durable decline for a cross-repo proposal and reports the
// outcome as the primitive triple tuicore.ProposalActionFn expects.
func Decline(workdir, ref string) (ok bool, errMsg, canonicalRef string) {
	return flatten(proposals.Decline(workdir, ref))
}

// flatten reduces a proposals result to the primitive triple. A successful
// Result carries a nil Error, so the message is only read on failure.
func flatten(out proposals.Result[proposals.Outcome]) (ok bool, errMsg, canonicalRef string) {
	if !out.Success {
		return false, out.Error.Message, ""
	}
	return true, "", out.Data.CanonicalRef
}
