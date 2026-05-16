// view_release_history.go - Edit history view for releases
package tuirelease

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// ReleaseVersionItem wraps gitmsg.MessageVersion to implement tuicore.VersionItem.
type ReleaseVersionItem struct {
	Version   gitmsg.MessageVersion
	ShowEmail bool
}

// GetID returns the version's unique identifier.
func (v ReleaseVersionItem) GetID() string { return v.Version.ID }

// GetTimestamp returns the version's creation time.
func (v ReleaseVersionItem) GetTimestamp() time.Time { return v.Version.Timestamp }

// GetEditOf returns the ID of the item this version edits.
func (v ReleaseVersionItem) GetEditOf() string { return v.Version.EditOf }

// IsRetracted returns true if this version has been retracted.
func (v ReleaseVersionItem) IsRetracted() bool { return v.Version.IsRetracted }

// subject extracts the subject (first line) from the content.
func (v ReleaseVersionItem) subject() string {
	lines := strings.SplitN(v.Version.Content, "\n", 2)
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return ""
}

// body extracts the body (after first line) from the content.
func (v ReleaseVersionItem) body() string {
	lines := strings.SplitN(v.Version.Content, "\n", 2)
	if len(lines) > 1 {
		return strings.TrimSpace(lines[1])
	}
	return ""
}

// RenderListEntry renders a compact list entry for this version.
func (v ReleaseVersionItem) RenderListEntry(index, total int, label string, selected bool, width int) string {
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
func (v ReleaseVersionItem) RenderDetail(width int) string {
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
	card := tuicore.Card{
		Header: tuicore.CardHeader{
			Title:       name,
			Subtitle:    []tuicore.HeaderPart{{Text: tuicore.FormatFullTime(v.Version.Timestamp)}},
			IsRetracted: v.Version.IsRetracted,
		},
		Content: tuicore.CardContent{Text: content},
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

// ReleaseHistoryView displays edit history for a release.
type ReleaseHistoryView struct {
	picker       *tuicore.VersionPicker
	workdir      string
	workspaceURL string
	showEmail    bool
}

// NewReleaseHistoryView creates a new release history view.
func NewReleaseHistoryView(workdir string) *ReleaseHistoryView {
	return &ReleaseHistoryView{
		workdir:      workdir,
		workspaceURL: gitmsg.ResolveRepoURL(workdir),
		picker:       tuicore.NewVersionPicker(),
	}
}

// SetSize sets the view dimensions.
func (v *ReleaseHistoryView) SetSize(width, height int) {
	v.picker.SetSize(width, height)
}

// Activate loads the edit history when the view becomes active.
func (v *ReleaseHistoryView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	loc := state.Router.Location()
	releaseID := loc.Param("releaseID")
	if releaseID == "" {
		return nil
	}
	v.picker.SetLoading(true)
	return v.loadHistory(releaseID)
}

// loadHistory fetches the edit history for a release.
func (v *ReleaseHistoryView) loadHistory(releaseID string) tea.Cmd {
	workdir := v.workdir
	return func() tea.Msg {
		parsed := protocol.ParseRef(releaseID)
		if parsed.Value == "" {
			return ReleaseHistoryLoadedMsg{Err: fmt.Errorf("invalid ref: %s", releaseID)}
		}
		branch := parsed.Branch
		if branch == "" {
			branch = gitmsg.GetExtBranch(workdir, "release")
		}
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		ref := protocol.CreateRef(protocol.RefTypeCommit, parsed.Value, parsed.Repository, branch)
		versions, err := gitmsg.GetHistory(ref, workspaceURL)
		if err != nil {
			return ReleaseHistoryLoadedMsg{Err: err}
		}
		return ReleaseHistoryLoadedMsg{Versions: versions}
	}
}

// ReleaseHistoryLoadedMsg signals that the release history has been loaded.
type ReleaseHistoryLoadedMsg struct {
	Versions []gitmsg.MessageVersion
	Err      error
}

// Update handles messages and returns commands.
func (v *ReleaseHistoryView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		handled, cmd := v.picker.HandleMouse(msg)
		if handled {
			return cmd
		}
	case tea.KeyPressMsg:
		if msg.String() == "d" {
			return tuicore.OpenHistoryDiff(v.picker, state, "releaseID", tuicore.LocReleaseHistoryDiff, 1, nil)
		}
		handled, cmd := v.picker.HandleKey(msg.String())
		if handled {
			return cmd
		}
	case ReleaseHistoryLoadedMsg:
		v.handleLoaded(msg)
	}
	return nil
}

// handleLoaded processes the loaded history data.
func (v *ReleaseHistoryView) handleLoaded(msg ReleaseHistoryLoadedMsg) {
	if msg.Err != nil {
		v.picker.SetLoading(false)
		return
	}
	items := make([]tuicore.VersionItem, len(msg.Versions))
	for i, version := range msg.Versions {
		items[i] = ReleaseVersionItem{Version: version, ShowEmail: v.showEmail}
	}
	v.picker.SetItems(items)
}

// Render renders the history view to a string.
func (v *ReleaseHistoryView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	content := v.picker.Render()
	footer := tuicore.RenderFooter(state.Registry, tuicore.ReleaseHistory, wrapper.ContentWidth(), nil)
	return wrapper.Render(content, footer)
}

// IsInputActive returns false since the history view has no text input.
func (v *ReleaseHistoryView) IsInputActive() bool { return false }

// Title returns the header title showing canonical release info.
func (v *ReleaseHistoryView) Title() string {
	items := v.picker.Items()
	if len(items) == 0 {
		return "History"
	}
	canonical := items[len(items)-1]
	version := canonical.(ReleaseVersionItem).Version
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
func (v *ReleaseHistoryView) Bindings() []tuicore.Binding {
	noop := func(*tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []tuicore.Binding{
		{Key: "d", Label: "version diff", Contexts: []tuicore.Context{tuicore.ReleaseHistory}, Handler: noop},
	}
}
