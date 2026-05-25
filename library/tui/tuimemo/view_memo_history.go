// view_memo_history.go - Memo edit-history view (versions picker)
package tuimemo

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// MemoVersionItem wraps gitmsg.MessageVersion to implement tuicore.VersionItem.
type MemoVersionItem struct {
	Version   gitmsg.MessageVersion
	ShowEmail bool
}

// GetID returns the version's unique ref.
func (m MemoVersionItem) GetID() string { return m.Version.ID }

// GetTimestamp returns the version's commit time.
func (m MemoVersionItem) GetTimestamp() time.Time { return m.Version.Timestamp }

// GetEditOf returns the ID of the version this one edits, or "" for canonical.
func (m MemoVersionItem) GetEditOf() string { return m.Version.EditOf }

// IsRetracted reports whether this version is a retract commit.
func (m MemoVersionItem) IsRetracted() bool { return m.Version.IsRetracted }

// RenderListEntry renders a compact summary line for the picker: header on
// line 1 (version, label, hash, author, time), body excerpt on line 2, and
// labels on line 3 (when set).
func (m MemoVersionItem) RenderListEntry(index, total int, label string, selected bool, width int) string {
	hash, _ := protocol.NormalizeHash(protocol.ParseRef(m.Version.ID).Value)
	name := m.Version.AuthorName
	if name == "" {
		name = "Anonymous"
	}
	if m.ShowEmail && m.Version.AuthorEmail != "" {
		name += " <" + m.Version.AuthorEmail + ">"
	}
	header := fmt.Sprintf("Version %d (%s) - %s - %s - %s",
		total-index, label, hash, name, m.Version.Timestamp.Format("2006-01-02 15:04:05"))
	var b strings.Builder
	if selected {
		b.WriteString(tuicore.Highlight.Render("▶ " + header))
	} else {
		b.WriteString("  " + header)
	}
	b.WriteString("\n")
	if m.Version.IsRetracted {
		b.WriteString(tuicore.Dim.Render("    [retracted]"))
	} else {
		subj, _ := protocol.SplitSubjectBody(m.Version.Content)
		excerpt := strings.TrimSpace(subj)
		if excerpt == "" {
			excerpt = strings.TrimSpace(m.Version.Content)
		}
		if len(excerpt) > 100 {
			excerpt = excerpt[:100] + "..."
		}
		b.WriteString("    " + excerpt)
	}
	if len(m.Version.Labels) > 0 {
		b.WriteString("\n")
		b.WriteString("    " + tuicore.Dim.Render(strings.Join(m.Version.Labels, " · ")))
	}
	b.WriteString("\n")
	return b.String()
}

// RenderDetail renders the version's full content for the picker's detail
// pane: header (author, timestamp, ref), labels, then the markdown-rendered
// body. Mirrors the layout of the memo detail card so version inspection is
// visually consistent.
func (m MemoVersionItem) RenderDetail(width int) string {
	if m.Version.IsRetracted {
		return tuicore.Dim.Render("[retracted]")
	}
	wrap := width - 5
	if wrap < 20 {
		wrap = 20
	}
	subj, body := protocol.SplitSubjectBody(m.Version.Content)
	var lines []string
	if subj != "" {
		lines = append(lines, tuicore.Bold.Render(subj))
		lines = append(lines, tuicore.Dim.Render(strings.Repeat("─", wrap)))
	}
	name := m.Version.AuthorName
	if name == "" {
		name = "Anonymous"
	}
	if m.ShowEmail && m.Version.AuthorEmail != "" {
		name += " <" + m.Version.AuthorEmail + ">"
	}
	meta := fmt.Sprintf("%s · %s", name, m.Version.Timestamp.Format("2006-01-02 15:04:05"))
	hash, _ := protocol.NormalizeHash(protocol.ParseRef(m.Version.ID).Value)
	if hash != "" {
		meta += " · " + hash
	}
	lines = append(lines, tuicore.Dim.Render(meta))
	if len(m.Version.Labels) > 0 {
		lines = append(lines, tuicore.Dim.Render(strings.Join(m.Version.Labels, " · ")))
	}
	if body != "" {
		lines = append(lines, "")
		lines = append(lines, tuicore.RenderMarkdown(body, wrap))
	}
	return strings.Join(lines, "\n")
}

// MemoHistoryView displays the edit chain of a memo using the shared VersionPicker.
type MemoHistoryView struct {
	picker       *tuicore.VersionPicker
	workdir      string
	workspaceURL string
	showEmail    bool
	canonical    string // ID of the canonical (latest) memo for the title bar
}

// NewMemoHistoryView creates a new memo history view.
func NewMemoHistoryView(workdir string) *MemoHistoryView {
	return &MemoHistoryView{
		workdir:      workdir,
		workspaceURL: gitmsg.ResolveRepoURL(workdir),
		picker:       tuicore.NewVersionPicker(),
	}
}

// SetSize sets the view dimensions.
func (v *MemoHistoryView) SetSize(width, height int) { v.picker.SetSize(width, height) }

// Activate loads the edit history for the memo on the current route.
func (v *MemoHistoryView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	memoID := state.Router.Location().Params["memoID"]
	if memoID == "" {
		return nil
	}
	v.canonical = memoID
	v.picker.SetLoading(true)
	workdir := v.workdir
	return func() tea.Msg {
		versions, err := gitmsg.GetHistory(memoID, gitmsg.ResolveRepoURL(workdir))
		if err != nil {
			return memoHistoryLoadedMsg{err: err}
		}
		return memoHistoryLoadedMsg{versions: versions}
	}
}

// Update handles messages.
func (v *MemoHistoryView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch m := msg.(type) {
	case tea.MouseMsg:
		if handled, cmd := v.picker.HandleMouse(m); handled {
			return cmd
		}
	case tea.KeyPressMsg:
		if m.String() == "d" {
			return tuicore.OpenHistoryDiff(v.picker, state, "memoID", tuicore.LocMemoHistoryDiff, 1, nil)
		}
		if handled, cmd := v.picker.HandleKey(m.String()); handled {
			return cmd
		}
	case memoHistoryLoadedMsg:
		v.picker.SetLoading(false)
		if m.err != nil {
			return nil
		}
		items := make([]tuicore.VersionItem, len(m.versions))
		for i, ver := range m.versions {
			items[i] = MemoVersionItem{Version: ver, ShowEmail: v.showEmail}
		}
		v.picker.SetItems(items)
	}
	return nil
}

// Render renders the history picker to a string.
func (v *MemoHistoryView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	content := v.picker.Render()
	footer := tuicore.RenderFooter(state.Registry, tuicore.MemoHistory, wrapper.ContentWidth(), nil)
	return wrapper.Render(content, footer)
}

// Bindings returns view-specific keybindings.
func (v *MemoHistoryView) Bindings() []tuicore.Binding {
	noop := func(*tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []tuicore.Binding{
		{Key: "d", Label: "version diff", Contexts: []tuicore.Context{tuicore.MemoHistory}, Handler: noop},
	}
}

// IsInputActive returns false since the history view has no text input.
func (v *MemoHistoryView) IsInputActive() bool { return false }

// Title returns the panel header for the history view.
func (v *MemoHistoryView) Title() string {
	items := v.picker.Items()
	if len(items) == 0 {
		return "☞  History"
	}
	canonical := items[len(items)-1].(MemoVersionItem).Version
	name := canonical.AuthorName
	if name == "" {
		name = "Anonymous"
	}
	title := "☞  History · " + name + " · " + tuicore.FormatFullTime(canonical.Timestamp)
	hash := protocol.ParseRef(canonical.ID).Value
	if ref := tuicore.BuildCommitRef(canonical.RepoURL, hash, canonical.Branch, v.workspaceURL); ref != "" {
		title += " · " + ref
	}
	return title
}

type memoHistoryLoadedMsg struct {
	versions []gitmsg.MessageVersion
	err      error
}
