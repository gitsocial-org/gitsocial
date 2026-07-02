// sprint_history.go - Edit history view for PM sprints
package tuipm

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/proposals"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// SprintVersionItem wraps gitmsg.MessageVersion to implement tuicore.VersionItem.
type SprintVersionItem struct {
	Version     gitmsg.MessageVersion
	ShowEmail   bool
	ProposalTag string
}

// GetID returns the version's unique identifier.
func (v SprintVersionItem) GetID() string {
	return v.Version.ID
}

// GetTimestamp returns the version's creation time.
func (v SprintVersionItem) GetTimestamp() time.Time {
	return v.Version.Timestamp
}

// GetEditOf returns the ID of the item this version edits.
func (v SprintVersionItem) GetEditOf() string {
	return v.Version.EditOf
}

// IsRetracted returns true if this version has been retracted.
func (v SprintVersionItem) IsRetracted() bool {
	return v.Version.IsRetracted
}

// title extracts the title (first line) from the content.
func (v SprintVersionItem) title() string {
	lines := strings.SplitN(v.Version.Content, "\n", 2)
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return ""
}

// body extracts the body (after first line) from the content.
func (v SprintVersionItem) body() string {
	lines := strings.SplitN(v.Version.Content, "\n", 2)
	if len(lines) > 1 {
		return strings.TrimSpace(lines[1])
	}
	return ""
}

// RenderListEntry renders a compact list entry for this version.
func (v SprintVersionItem) RenderListEntry(index, total int, label string, selected bool, width int) string {
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
	b.WriteString(tuicore.RenderProposalTag(v.ProposalTag))
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
func (v SprintVersionItem) RenderDetail(width int) string {
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

// SprintHistoryView displays edit history for a PM sprint.
type SprintHistoryView struct {
	picker       *tuicore.VersionPicker
	workdir      string
	workspaceURL string
	showEmail    bool
	owned        bool
}

// NewSprintHistoryView creates a new sprint history view.
func NewSprintHistoryView(workdir string) *SprintHistoryView {
	return &SprintHistoryView{
		workdir:      workdir,
		workspaceURL: gitmsg.ResolveRepoURL(workdir),
		picker:       tuicore.NewVersionPicker(),
	}
}

// SetSize sets the view dimensions.
func (v *SprintHistoryView) SetSize(width, height int) {
	v.picker.SetSize(width, height)
}

// Activate loads the edit history when the view becomes active.
func (v *SprintHistoryView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	loc := state.Router.Location()
	sprintID := loc.Param("sprintID")
	if sprintID == "" {
		return nil
	}
	v.owned = tuicore.OwnsCanonical(sprintID, v.workspaceURL)
	v.picker.SetLoading(true)
	return v.loadHistory(sprintID)
}

// loadHistory fetches the edit history for a sprint.
func (v *SprintHistoryView) loadHistory(sprintID string) tea.Cmd {
	workdir := v.workdir
	return func() tea.Msg {
		parsed := protocol.ParseRef(sprintID)
		if parsed.Value == "" {
			return SprintHistoryLoadedMsg{Err: fmt.Errorf("invalid ref: %s", sprintID)}
		}
		branch := parsed.Branch
		if branch == "" {
			branch = gitmsg.GetExtBranch(workdir, "pm")
		}
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		ref := protocol.CreateRef(protocol.RefTypeCommit, parsed.Value, parsed.Repository, branch)
		versions, err := gitmsg.GetHistory(ref, workspaceURL)
		if err != nil {
			return SprintHistoryLoadedMsg{Err: err}
		}
		return SprintHistoryLoadedMsg{Versions: versions}
	}
}

// SprintHistoryLoadedMsg signals that the sprint history has been loaded.
type SprintHistoryLoadedMsg struct {
	Versions []gitmsg.MessageVersion
	Err      error
}

// Update handles messages and returns commands.
func (v *SprintHistoryView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		handled, cmd := v.picker.HandleMouse(msg)
		if handled {
			return cmd
		}
	case tea.KeyPressMsg:
		if msg.String() == "d" {
			return tuicore.OpenHistoryDiff(v.picker, state, "sprintID", tuicore.LocPMSprintHistoryDiff, 1, nil)
		}
		if msg.String() == "A" {
			return v.acceptSelected()
		}
		if msg.String() == "X" {
			return v.declineSelected()
		}
		handled, cmd := v.picker.HandleKey(msg.String())
		if handled {
			return cmd
		}
	case SprintHistoryLoadedMsg:
		v.handleLoaded(msg)
	}
	return nil
}

// handleLoaded processes the loaded history data.
func (v *SprintHistoryView) handleLoaded(msg SprintHistoryLoadedMsg) {
	if msg.Err != nil {
		v.picker.SetLoading(false)
		return
	}
	items := make([]tuicore.VersionItem, len(msg.Versions))
	for i, version := range msg.Versions {
		items[i] = SprintVersionItem{Version: version, ShowEmail: v.showEmail,
			ProposalTag: tuicore.ProposalTag(v.owned, v.workspaceURL, version.RepoURL, version.CommitHash, version.Branch)}
	}
	v.picker.SetItems(items)
}

// acceptInclude force-shows "A accept" only when this workspace owns the sprint
// and a cross-repo proposal is present.
func (v *SprintHistoryView) acceptInclude() map[string]bool {
	for _, it := range v.picker.Items() {
		if iv, ok := it.(SprintVersionItem); ok && tuicore.IsOpenProposalTag(iv.ProposalTag) {
			return map[string]bool{"A": true, "X": true}
		}
	}
	return nil
}

// acceptSelected accepts the selected version when it is a cross-repo proposed
// edit on a sprint this workspace owns, authoring an authoritative mirror.
func (v *SprintHistoryView) acceptSelected() tea.Cmd {
	sel := v.picker.SelectedItem()
	sv, ok := sel.(SprintVersionItem)
	if !ok {
		return nil
	}
	if sv.Version.RepoURL == v.workspaceURL {
		return func() tea.Msg {
			return tuicore.ProposalAcceptedMsg{Err: fmt.Errorf("select a proposed edit from another repo to accept")}
		}
	}
	ref := protocol.CreateRef(protocol.RefTypeCommit, sv.Version.CommitHash, sv.Version.RepoURL, sv.Version.Branch)
	workdir := v.workdir
	return func() tea.Msg {
		out := proposals.Accept(workdir, ref)
		if !out.Success {
			return tuicore.ProposalAcceptedMsg{Err: fmt.Errorf("%s", out.Error.Message)}
		}
		return tuicore.ProposalAcceptedMsg{Location: tuicore.LocPMSprintDetail(out.Data.CanonicalRef)}
	}
}

// declineSelected publishes a durable decline for the selected cross-repo proposed
// edit on a sprint this workspace owns, so the proposer learns and it stops nagging.
func (v *SprintHistoryView) declineSelected() tea.Cmd {
	sel := v.picker.SelectedItem()
	sv, ok := sel.(SprintVersionItem)
	if !ok {
		return nil
	}
	if sv.Version.RepoURL == v.workspaceURL {
		return func() tea.Msg {
			return tuicore.ProposalAcceptedMsg{Err: fmt.Errorf("select a proposed edit from another repo to decline")}
		}
	}
	ref := protocol.CreateRef(protocol.RefTypeCommit, sv.Version.CommitHash, sv.Version.RepoURL, sv.Version.Branch)
	workdir := v.workdir
	return func() tea.Msg {
		out := proposals.Decline(workdir, ref)
		if !out.Success {
			return tuicore.ProposalAcceptedMsg{Err: fmt.Errorf("%s", out.Error.Message)}
		}
		return tuicore.ProposalAcceptedMsg{Declined: true, Location: tuicore.LocPMSprintDetail(out.Data.CanonicalRef)}
	}
}

// Render renders the history view to a string.
func (v *SprintHistoryView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	content := v.picker.Render()
	footer := tuicore.RenderFooterInclude(state.Registry, tuicore.PMSprintHistory, nil, v.acceptInclude())
	return wrapper.Render(content, footer)
}

// IsInputActive returns false since history view has no text input.
func (v *SprintHistoryView) IsInputActive() bool {
	return false
}

// Title returns the header title showing canonical sprint info.
func (v *SprintHistoryView) Title() string {
	items := v.picker.Items()
	if len(items) == 0 {
		return "History"
	}
	canonical := items[len(items)-1]
	version := canonical.(SprintVersionItem).Version
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
func (v *SprintHistoryView) Bindings() []tuicore.Binding {
	noop := func(*tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []tuicore.Binding{
		{Key: "d", Label: "version diff", Contexts: []tuicore.Context{tuicore.PMSprintHistory}, Handler: noop},
		{Key: "A", Label: "accept", Contexts: []tuicore.Context{tuicore.PMSprintHistory}, Handler: noop},
		{Key: "X", Label: "decline", Contexts: []tuicore.Context{tuicore.PMSprintHistory}, Handler: noop},
	}
}
