// issue_history.go - Edit history view for PM issues
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

// IssueVersionItem wraps gitmsg.MessageVersion to implement tuicore.VersionItem.
type IssueVersionItem struct {
	Version     gitmsg.MessageVersion
	ShowEmail   bool
	ProposalTag string
}

// GetID returns the version's unique identifier.
func (v IssueVersionItem) GetID() string {
	return v.Version.ID
}

// GetTimestamp returns the version's creation time.
func (v IssueVersionItem) GetTimestamp() time.Time {
	return v.Version.Timestamp
}

// GetEditOf returns the ID of the item this version edits.
func (v IssueVersionItem) GetEditOf() string {
	return v.Version.EditOf
}

// IsRetracted returns true if this version has been retracted.
func (v IssueVersionItem) IsRetracted() bool {
	return v.Version.IsRetracted
}

// subject extracts the subject (first line) from the content.
func (v IssueVersionItem) subject() string {
	lines := strings.SplitN(v.Version.Content, "\n", 2)
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return ""
}

// body extracts the body (after first line) from the content.
func (v IssueVersionItem) body() string {
	lines := strings.SplitN(v.Version.Content, "\n", 2)
	if len(lines) > 1 {
		return strings.TrimSpace(lines[1])
	}
	return ""
}

// RenderListEntry renders a compact list entry for this version.
func (v IssueVersionItem) RenderListEntry(index, total int, label string, selected bool, width int) string {
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
func (v IssueVersionItem) RenderDetail(width int) string {
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

// IssueHistoryView displays edit history for a PM issue.
type IssueHistoryView struct {
	picker       *tuicore.VersionPicker
	workdir      string
	workspaceURL string
	showEmail    bool
	owned        bool
}

// NewIssueHistoryView creates a new issue history view.
func NewIssueHistoryView(workdir string) *IssueHistoryView {
	return &IssueHistoryView{
		workdir:      workdir,
		workspaceURL: gitmsg.ResolveRepoURL(workdir),
		picker:       tuicore.NewVersionPicker(),
	}
}

// SetSize sets the view dimensions.
func (v *IssueHistoryView) SetSize(width, height int) {
	v.picker.SetSize(width, height)
}

// Activate loads the edit history when the view becomes active.
func (v *IssueHistoryView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	loc := state.Router.Location()
	issueID := loc.Param("issueID")
	if issueID == "" {
		return nil
	}
	v.owned = tuicore.OwnsCanonical(issueID, v.workspaceURL)
	v.picker.SetLoading(true)
	return v.loadHistory(issueID)
}

// loadHistory fetches the edit history for an issue.
func (v *IssueHistoryView) loadHistory(issueID string) tea.Cmd {
	workdir := v.workdir
	return func() tea.Msg {
		parsed := protocol.ParseRef(issueID)
		if parsed.Value == "" {
			return IssueHistoryLoadedMsg{Err: fmt.Errorf("invalid ref: %s", issueID)}
		}
		branch := parsed.Branch
		if branch == "" {
			branch = gitmsg.GetExtBranch(workdir, "pm")
		}
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		ref := protocol.CreateRef(protocol.RefTypeCommit, parsed.Value, parsed.Repository, branch)
		versions, err := gitmsg.GetHistory(ref, workspaceURL)
		if err != nil {
			return IssueHistoryLoadedMsg{Err: err}
		}
		return IssueHistoryLoadedMsg{Versions: versions}
	}
}

// IssueHistoryLoadedMsg signals that the issue history has been loaded.
type IssueHistoryLoadedMsg struct {
	Versions []gitmsg.MessageVersion
	Err      error
}

// Update handles messages and returns commands.
func (v *IssueHistoryView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		handled, cmd := v.picker.HandleMouse(msg)
		if handled {
			return cmd
		}
	case tea.KeyPressMsg:
		if msg.String() == "d" {
			return tuicore.OpenHistoryDiff(v.picker, state, "issueID", tuicore.LocPMIssueHistoryDiff, 1, nil)
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
	case IssueHistoryLoadedMsg:
		v.handleLoaded(msg)
	}
	return nil
}

// handleLoaded processes the loaded history data.
func (v *IssueHistoryView) handleLoaded(msg IssueHistoryLoadedMsg) {
	if msg.Err != nil {
		v.picker.SetLoading(false)
		return
	}
	items := make([]tuicore.VersionItem, len(msg.Versions))
	for i, version := range msg.Versions {
		items[i] = IssueVersionItem{Version: version, ShowEmail: v.showEmail,
			ProposalTag: tuicore.ProposalTag(v.owned, v.workspaceURL, version.RepoURL, version.CommitHash, version.Branch)}
	}
	v.picker.SetItems(items)
}

// acceptInclude force-shows "A accept" only when this workspace owns the issue
// and a cross-repo proposal is present (not the proposer's "awaiting owner" view).
func (v *IssueHistoryView) acceptInclude() map[string]bool {
	for _, it := range v.picker.Items() {
		if iv, ok := it.(IssueVersionItem); ok && tuicore.IsOpenProposalTag(iv.ProposalTag) {
			return map[string]bool{"A": true, "X": true}
		}
	}
	return nil
}

// acceptSelected accepts the selected version when it is a cross-repo proposed
// edit on an issue this workspace owns, authoring an authoritative same-repo mirror.
func (v *IssueHistoryView) acceptSelected() tea.Cmd {
	sel := v.picker.SelectedItem()
	iv, ok := sel.(IssueVersionItem)
	if !ok {
		return nil
	}
	if iv.Version.RepoURL == v.workspaceURL {
		return func() tea.Msg {
			return tuicore.ProposalAcceptedMsg{Err: fmt.Errorf("select a proposed edit from another repo to accept")}
		}
	}
	ref := protocol.CreateRef(protocol.RefTypeCommit, iv.Version.CommitHash, iv.Version.RepoURL, iv.Version.Branch)
	workdir := v.workdir
	return func() tea.Msg {
		out := proposals.Accept(workdir, ref)
		if !out.Success {
			return tuicore.ProposalAcceptedMsg{Err: fmt.Errorf("%s", out.Error.Message)}
		}
		return tuicore.ProposalAcceptedMsg{Location: tuicore.LocPMIssueDetail(out.Data.CanonicalRef)}
	}
}

// declineSelected publishes a durable decline for the selected cross-repo proposed
// edit on an issue this workspace owns, so the proposer learns and it stops nagging.
func (v *IssueHistoryView) declineSelected() tea.Cmd {
	sel := v.picker.SelectedItem()
	iv, ok := sel.(IssueVersionItem)
	if !ok {
		return nil
	}
	if iv.Version.RepoURL == v.workspaceURL {
		return func() tea.Msg {
			return tuicore.ProposalAcceptedMsg{Err: fmt.Errorf("select a proposed edit from another repo to decline")}
		}
	}
	ref := protocol.CreateRef(protocol.RefTypeCommit, iv.Version.CommitHash, iv.Version.RepoURL, iv.Version.Branch)
	workdir := v.workdir
	return func() tea.Msg {
		out := proposals.Decline(workdir, ref)
		if !out.Success {
			return tuicore.ProposalAcceptedMsg{Err: fmt.Errorf("%s", out.Error.Message)}
		}
		return tuicore.ProposalAcceptedMsg{Declined: true, Location: tuicore.LocPMIssueDetail(out.Data.CanonicalRef)}
	}
}

// Render renders the history view to a string.
func (v *IssueHistoryView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	content := v.picker.Render()
	footer := tuicore.RenderFooterInclude(state.Registry, tuicore.PMIssueHistory, nil, v.acceptInclude())
	return wrapper.Render(content, footer)
}

// IsInputActive returns false since history view has no text input.
func (v *IssueHistoryView) IsInputActive() bool {
	return false
}

// Title returns the header title showing canonical issue info.
func (v *IssueHistoryView) Title() string {
	items := v.picker.Items()
	if len(items) == 0 {
		return "History"
	}
	canonical := items[len(items)-1]
	version := canonical.(IssueVersionItem).Version
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
func (v *IssueHistoryView) Bindings() []tuicore.Binding {
	noop := func(*tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []tuicore.Binding{
		{Key: "d", Label: "version diff", Contexts: []tuicore.Context{tuicore.PMIssueHistory}, Handler: noop},
		{Key: "A", Label: "accept", Contexts: []tuicore.Context{tuicore.PMIssueHistory}, Handler: noop},
		{Key: "X", Label: "decline", Contexts: []tuicore.Context{tuicore.PMIssueHistory}, Handler: noop},
	}
}
