// util_proposal.go - Helpers for marking cross-repo proposal versions in the
// history pickers, so it's obvious which version to accept (A).
package tuicore

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

func init() {
	RegisterMessageHandler(func(msg tea.Msg, ctx AppContext) (bool, tea.Cmd) {
		if m, ok := msg.(ProposalAcceptedMsg); ok {
			return handleProposalAccepted(m, ctx)
		}
		return false, nil
	})
}

// ProposalAcceptedMsg is sent when a cross-repo proposed edit is accepted (or
// declined) from a history picker. Location is the item detail view to reload
// so the now-applied edit is visible.
type ProposalAcceptedMsg struct {
	Location Location
	Declined bool
	Err      error
}

// handleProposalAccepted reports the outcome and navigates back to the detail view.
func handleProposalAccepted(msg ProposalAcceptedMsg, ctx AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), MessageTypeError)
		return true, nil
	}
	text := "Proposal accepted"
	if msg.Declined {
		text = "Proposal declined"
	}
	msgCmd := ctx.Host().SetMessageWithTimeout(text, MessageTypeSuccess, 5*time.Second)
	return true, tea.Batch(msgCmd, func() tea.Msg {
		return NavigateMsg{Location: msg.Location, Action: NavReplace}
	})
}

// ownsCanonical reports whether this workspace owns the item named by ref (its
// canonical lives here), i.e. accepting proposals on it is possible.
func OwnsCanonical(ref, workspaceURL string) bool {
	repo := protocol.ParseRef(ref).Repository
	return repo == "" || protocol.NormalizeURL(repo) == workspaceURL
}

// proposalTag returns a history-row tag: a not-yet-accepted cross-repo edit on an
// owned canonical is an acceptable proposal; an already-accepted one is dimmed.
// Empty when the version isn't an actionable proposal (own version, or not owned).
func ProposalTag(owned bool, workspaceURL, repoURL, hash, branch string) string {
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
func IsOpenProposalTag(tag string) bool { return strings.HasPrefix(tag, "✎") }

// renderProposalTag styles a proposal tag for a history row (empty input → "").
func RenderProposalTag(tag string) string {
	if tag == "" {
		return ""
	}
	style := Warning
	if strings.HasPrefix(tag, "✓") {
		style = Dim
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
