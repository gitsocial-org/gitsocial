// view_changes.go - Changes view: the status rows of the workspace, the diff of the selected file, the index writes and the commit of the index
package tuisocial

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// changesView lists the changed and untracked files of the workspace and shows the diff of the selected one under them.
type changesView struct {
	core       *tuicore.DiffViewCore
	entries    []git.StatusEntry
	cursor     int
	keepPath   string
	loading    bool
	loadErr    error
	width      int
	height     int
	zonePrefix string
}

// newChangesView creates the changes view of a workspace.
func newChangesView(workdir string) *changesView {
	v := &changesView{core: tuicore.NewDiffViewCore(workdir), zonePrefix: zone.NewPrefix()}
	v.core.SetExtraKey(v.extraKey)
	return v
}

// SetSize keeps the rows of the list above the diff.
func (v *changesView) SetSize(w, h int) {
	v.width, v.height = w, h
	v.core.SetSize(w, max(1, h-v.listRows()-1))
}

// listRows is the height of the file list: every row up to a third of the panel, at least three.
func (v *changesView) listRows() int {
	limit := max(3, v.height/3)
	return min(max(1, len(v.entries)), limit)
}

// IsInputActive returns true while the diff search takes input.
func (v *changesView) IsInputActive() bool { return v.core.IsInputActive() }

// Bindings returns the file keys, the index keys, the commit and the shared diff keys.
func (v *changesView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	contexts := []tuicore.Context{tuicore.Changes}
	own := []tuicore.Binding{
		{Key: "j", Label: "next file", Contexts: contexts, Handler: noop},
		{Key: "k", Label: "prev file", Contexts: contexts, Handler: noop},
		{Key: "s", Label: "stage", Contexts: contexts, Handler: noop},
		{Key: "u", Label: "unstage", Contexts: contexts, Handler: noop},
		{Key: "S/U", Label: "stage/unstage all", Contexts: contexts, Handler: noop},
		{Key: "C", Label: "commit", Contexts: contexts, Handler: noop},
	}
	for _, b := range v.core.SharedBindings(tuicore.Changes) {
		if b.Key != "j" && b.Key != "k" && b.Key != "tab" && b.Key != "shift+tab" {
			own = append(own, b)
		}
	}
	return own
}

// Activate reads the status of the workspace.
func (v *changesView) Activate(state *tuicore.State) tea.Cmd {
	v.core.Reset()
	v.entries, v.cursor, v.keepPath = nil, 0, ""
	return v.load()
}

// load reads the status rows; the selected path is kept across the reload.
func (v *changesView) load() tea.Cmd {
	v.loading, v.loadErr = true, nil
	if e, ok := v.selected(); ok {
		v.keepPath = e.Path
	}
	workdir := v.core.Workdir()
	return func() tea.Msg {
		entries, err := git.WorkingStatus(workdir)
		return changesLoadedMsg{Entries: entries, Err: err}
	}
}

// loadDiff reads the diff of the selected file: the staged diff, then the unstaged one, or the content of an untracked file.
func (v *changesView) loadDiff() tea.Cmd {
	entry, ok := v.selected()
	if !ok {
		v.core.LoadDiffs(nil, git.DiffStats{})
		return nil
	}
	workdir := v.core.Workdir()
	return func() tea.Msg {
		msg := fileDiffLoadedMsg{Path: entry.Path}
		if entry.Untracked() {
			d, err := git.UntrackedFileDiff(workdir, entry.Path)
			if err != nil {
				return fileDiffLoadedMsg{Path: entry.Path, Err: err}
			}
			msg.Diffs = []git.FileDiff{*d}
			return msg
		}
		for _, side := range []struct {
			staged bool
			label  string
		}{{true, "staged"}, {false, "unstaged"}} {
			d, err := git.GetWorkingFileDiff(workdir, entry.Path, side.staged)
			if err != nil {
				return fileDiffLoadedMsg{Path: entry.Path, Err: err}
			}
			if d != nil {
				// The file heading of the diff names the side, so the user sees what the commit takes.
				d.NewPath = side.label + ": " + d.NewPath
				msg.Diffs = append(msg.Diffs, *d)
			}
		}
		return msg
	}
}

// Update takes the loaded status, the loaded diff and the index results, else hands the message to the diff core.
func (v *changesView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch m := msg.(type) {
	case changesLoadedMsg:
		v.loading, v.loadErr = false, m.Err
		if m.Err != nil {
			state.SetMessage(m.Err.Error(), tuicore.MessageTypeError)
			return nil
		}
		v.entries = m.Entries
		v.cursor = 0
		for i, e := range m.Entries {
			if e.Path == v.keepPath {
				v.cursor = i
			}
		}
		v.SetSize(v.width, v.height)
		return v.loadDiff()
	case fileDiffLoadedMsg:
		if e, ok := v.selected(); !ok || e.Path != m.Path {
			return nil
		}
		if m.Err != nil {
			state.SetMessage(m.Err.Error(), tuicore.MessageTypeError)
			return nil
		}
		v.core.LoadDiffs(m.Diffs, diffStats(m.Diffs))
		return nil
	case indexChangedMsg:
		if m.Err != nil {
			state.SetMessage(m.Err.Error(), tuicore.MessageTypeError)
		}
		return v.load()
	case tea.MouseClickMsg:
		if idx := tuicore.ZoneClicked(m, len(v.entries), v.zonePrefix); idx >= 0 {
			v.cursor = idx
			return v.loadDiff()
		}
	}
	return v.core.Update(msg, state)
}

// diffStats sums the lines of the loaded diffs for the title badge.
func diffStats(diffs []git.FileDiff) git.DiffStats {
	var stats git.DiffStats
	for _, d := range diffs {
		stats.Files++
		for _, h := range d.Hunks {
			for _, l := range h.Lines {
				switch l.Type {
				case git.LineAdded:
					stats.Added++
				case git.LineRemoved:
					stats.Removed++
				}
			}
		}
	}
	return stats
}

// selected returns the entry under the cursor.
func (v *changesView) selected() (git.StatusEntry, bool) {
	if v.cursor < 0 || v.cursor >= len(v.entries) {
		return git.StatusEntry{}, false
	}
	return v.entries[v.cursor], true
}

// stagedPaths returns the paths the index holds a change of.
func (v *changesView) stagedPaths() []string {
	var paths []string
	for _, e := range v.entries {
		if e.Staged() {
			paths = append(paths, e.Path)
		}
	}
	return paths
}

// unstagedPaths returns the paths with a change the index does not hold yet, untracked files included.
func (v *changesView) unstagedPaths() []string {
	var paths []string
	for _, e := range v.entries {
		if e.Unstaged() || e.Untracked() {
			paths = append(paths, e.Path)
		}
	}
	return paths
}

// moveCursor moves along the files and loads the diff of the new one.
func (v *changesView) moveCursor(delta int) tea.Cmd {
	next := v.cursor + delta
	if next < 0 || next >= len(v.entries) {
		return nil
	}
	v.cursor = next
	return v.loadDiff()
}

// writeIndex runs a stage or an unstage of the paths and reloads the list after it.
func (v *changesView) writeIndex(paths []string, stage bool) tea.Cmd {
	if len(paths) == 0 {
		return nil
	}
	workdir := v.core.Workdir()
	return func() tea.Msg {
		if stage {
			return indexChangedMsg{Err: git.StageFiles(workdir, paths)}
		}
		return indexChangedMsg{Err: git.UnstageFiles(workdir, paths)}
	}
}

// extraKey answers the file, index and commit keys before the shared diff keys.
func (v *changesView) extraKey(key string, state *tuicore.State) (bool, tea.Cmd) {
	if v.loading || v.loadErr != nil {
		return key != "esc" && key != "?" && key != "q", nil
	}
	switch key {
	case "j", "down":
		return true, v.moveCursor(1)
	case "k", "up":
		return true, v.moveCursor(-1)
	case "s":
		if e, ok := v.selected(); ok && (e.Unstaged() || e.Untracked()) {
			return true, v.writeIndex([]string{e.Path}, true)
		}
		return true, nil
	case "u":
		if e, ok := v.selected(); ok && e.Staged() {
			return true, v.writeIndex([]string{e.Path}, false)
		}
		return true, nil
	case "S":
		return true, v.writeIndex(v.unstagedPaths(), true)
	case "U":
		return true, v.writeIndex(v.stagedPaths(), false)
	case "tab", "shift+tab":
		return true, nil // the focus toggle of the host, not the file step of the diff core
	case "C":
		staged := len(v.stagedPaths())
		if staged == 0 {
			state.SetMessage("Nothing staged: s stages the selected file", tuicore.MessageTypeWarning)
			return true, nil
		}
		return true, func() tea.Msg {
			return tuicore.NavigateMsg{Location: tuicore.LocCommitFormFor(staged), Action: tuicore.NavPush}
		}
	}
	return false, nil
}

// Render renders the file rows, the diff of the selected file and the footer.
func (v *changesView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	var content string
	switch {
	case v.loading && len(v.entries) == 0:
		content = tuicore.Dim.Render("Loading changes...")
	case v.loadErr != nil:
		content = tuicore.Dim.Render("Error: " + v.loadErr.Error())
	case len(v.entries) == 0:
		content = tuicore.Dim.Render("No changes")
	default:
		content = v.renderList(wrapper.ContentWidth()) + "\n" + v.core.RenderContent()
	}
	footer := v.core.RenderFooter(state, tuicore.Changes, wrapper.ContentWidth())
	return wrapper.Render(content, footer)
}

// renderList renders the rows around the cursor, each with its two status columns and a word for them.
func (v *changesView) renderList(width int) string {
	rows := v.listRows()
	start := min(max(0, v.cursor-rows/2), max(0, len(v.entries)-rows))
	var b strings.Builder
	for i := start; i < len(v.entries) && i < start+rows; i++ {
		e := v.entries[i]
		columns := string([]byte{e.Index, e.Worktree})
		line := "  " + columns + " " + e.Path
		if i == v.cursor {
			line = tuicore.Title.Render("▸ " + columns + " " + e.Path)
		}
		line += tuicore.Dim.Render(" · " + stateWord(e))
		b.WriteString(tuicore.MarkZone(tuicore.ZoneID(v.zonePrefix, i), tuicore.TruncateToWidth(line, width)) + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// stateWord names the state of a row: untracked, staged, unstaged, or both.
func stateWord(e git.StatusEntry) string {
	switch {
	case e.Untracked():
		return "untracked"
	case e.Staged() && e.Unstaged():
		return "staged, then changed again"
	case e.Staged():
		return "staged"
	}
	return "unstaged"
}

// Title returns the view title with the counts of the list and of the index.
func (v *changesView) Title() string {
	if v.loading && len(v.entries) == 0 || v.loadErr != nil {
		return "±  Changes"
	}
	return fmt.Sprintf("±  Changes · %d files · %d staged", len(v.entries), len(v.stagedPaths()))
}
