// milestone_history.go - Edit history view for PM milestones
package tuipm

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// MilestoneVersionItem wraps gitmsg.MessageVersion to implement tuicore.VersionItem.
type MilestoneVersionItem struct {
	Version   gitmsg.MessageVersion
	ShowEmail bool
}

// GetID returns the version's unique identifier.
func (v MilestoneVersionItem) GetID() string {
	return v.Version.ID
}

// GetTimestamp returns the version's creation time.
func (v MilestoneVersionItem) GetTimestamp() time.Time {
	return v.Version.Timestamp
}

// GetEditOf returns the ID of the item this version edits.
func (v MilestoneVersionItem) GetEditOf() string {
	return v.Version.EditOf
}

// IsRetracted returns true if this version has been retracted.
func (v MilestoneVersionItem) IsRetracted() bool {
	return v.Version.IsRetracted
}

// title extracts the title (first line) from the content.
func (v MilestoneVersionItem) title() string {
	lines := strings.SplitN(v.Version.Content, "\n", 2)
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return ""
}

// body extracts the body (after first line) from the content.
func (v MilestoneVersionItem) body() string {
	lines := strings.SplitN(v.Version.Content, "\n", 2)
	if len(lines) > 1 {
		return strings.TrimSpace(lines[1])
	}
	return ""
}

// RenderListEntry renders a compact list entry for this version.
func (v MilestoneVersionItem) RenderListEntry(index, total int, label string, selected bool, width int) string {
	hash := v.Version.CommitHash
	if len(hash) > 12 {
		hash = hash[:12]
	}
	name := v.Version.AuthorName
	if name == "" {
		name = "Anonymous"
	}
	if v.ShowEmail && v.Version.AuthorEmail != "" {
		name += " <" + v.Version.AuthorEmail + ">"
	}
	header := fmt.Sprintf("Version %d (%s) - %s - %s - %s", total-index, label, hash, name, v.Version.Timestamp.Format("2006-01-02 15:04:05"))
	var b strings.Builder
	if selected {
		b.WriteString(tuicore.Highlight.Render("▶ " + header))
	} else {
		b.WriteString("  " + header)
	}
	b.WriteString("\n")
	if v.Version.IsRetracted {
		b.WriteString(tuicore.Dim.Render("    [deleted]"))
	} else {
		title := v.title()
		if len(title) > 80 {
			title = title[:80] + "..."
		}
		b.WriteString("    " + title)
	}
	b.WriteString("\n")
	return b.String()
}

// RenderDetail renders the full detail view for this version.
func (v MilestoneVersionItem) RenderDetail(width int) string {
	name := v.Version.AuthorName
	if name == "" {
		name = "Anonymous"
	}
	if v.ShowEmail && v.Version.AuthorEmail != "" {
		name += " <" + v.Version.AuthorEmail + ">"
	}
	var content string
	if v.Version.IsRetracted {
		content = "[deleted]"
	} else {
		title := v.title()
		body := v.body()
		if title != "" && body != "" {
			content = title + "\n\n" + body
		} else if title != "" {
			content = title
		} else if body != "" {
			content = body
		}
	}
	card := tuicore.Card{
		Header: tuicore.CardHeader{
			Title:       name,
			Subtitle:    []tuicore.HeaderPart{{Text: tuicore.FormatFullTime(v.Version.Timestamp)}},
			IsRetracted: v.Version.IsRetracted,
		},
		Content: tuicore.CardContent{
			Text: content,
		},
	}
	opts := tuicore.CardOptions{
		MaxLines:  -1,
		ShowStats: false,
		Width:     width,
		WrapWidth: width - 5,
		Markdown:  true,
	}
	return tuicore.RenderCard(card, opts)
}

// MilestoneHistoryView displays edit history for a PM milestone.
type MilestoneHistoryView struct {
	picker       *tuicore.VersionPicker
	workdir      string
	workspaceURL string
	showEmail    bool
}

// NewMilestoneHistoryView creates a new milestone history view.
func NewMilestoneHistoryView(workdir string) *MilestoneHistoryView {
	return &MilestoneHistoryView{
		workdir:      workdir,
		workspaceURL: gitmsg.ResolveRepoURL(workdir),
		picker:       tuicore.NewVersionPicker(),
	}
}

// SetSize sets the view dimensions.
func (v *MilestoneHistoryView) SetSize(width, height int) {
	v.picker.SetSize(width, height)
}

// Activate loads the edit history when the view becomes active.
func (v *MilestoneHistoryView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	loc := state.Router.Location()
	milestoneID := loc.Param("milestoneID")
	if milestoneID == "" {
		return nil
	}
	v.picker.SetLoading(true)
	return v.loadHistory(milestoneID)
}

// loadHistory fetches the edit history for a milestone.
func (v *MilestoneHistoryView) loadHistory(milestoneID string) tea.Cmd {
	workdir := v.workdir
	return func() tea.Msg {
		parsed := protocol.ParseRef(milestoneID)
		if parsed.Value == "" {
			return MilestoneHistoryLoadedMsg{Err: fmt.Errorf("invalid ref: %s", milestoneID)}
		}
		branch := parsed.Branch
		if branch == "" {
			branch = gitmsg.GetExtBranch(workdir, "pm")
		}
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		ref := protocol.CreateRef(protocol.RefTypeCommit, parsed.Value, parsed.Repository, branch)
		versions, err := gitmsg.GetHistory(ref, workspaceURL)
		if err != nil {
			return MilestoneHistoryLoadedMsg{Err: err}
		}
		return MilestoneHistoryLoadedMsg{Versions: versions}
	}
}

// MilestoneHistoryLoadedMsg signals that the milestone history has been loaded.
type MilestoneHistoryLoadedMsg struct {
	Versions []gitmsg.MessageVersion
	Err      error
}

// Update handles messages and returns commands.
func (v *MilestoneHistoryView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
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
	case MilestoneHistoryLoadedMsg:
		v.handleLoaded(msg)
	}
	return nil
}

// handleLoaded processes the loaded history data.
func (v *MilestoneHistoryView) handleLoaded(msg MilestoneHistoryLoadedMsg) {
	if msg.Err != nil {
		v.picker.SetLoading(false)
		return
	}
	items := make([]tuicore.VersionItem, len(msg.Versions))
	for i, version := range msg.Versions {
		items[i] = MilestoneVersionItem{Version: version, ShowEmail: v.showEmail}
	}
	v.picker.SetItems(items)
}

// Render renders the history view to a string.
func (v *MilestoneHistoryView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	content := v.picker.Render()
	footer := tuicore.RenderFooter(state.Registry, tuicore.PMMilestoneHistory, wrapper.ContentWidth(), nil)
	return wrapper.Render(content, footer)
}

// IsInputActive returns false since history view has no text input.
func (v *MilestoneHistoryView) IsInputActive() bool {
	return false
}

// Title returns the header title showing canonical milestone info.
func (v *MilestoneHistoryView) Title() string {
	items := v.picker.Items()
	if len(items) == 0 {
		return "History"
	}
	canonical := items[len(items)-1]
	version := canonical.(MilestoneVersionItem).Version
	name := version.AuthorName
	if name == "" {
		name = "Anonymous"
	}
	if v.showEmail && version.AuthorEmail != "" {
		name += " <" + version.AuthorEmail + ">"
	}
	title := "History · " + name
	title += " · " + tuicore.FormatFullTime(version.Timestamp)
	if ref := tuicore.BuildCommitRef(version.RepoURL, version.CommitHash, version.Branch, v.workspaceURL); ref != "" {
		title += " · " + ref
	}
	return title
}

// Bindings returns view-specific key bindings.
func (v *MilestoneHistoryView) Bindings() []tuicore.Binding {
	return nil
}
