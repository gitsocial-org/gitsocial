// version_item_message.go - Default card-style VersionItem for gitmsg message versions.
package tuicore

import (
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// MessageHistoryLoader returns a history loader that fetches a gitmsg edit
// history and wraps each version as a MessageVersionItem. extBranch is the
// default data branch used when the requested ref omits one. Shared by the PM
// and release history views, which differ only in that branch.
func MessageHistoryLoader(extBranch string) func(HistoryLoadContext) ([]VersionItem, error) {
	return func(ctx HistoryLoadContext) ([]VersionItem, error) {
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
			items[i] = MessageVersionItem{
				Version:     version,
				ShowEmail:   ctx.ShowEmail,
				ProposalTag: ProposalTag(ctx.Owned, ctx.WorkspaceURL, version.RepoURL, version.CommitHash, version.Branch),
			}
		}
		return items, nil
	}
}

// MessageVersionItem is the default VersionItem for plain message-versioned
// items (PM issues/milestones/sprints, releases): it renders a gitmsg version
// as a subject/body card. Items with bespoke layouts implement VersionItem
// directly instead of using this type.
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

// body extracts the body (after first line) from the content.
func (v MessageVersionItem) body() string {
	lines := strings.SplitN(v.Version.Content, "\n", 2)
	if len(lines) > 1 {
		return strings.TrimSpace(lines[1])
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

// RenderDetail renders the full detail view for this version.
func (v MessageVersionItem) RenderDetail(width int) string {
	var content string
	if v.Version.IsRetracted {
		content = "[deleted]"
	} else {
		subject := v.subject()
		body := v.body()
		if subject != "" && body != "" {
			content = subject + "\n\n" + body
		} else if subject != "" {
			content = subject
		} else if body != "" {
			content = body
		}
	}
	card := Card{
		Header: CardHeader{
			Title:       v.AuthorDisplay(v.ShowEmail),
			Subtitle:    []HeaderPart{{Text: FormatFullTime(v.Version.Timestamp)}},
			IsRetracted: v.Version.IsRetracted,
		},
		Content: CardContent{Text: content},
	}
	opts := CardOptions{
		MaxLines:  -1,
		ShowStats: false,
		Width:     width,
		WrapWidth: width - 5,
		Markdown:  true,
	}
	return RenderCard(card, opts)
}
