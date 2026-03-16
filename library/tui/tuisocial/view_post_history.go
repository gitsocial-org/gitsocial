// post_history.go - Edit history view showing post versions with diff display
package tuisocial

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/social"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// PostVersionItem wraps social.Post to implement tuicore.VersionItem.
type PostVersionItem struct {
	Post      social.Post
	ShowEmail bool
}

// GetID returns the post's unique identifier.
func (p PostVersionItem) GetID() string {
	return p.Post.ID
}

// GetTimestamp returns the post's creation time.
func (p PostVersionItem) GetTimestamp() time.Time {
	return p.Post.Timestamp
}

// GetEditOf returns the ID of the post this version edits.
func (p PostVersionItem) GetEditOf() string {
	return p.Post.EditOf
}

// IsRetracted returns true if this version has been retracted.
func (p PostVersionItem) IsRetracted() bool {
	return p.Post.IsRetracted
}

// RenderListEntry renders a compact list entry for this version.
func (p PostVersionItem) RenderListEntry(index, total int, label string, selected bool, width int) string {
	hash, _ := protocol.NormalizeHash(protocol.ParseRef(p.Post.ID).Value)
	name := p.Post.Author.Name
	if name == "" {
		name = "Anonymous"
	}
	if p.ShowEmail && p.Post.Author.Email != "" {
		name += " <" + p.Post.Author.Email + ">"
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

// HistoryView displays edit history for a post.
type HistoryView struct {
	picker       *tuicore.VersionPicker
	workdir      string
	workspaceURL string
	showEmail    bool
}

// NewHistoryView creates a new history view.
func NewHistoryView(workdir string) *HistoryView {
	return &HistoryView{
		workdir:      workdir,
		workspaceURL: gitmsg.ResolveRepoURL(workdir),
		picker:       tuicore.NewVersionPicker(),
	}
}

// SetSize sets the view dimensions.
func (v *HistoryView) SetSize(width, height int) {
	v.picker.SetSize(width, height)
}

// Activate loads the edit history when the view becomes active.
func (v *HistoryView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	loc := state.Router.Location()
	postID := loc.Param("postID")
	if postID == "" {
		return nil
	}
	v.picker.SetLoading(true)
	return v.loadHistory(postID)
}

// loadHistory fetches the edit history for a post.
func (v *HistoryView) loadHistory(postID string) tea.Cmd {
	workdir := v.workdir
	return func() tea.Msg {
		parsed := protocol.ParseRef(postID)
		if parsed.Value == "" {
			return HistoryLoadedMsg{Err: fmt.Errorf("invalid ref: %s", postID)}
		}
		branch := parsed.Branch
		if branch == "" {
			branch = gitmsg.GetExtBranch(workdir, "social")
		}
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		posts, err := social.GetEditHistoryPosts(parsed.Repository, parsed.Value, branch, workspaceURL)
		if err != nil {
			return HistoryLoadedMsg{Err: err}
		}
		return HistoryLoadedMsg{Versions: posts}
	}
}

// Update handles messages and returns commands.
func (v *HistoryView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		handled, cmd := v.picker.HandleMouse(msg)
		if handled {
			return cmd
		}
	case tea.KeyPressMsg:
		handled, cmd := v.picker.HandleKey(msg.String())
		if handled {
			return cmd
		}
	case HistoryLoadedMsg:
		v.handleLoaded(msg)
	}
	return nil
}

// handleLoaded processes the loaded history data.
func (v *HistoryView) handleLoaded(msg HistoryLoadedMsg) {
	if msg.Err != nil {
		v.picker.SetLoading(false)
		return
	}
	items := make([]tuicore.VersionItem, len(msg.Versions))
	for i, post := range msg.Versions {
		items[i] = PostVersionItem{Post: post, ShowEmail: v.showEmail}
	}
	v.picker.SetItems(items)
}

// Render renders the history view to a string.
func (v *HistoryView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	content := v.picker.Render()
	footer := tuicore.RenderFooter(state.Registry, tuicore.History, wrapper.ContentWidth(), nil)
	return wrapper.Render(content, footer)
}

// IsInputActive returns false since history view has no text input.
func (v *HistoryView) IsInputActive() bool {
	return false
}

// Title returns the header title showing canonical post info.
func (v *HistoryView) Title() string {
	items := v.picker.Items()
	if len(items) == 0 {
		return "History"
	}
	canonical := items[len(items)-1]
	post := canonical.(PostVersionItem).Post
	name := post.Author.Name
	if name == "" {
		name = "Anonymous"
	}
	if v.showEmail && post.Author.Email != "" {
		name += " <" + post.Author.Email + ">"
	}
	title := "History · " + name
	title += " · " + tuicore.FormatFullTime(post.Timestamp)
	hash := protocol.ParseRef(post.ID).Value
	if ref := tuicore.BuildCommitRef(post.Repository, hash, post.Branch, v.workspaceURL); ref != "" {
		title += " · " + ref
	}
	return title
}

// Bindings returns view-specific key bindings.
func (v *HistoryView) Bindings() []tuicore.Binding {
	return nil
}
