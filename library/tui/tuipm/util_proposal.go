// util_proposal.go - Helpers for marking cross-repo proposal versions in the
// history pickers, so it's obvious which version to accept (A).
package tuipm

import (
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// ownsCanonical reports whether this workspace owns the item named by ref (its
// canonical lives here), i.e. accepting proposals on it is possible.
func ownsCanonical(ref, workspaceURL string) bool {
	repo := protocol.ParseRef(ref).Repository
	return repo == "" || protocol.NormalizeURL(repo) == workspaceURL
}

// proposalTag returns a history-row tag: a not-yet-accepted cross-repo edit on an
// owned canonical is an acceptable proposal; an already-accepted one is dimmed.
// Empty when the version isn't an actionable proposal (own version, or not owned).
func proposalTag(owned bool, workspaceURL, repoURL, hash, branch string) string {
	if !owned {
		return ""
	}
	repo := protocol.NormalizeURL(repoURL)
	if repo == "" || repo == workspaceURL {
		return ""
	}
	if accepted, _ := cache.HasAcceptance(repo, hash, branch); accepted {
		return "✓ accepted · " + shortRepo(repoURL)
	}
	return "✎ proposal · " + shortRepo(repoURL)
}

// isOpenProposalTag reports whether a tag marks an unaccepted (acceptable) proposal.
func isOpenProposalTag(tag string) bool { return strings.HasPrefix(tag, "✎") }

// renderProposalTag styles a proposal tag for a history row (empty input → "").
func renderProposalTag(tag string) string {
	if tag == "" {
		return ""
	}
	style := tuicore.Warning
	if strings.HasPrefix(tag, "✓") {
		style = tuicore.Dim
	}
	return "  " + style.Render(tag)
}

// shortRepo renders a repo URL as owner/repo for compact display.
func shortRepo(url string) string {
	u := strings.TrimSuffix(url, "/")
	parts := strings.Split(u, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return u
}
