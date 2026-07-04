// post_history.go - Edit history view showing post versions with diff display.
package tuisocial

import (
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// PostVersionItem wraps social.Post to implement tuicore.VersionItem.
type PostVersionItem struct {
	Post      social.Post
	ShowEmail bool
}

// GetID returns the post's unique identifier.
func (p PostVersionItem) GetID() string { return p.Post.ID }

// GetTimestamp returns the post's creation time.
func (p PostVersionItem) GetTimestamp() time.Time { return p.Post.Timestamp }

// GetEditOf returns the ID of the post this version edits.
func (p PostVersionItem) GetEditOf() string { return p.Post.EditOf }

// IsRetracted returns true if this version has been retracted.
func (p PostVersionItem) IsRetracted() bool { return p.Post.IsRetracted }

// AuthorDisplay returns the author name, optionally with email.
func (p PostVersionItem) AuthorDisplay(showEmail bool) string {
	name := p.Post.Author.Name
	if name == "" {
		name = "Anonymous"
	}
	if showEmail && p.Post.Author.Email != "" {
		name += " <" + p.Post.Author.Email + ">"
	}
	return name
}

// Ref returns the post's repo URL, commit hash, and branch.
func (p PostVersionItem) Ref() (string, string, string) {
	return p.Post.Repository, protocol.ParseRef(p.Post.ID).Value, p.Post.Branch
}

// IsOpenProposal reports whether this version is an open cross-repo proposal.
// Posts have no cross-repo proposal flow, so this is always false.
func (p PostVersionItem) IsOpenProposal() bool { return false }

// RenderListEntry renders a compact list entry for this version.
func (p PostVersionItem) RenderListEntry(index, total int, label string, selected bool, width int) string {
	hash, _ := protocol.NormalizeHash(protocol.ParseRef(p.Post.ID).Value)
	name := p.AuthorDisplay(p.ShowEmail)
	if p.Post.Display.IsVerified {
		name += " " + tuicore.SafeIcon("⚿")
	}
	header := fmt.Sprintf("Version %d (%s) - %s - %s - %s", total-index, label, hash, name, p.Post.Timestamp.Format("2006-01-02 15:04:05"))
	var b strings.Builder
	if selected {
		b.WriteString(tuicore.Highlight.Render("▶ " + header))
	} else {
		b.WriteString("  " + header)
	}
	b.WriteString("\n")
	if p.Post.IsRetracted {
		b.WriteString(tuicore.Dim.Render("    [deleted]"))
	} else {
		content := strings.TrimSpace(p.Post.Content)
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		b.WriteString("    " + content)
	}
	b.WriteString("\n")
	return b.String()
}

// RenderDetail renders the full detail view for this version.
func (p PostVersionItem) RenderDetail(width int) string {
	card := PostToCardWithOptions(p.Post, nil, PostToCardOptions{
		FullTime:   true,
		SkipNested: true,
		ShowEmail:  p.ShowEmail,
	})
	opts := tuicore.CardOptions{
		MaxLines:  -1,
		ShowStats: false,
		Width:     width,
		WrapWidth: width - 5,
		Markdown:  true,
	}
	return tuicore.RenderCard(card, opts)
}

// loadPostHistory fetches and wraps the edit history for a post.
func loadPostHistory(ctx tuicore.HistoryLoadContext) ([]tuicore.VersionItem, error) {
	parsed := protocol.ParseRef(ctx.Ref)
	if parsed.Value == "" {
		return nil, fmt.Errorf("invalid ref: %s", ctx.Ref)
	}
	branch := parsed.Branch
	if branch == "" {
		branch = gitmsg.GetExtBranch(ctx.Workdir, "social")
	}
	posts, err := social.GetEditHistoryPosts(parsed.Repository, parsed.Value, branch, ctx.WorkspaceURL)
	if err != nil {
		return nil, err
	}
	items := make([]tuicore.VersionItem, len(posts))
	for i, post := range posts {
		items[i] = PostVersionItem{Post: post, ShowEmail: ctx.ShowEmail}
	}
	return items, nil
}

// NewHistoryView creates the edit-history view for a post.
func NewHistoryView(workdir string) *tuicore.HistoryView {
	return tuicore.NewHistoryView(workdir, tuicore.HistoryConfig{
		ParamName:  "postID",
		Context:    tuicore.History,
		TitleLabel: "History",
		Load:       loadPostHistory,
		DiffLoc:    tuicore.LocHistoryDiff,
	})
}
