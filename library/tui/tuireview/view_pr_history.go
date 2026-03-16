// view_pr_history.go - Version history view for pull requests
package tuireview

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// PRVersionItem wraps review.PRVersion to implement tuicore.VersionItem.
type PRVersionItem struct {
	Version   review.PRVersion
	ShowEmail bool
}

// GetID returns the version's commit hash as unique identifier.
func (v PRVersionItem) GetID() string {
	return v.Version.CommitHash
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
		handled, cmd := v.picker.HandleKey(msg.String())
		if handled {
			return cmd
		}
	case prHistoryLoadedMsg:
		if msg.Err != nil {
			v.picker.SetLoading(false)
			return nil
		}
		items := make([]tuicore.VersionItem, len(msg.Versions))
		for i, version := range msg.Versions {
			items[i] = PRVersionItem{Version: version, ShowEmail: v.showEmail}
		}
		v.picker.SetItems(items)
	}
	return nil
}

// Render renders the history view.
func (v *PRHistoryView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	content := v.picker.Render()
	footer := tuicore.RenderFooter(state.Registry, tuicore.ReviewPRHistory, wrapper.ContentWidth(), nil)
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

// Bindings returns view-specific key bindings.
func (v *PRHistoryView) Bindings() []tuicore.Binding {
	return nil
}
