// view_pr_history.go - Version history view for pull requests
package tuireview

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/proposals"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// PRVersionItem wraps review.PRVersion to implement tuicore.VersionItem.
type PRVersionItem struct {
	Version     review.PRVersion
	ShowEmail   bool
	ProposalTag string
}

// GetID returns the version's commit ref, matching the IDs the history-diff
// loader emits via gitmsg.GetHistory so the diff route resolves the pair.
func (v PRVersionItem) GetID() string {
	return protocol.CreateRef(protocol.RefTypeCommit, v.Version.CommitHash, v.Version.RepoURL, v.Version.Branch)
}

// GetTimestamp returns the version's creation time.
func (v PRVersionItem) GetTimestamp() time.Time {
	return v.Version.Timestamp
}

// GetEditOf returns empty since PRVersions are a flat list.
func (v PRVersionItem) GetEditOf() string {
	return ""
}

// IsRetracted returns true if this version has been retracted.
func (v PRVersionItem) IsRetracted() bool {
	return v.Version.IsRetracted
}

// RenderListEntry renders a compact table row for this version.
func (v PRVersionItem) RenderListEntry(index, total int, label string, selected bool, width int) string {
	baseTip := v.Version.BaseTip
	if baseTip == "" {
		baseTip = "—"
	}
	headTip := v.Version.HeadTip
	if headTip == "" {
		headTip = "—"
	}
	name := v.Version.AuthorName
	if name == "" {
		name = "Anonymous"
	}
	if v.ShowEmail && v.Version.AuthorEmail != "" {
		name += " <" + v.Version.AuthorEmail + ">"
	}
	stateStr := ""
	if v.Version.State != "" && v.Version.State != review.PRStateOpen {
		stateStr = "  " + string(v.Version.State)
	}
	header := fmt.Sprintf("#%d  %s  %s  %s  %s  %s%s",
		v.Version.Number, label, baseTip, headTip, name,
		v.Version.Timestamp.Format("2006-01-02 15:04"), stateStr)
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
		b.WriteString("\n")
	}
	return b.String()
}

// RenderDetail renders the full detail view for this version.
func (v PRVersionItem) RenderDetail(width int) string {
	name := v.Version.AuthorName
	if name == "" {
		name = "Anonymous"
	}
	if v.ShowEmail && v.Version.AuthorEmail != "" {
		name += " <" + v.Version.AuthorEmail + ">"
	}
	styles := tuicore.RowStylesWithWidths(14, 0)
	var lines []string
	lines = append(lines, tuicore.Bold.Render(fmt.Sprintf("Version #%d (%s)", v.Version.Number, v.Version.Label)))
	lines = append(lines, "")
	lines = append(lines, styles.Label.Render("Author")+styles.Value.Render(name))
	lines = append(lines, styles.Label.Render("Date")+styles.Value.Render(tuicore.FormatFullTime(v.Version.Timestamp)))
	commitURL := protocol.CommitURL(v.Version.RepoURL, v.Version.CommitHash)
	commitDisplay := tuicore.Hyperlink(commitURL, v.Version.CommitHash)
	lines = append(lines, styles.Label.Render("Commit")+commitDisplay)
	if v.Version.BaseTip != "" {
		lines = append(lines, styles.Label.Render("Base-Tip")+tuicore.Dim.Render(v.Version.BaseTip))
	}
	if v.Version.HeadTip != "" {
		lines = append(lines, styles.Label.Render("Head-Tip")+tuicore.Dim.Render(v.Version.HeadTip))
	}
	if v.Version.State != "" {
		lines = append(lines, styles.Label.Render("State")+styles.Value.Render(string(v.Version.State)))
	}
	if v.Version.IsRetracted {
		lines = append(lines, "", tuicore.Dim.Render("[deleted]"))
	}
	return strings.Join(lines, "\n")
}

// PRHistoryView displays version history for a pull request.
type PRHistoryView struct {
	picker       *tuicore.VersionPicker
	workdir      string
	workspaceURL string
	showEmail    bool
	owned        bool
}

// NewPRHistoryView creates a new PR history view.
func NewPRHistoryView(workdir string) *PRHistoryView {
	return &PRHistoryView{
		workdir:      workdir,
		workspaceURL: gitmsg.ResolveRepoURL(workdir),
		picker:       tuicore.NewVersionPicker(),
	}
}

// SetSize sets the view dimensions.
func (v *PRHistoryView) SetSize(width, height int) {
	v.picker.SetSize(width, height)
}

// Activate loads the version history when the view becomes active.
func (v *PRHistoryView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	prID := state.Router.Location().Param("prID")
	if prID == "" {
		return nil
	}
	v.owned = tuicore.OwnsCanonical(prID, v.workspaceURL)
	v.picker.SetLoading(true)
	workspaceURL := v.workspaceURL
	return func() tea.Msg {
		res := review.GetPRVersions(prID, workspaceURL)
		if !res.Success {
			return prHistoryLoadedMsg{Err: fmt.Errorf("%s", res.Error.Message)}
		}
		return prHistoryLoadedMsg{Versions: res.Data}
	}
}

// Deactivate is called when the view is hidden.
func (v *PRHistoryView) Deactivate() {}

type prHistoryLoadedMsg struct {
	Versions []review.PRVersion
	Err      error
}

// Update handles messages and returns commands.
func (v *PRHistoryView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		handled, cmd := v.picker.HandleMouse(msg)
		if handled {
			return cmd
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "d":
			return v.openDescriptionDiff(state)
		case "i":
			return v.openInterdiff(state)
		case "A":
			return v.acceptSelected()
		case "X":
			return v.declineSelected()
		}
		handled, cmd := v.picker.HandleKey(msg.String())
		if handled {
			return cmd
		}
	case prHistoryLoadedMsg:
		if msg.Err != nil {
			v.picker.SetLoading(false)
			return nil
		}
		// GetPRVersions returns ASC (oldest first), but the picker's labels and
		// diff navigation assume DESC (newest first) like every other history
		// view, so place the newest version at index 0.
		items := make([]tuicore.VersionItem, len(msg.Versions))
		for i, version := range msg.Versions {
			items[len(msg.Versions)-1-i] = PRVersionItem{Version: version, ShowEmail: v.showEmail,
				ProposalTag: tuicore.ProposalTag(v.owned, v.workspaceURL, version.RepoURL, version.CommitHash, version.Branch)}
		}
		v.picker.SetItems(items)
	}
	return nil
}

// Render renders the history view.
func (v *PRHistoryView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	content := v.picker.Render()
	footer := tuicore.RenderFooterInclude(state.Registry, tuicore.ReviewPRHistory, nil, v.acceptInclude())
	return wrapper.Render(content, footer)
}

// IsInputActive returns false since history view has no text input.
func (v *PRHistoryView) IsInputActive() bool {
	return false
}

// Title returns the header title.
func (v *PRHistoryView) Title() string {
	items := v.picker.Items()
	if len(items) == 0 {
		return "Version History"
	}
	canonical := items[len(items)-1]
	version := canonical.(PRVersionItem).Version
	name := version.AuthorName
	if name == "" {
		name = "Anonymous"
	}
	if v.showEmail && version.AuthorEmail != "" {
		name += " <" + version.AuthorEmail + ">"
	}
	title := "Version History · " + name
	title += " · " + tuicore.FormatFullTime(version.Timestamp)
	if ref := tuicore.BuildCommitRef(version.RepoURL, version.CommitHash, version.Branch, v.workspaceURL); ref != "" {
		title += " · " + ref
	}
	return title
}

// openDescriptionDiff navigates to the PR description-text diff for the cursor pair.
// Picker items are DESC (newest first), so the older neighbor is at offset +1.
// Item GetID() and the diff loader both produce commit refs, so the default
// itemID mapper resolves the pair.
func (v *PRHistoryView) openDescriptionDiff(state *tuicore.State) tea.Cmd {
	return tuicore.OpenHistoryDiff(v.picker, state, "prID", tuicore.LocReviewPRHistoryDiff, 1, nil)
}

// acceptInclude force-shows "A accept" in the footer only when this workspace
// owns the PR (so the proposer's "awaiting the owner" view doesn't advertise it)
// and the picker holds a cross-repo proposal to accept.
func (v *PRHistoryView) acceptInclude() map[string]bool {
	for _, it := range v.picker.Items() {
		if iv, ok := it.(PRVersionItem); ok && tuicore.IsOpenProposalTag(iv.ProposalTag) {
			return map[string]bool{"A": true, "X": true}
		}
	}
	return nil
}

// acceptSelected accepts the selected version when it is a cross-repo proposed
// edit on a PR this workspace owns, authoring an authoritative same-repo mirror.
func (v *PRHistoryView) acceptSelected() tea.Cmd {
	sel := v.picker.SelectedItem()
	pv, ok := sel.(PRVersionItem)
	if !ok {
		return nil
	}
	if pv.Version.RepoURL == v.workspaceURL {
		return func() tea.Msg {
			return tuicore.ProposalAcceptedMsg{Err: fmt.Errorf("select a proposed edit from another repo to accept")}
		}
	}
	ref := protocol.CreateRef(protocol.RefTypeCommit, pv.Version.CommitHash, pv.Version.RepoURL, pv.Version.Branch)
	workdir := v.workdir
	return func() tea.Msg {
		out := proposals.Accept(workdir, ref)
		if !out.Success {
			return tuicore.ProposalAcceptedMsg{Err: fmt.Errorf("%s", out.Error.Message)}
		}
		return tuicore.ProposalAcceptedMsg{Location: tuicore.LocReviewPRDetail(out.Data.CanonicalRef)}
	}
}

// declineSelected publishes a durable decline for the selected cross-repo proposed
// edit on a PR this workspace owns, so the proposer learns and it stops nagging.
func (v *PRHistoryView) declineSelected() tea.Cmd {
	sel := v.picker.SelectedItem()
	pv, ok := sel.(PRVersionItem)
	if !ok {
		return nil
	}
	if pv.Version.RepoURL == v.workspaceURL {
		return func() tea.Msg {
			return tuicore.ProposalAcceptedMsg{Err: fmt.Errorf("select a proposed edit from another repo to decline")}
		}
	}
	ref := protocol.CreateRef(protocol.RefTypeCommit, pv.Version.CommitHash, pv.Version.RepoURL, pv.Version.Branch)
	workdir := v.workdir
	return func() tea.Msg {
		out := proposals.Decline(workdir, ref)
		if !out.Success {
			return tuicore.ProposalAcceptedMsg{Err: fmt.Errorf("%s", out.Error.Message)}
		}
		return tuicore.ProposalAcceptedMsg{Declined: true, Location: tuicore.LocReviewPRDetail(out.Data.CanonicalRef)}
	}
}

// openInterdiff navigates to the existing PR range-diff (interdiff) view.
func (v *PRHistoryView) openInterdiff(state *tuicore.State) tea.Cmd {
	prID := state.Router.Location().Param("prID")
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocReviewInterdiff(prID),
			Action:   tuicore.NavPush,
		}
	}
}

// Bindings returns view-specific key bindings.
func (v *PRHistoryView) Bindings() []tuicore.Binding {
	noop := func(*tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "d", Label: "version diff", Contexts: []tuicore.Context{tuicore.ReviewPRHistory}, Handler: noop},
		{Key: "i", Label: "interdiff", Contexts: []tuicore.Context{tuicore.ReviewPRHistory}, Handler: noop},
		{Key: "A", Label: "accept", Contexts: []tuicore.Context{tuicore.ReviewPRHistory}, Handler: noop},
		{Key: "X", Label: "decline", Contexts: []tuicore.Context{tuicore.ReviewPRHistory}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.ReviewPRHistory}, Handler: push},
	}
}
