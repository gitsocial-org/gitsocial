// version_item_message.go - Shared base and helpers for gitmsg version items.
package tuicore

import (
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// MessageVersionItem is the shared base for gitmsg version items: it provides
// the VersionItem plumbing and the generic list entry. The extension version
// items embed it, adding a RenderDetail that reconstructs their real hero card.
type MessageVersionItem struct {
	Version     gitmsg.MessageVersion
	ShowEmail   bool
	ProposalTag string
}

// GetID returns the version's unique identifier.
func (v MessageVersionItem) GetID() string { return v.Version.ID }

// GetTimestamp returns the version's creation time.
func (v MessageVersionItem) GetTimestamp() time.Time { return v.Version.Timestamp }

// GetEditOf returns the ID of the item this version edits.
func (v MessageVersionItem) GetEditOf() string { return v.Version.EditOf }

// IsRetracted returns true if this version has been retracted.
func (v MessageVersionItem) IsRetracted() bool { return v.Version.IsRetracted }

// AuthorDisplay returns the author name, optionally with email.
func (v MessageVersionItem) AuthorDisplay(showEmail bool) string {
	name := v.Version.AuthorName
	if name == "" {
		name = "Anonymous"
	}
	if showEmail && v.Version.AuthorEmail != "" {
		name += " <" + v.Version.AuthorEmail + ">"
	}
	return name
}

// Ref returns the version's repo URL, commit hash, and branch.
func (v MessageVersionItem) Ref() (string, string, string) {
	return v.Version.RepoURL, v.Version.CommitHash, v.Version.Branch
}

// IsOpenProposal reports whether this version is an open cross-repo proposal.
func (v MessageVersionItem) IsOpenProposal() bool { return IsOpenProposalTag(v.ProposalTag) }

// subject extracts the subject (first line) from the content.
func (v MessageVersionItem) subject() string {
	lines := strings.SplitN(v.Version.Content, "\n", 2)
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return ""
}

// RenderListEntry renders a compact list entry for this version.
func (v MessageVersionItem) RenderListEntry(index, total int, label string, selected bool, width int) string {
	hash := v.Version.CommitHash
	if len(hash) > 12 {
		hash = hash[:12]
	}
	header := fmt.Sprintf("Version %d (%s) - %s - %s - %s", total-index, label, hash, v.AuthorDisplay(v.ShowEmail), v.Version.Timestamp.Format("2006-01-02 15:04:05"))
	var b strings.Builder
	if selected {
		b.WriteString(Highlight.Render("▶ " + header))
	} else {
		b.WriteString("  " + header)
	}
	b.WriteString(RenderProposalTag(v.ProposalTag))
	b.WriteString("\n")
	if v.Version.IsRetracted {
		b.WriteString(Dim.Render("    [deleted]"))
	} else {
		subject := v.subject()
		if len(subject) > 80 {
			subject = subject[:80] + "..."
		}
		b.WriteString("    " + subject)
	}
	b.WriteString("\n")
	return b.String()
}

// RenderVersionBanner renders the version-mode banner lines shared by the hero
// cards: a dim "edited by <author> · <time>" ("[deleted] · " prefix when
// retracted) followed by a spacer line.
func RenderVersionBanner(selectionBar, author string, t time.Time, retracted bool) []string {
	if author == "" {
		author = "Anonymous"
	}
	banner := "edited by " + author + " · " + FormatFullTime(t)
	if retracted {
		banner = "[deleted] · " + banner
	}
	return []string{selectionBar + Dim.Render(banner), selectionBar}
}

// LoadMessageHistory fetches the gitmsg edit history for ctx.Ref, defaulting the
// branch to extBranch's data branch, and wraps each version via wrap. Shared by
// the PM and release history loaders, which differ only in that wrapping.
func LoadMessageHistory(ctx HistoryLoadContext, extBranch string, wrap func(MessageVersionItem) VersionItem) ([]VersionItem, error) {
	parsed := protocol.ParseRef(ctx.Ref)
	if parsed.Value == "" {
		return nil, fmt.Errorf("invalid ref: %s", ctx.Ref)
	}
	branch := parsed.Branch
	if branch == "" {
		branch = gitmsg.GetExtBranch(ctx.Workdir, extBranch)
	}
	ref := protocol.CreateRef(protocol.RefTypeCommit, parsed.Value, parsed.Repository, branch)
	versions, err := gitmsg.GetHistory(ref, ctx.WorkspaceURL)
	if err != nil {
		return nil, err
	}
	items := make([]VersionItem, len(versions))
	for i, version := range versions {
		items[i] = wrap(MessageVersionItem{
			Version:     version,
			ShowEmail:   ctx.ShowEmail,
			ProposalTag: ProposalTag(ctx.Owned, ctx.WorkspaceURL, version.RepoURL, version.CommitHash, version.Branch),
		})
	}
	return items, nil
}
