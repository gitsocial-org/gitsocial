// component_diff_view.go - Shared text-content diff view used by history-diff sub-views
package tuicore

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// DiffVersion is one version exposed by a history diff source.
type DiffVersion struct {
	ID        string // version identity (typically a commit ref)
	Label     string // "v3", "original", "latest", ...
	Content   string // text body to diff
	Author    string
	Email     string
	Timestamp time.Time
}

// LoadDiffVersionsFunc loads all versions for a history diff source.
// routeParams gives the source whatever ID key it needs (postID, issueID, prID, ...).
type LoadDiffVersionsFunc func(workdir string, routeParams map[string]string) ([]DiffVersion, error)

// HistoryDiffConfig parameterises a HistoryDiffView for one extension.
type HistoryDiffConfig struct {
	Context    Context              // keybinding/footer context
	TitleIcon  string               // icon prefix in the title bar
	Title      string               // base title (e.g., "Post Diff")
	Load       LoadDiffVersionsFunc // version loader
	EnablePush bool                 // when true, adds p:push (workspace push)
}

// HistoryDiffView is a shared, parameterised diff view for "version N vs version M"
// content comparisons across all history views.
type HistoryDiffView struct {
	cfg         HistoryDiffConfig
	workdir     string
	routeParams map[string]string
	width       int
	height      int
	loaded      bool
	loadErr     string
	versions    []DiffVersion
	fromIdx     int
	toIdx       int
	rows        []git.TextDiffRow
	expanded    bool // when true, suppress auto-collapse of unchanged context
	cursor      int
	scroll      int
	scrollH     int
	noPrev      bool // route asked for an unknown predecessor
}

// NewHistoryDiffView creates a new shared history-diff view configured by cfg.
func NewHistoryDiffView(workdir string, cfg HistoryDiffConfig) *HistoryDiffView {
	return &HistoryDiffView{
		workdir: workdir,
		cfg:     cfg,
	}
}

// SetSize sets the view dimensions.
func (v *HistoryDiffView) SetSize(width, height int) {
	v.width = width
	v.height = height - 3
}

// Activate loads versions and computes the initial diff.
func (v *HistoryDiffView) Activate(state *State) tea.Cmd {
	loc := state.Router.Location()
	v.routeParams = cloneParams(loc.Params)
	v.loaded = false
	v.loadErr = ""
	v.cursor = 0
	v.scroll = 0
	v.scrollH = 0
	v.expanded = false
	v.noPrev = false
	cfg := v.cfg
	workdir := v.workdir
	params := v.routeParams
	return func() tea.Msg {
		versions, err := cfg.Load(workdir, params)
		if err != nil {
			return historyDiffLoadedMsg{err: err.Error()}
		}
		return historyDiffLoadedMsg{versions: versions, params: params}
	}
}

// Deactivate is called when the view is hidden.
func (v *HistoryDiffView) Deactivate() {}

// historyDiffLoadedMsg signals that versions have been loaded.
type historyDiffLoadedMsg struct {
	versions []DiffVersion
	params   map[string]string
	err      string
}

// Update handles messages and key/mouse input.
func (v *HistoryDiffView) Update(msg tea.Msg, _ *State) tea.Cmd {
	switch m := msg.(type) {
	case historyDiffLoadedMsg:
		if m.err != "" {
			v.loaded = true
			v.loadErr = m.err
			return nil
		}
		v.versions = m.versions
		v.loaded = true
		v.applyInitialPair(m.params)
		v.recomputeDiff("")
		return nil
	case tea.KeyPressMsg:
		return v.handleKey(m.String())
	}
	return nil
}

// applyInitialPair derives (fromIdx, toIdx) from route params with sensible fallbacks.
func (v *HistoryDiffView) applyInitialPair(params map[string]string) {
	if len(v.versions) == 0 {
		return
	}
	from := indexOfVersionID(v.versions, params["from"])
	to := indexOfVersionID(v.versions, params["to"])
	if to < 0 {
		to = len(v.versions) - 1
	}
	if from < 0 {
		v.noPrev = params["from"] != "" // route asked for an explicit predecessor that isn't in the list
		from = to - 1
		if from < 0 {
			from = 0
		}
	}
	if from > to {
		from, to = to, from
	}
	if from == to && len(v.versions) > 1 {
		if to+1 < len(v.versions) {
			to++
		} else if from-1 >= 0 {
			from--
		}
	}
	v.fromIdx = from
	v.toIdx = to
}

// indexOfVersionID returns the slice position of a version by its ID, or -1.
func indexOfVersionID(vs []DiffVersion, id string) int {
	if id == "" {
		return -1
	}
	for i, ver := range vs {
		if ver.ID == id {
			return i
		}
	}
	return -1
}

// handleKey processes a single key press. Returns a non-nil Cmd for any key the
// view consumes, so the global registry handler doesn't also fire for keys like
// `h` (which would otherwise be intercepted by other registered bindings).
func (v *HistoryDiffView) handleKey(key string) tea.Cmd {
	if !v.loaded {
		if key == "esc" {
			return func() tea.Msg { return NavigateMsg{Action: NavBack} }
		}
		return nil
	}
	noop := func() tea.Msg { return nil }
	switch key {
	case "esc":
		return func() tea.Msg { return NavigateMsg{Action: NavBack} }
	case "j", "down":
		if v.cursor < len(v.rows)-1 {
			v.cursor++
			v.ensureVisible()
		}
		return noop
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
			v.ensureVisible()
		}
		return noop
	case "g", "home":
		v.cursor, v.scroll = 0, 0
		return noop
	case "G", "end":
		if len(v.rows) > 0 {
			v.cursor = len(v.rows) - 1
			v.ensureVisible()
		}
		return noop
	case "ctrl+d", "pgdown":
		v.cursor += v.height / 2
		if v.cursor >= len(v.rows) {
			v.cursor = len(v.rows) - 1
		}
		v.ensureVisible()
		return noop
	case "ctrl+u", "pgup":
		v.cursor -= v.height / 2
		if v.cursor < 0 {
			v.cursor = 0
		}
		v.ensureVisible()
		return noop
	case "h", "left":
		if v.scrollH >= 4 {
			v.scrollH -= 4
		} else {
			v.scrollH = 0
		}
		return noop
	case "l", "right":
		v.scrollH += 4
		return noop
	case "[":
		v.shiftPair(-1)
		return noop
	case "]":
		v.shiftPair(1)
		return noop
	case ",":
		v.moveFrom(-1)
		return noop
	case ".":
		v.moveFrom(1)
		return noop
	case "<":
		v.moveTo(-1)
		return noop
	case ">":
		v.moveTo(1)
		return noop
	case "e", "E", "enter":
		v.expanded = !v.expanded
		v.recomputeDiff(v.cursorKey())
		return noop
	}
	return nil
}

// shiftPair moves both endpoints of the pair by dir while preserving the gap.
func (v *HistoryDiffView) shiftPair(dir int) {
	if len(v.versions) < 2 {
		return
	}
	newFrom, newTo := v.fromIdx+dir, v.toIdx+dir
	if newFrom < 0 || newTo >= len(v.versions) {
		return
	}
	anchor := v.cursorKey()
	v.fromIdx, v.toIdx = newFrom, newTo
	v.recomputeDiff(anchor)
}

// moveFrom shifts only the "from" anchor by dir.
func (v *HistoryDiffView) moveFrom(dir int) {
	newFrom := v.fromIdx + dir
	if newFrom < 0 || newFrom >= v.toIdx {
		return
	}
	anchor := v.cursorKey()
	v.fromIdx = newFrom
	v.recomputeDiff(anchor)
}

// moveTo shifts only the "to" anchor by dir.
func (v *HistoryDiffView) moveTo(dir int) {
	newTo := v.toIdx + dir
	if newTo <= v.fromIdx || newTo >= len(v.versions) {
		return
	}
	anchor := v.cursorKey()
	v.toIdx = newTo
	v.recomputeDiff(anchor)
}

// recomputeDiff regenerates rows for the current pair and tries to land cursor
// on the previously-anchored row by stable Key (falls back to numeric clamp).
// Honors v.expanded: when true, runs UnifiedTextDiff without any collapse so
// every unchanged line is visible.
func (v *HistoryDiffView) recomputeDiff(anchorKey string) {
	if v.fromIdx < 0 || v.toIdx < 0 || v.fromIdx >= len(v.versions) || v.toIdx >= len(v.versions) {
		v.rows = nil
		return
	}
	from := v.versions[v.fromIdx].Content
	to := v.versions[v.toIdx].Content
	collapseAt := 5
	if v.expanded {
		collapseAt = 0
	}
	rows := git.UnifiedTextDiff(from, to, git.TextDiffOptions{ContextLines: 3, CollapseAt: collapseAt})
	v.rows = rows
	if anchorKey != "" {
		for i, r := range rows {
			if r.Key == anchorKey {
				v.cursor = i
				v.ensureVisible()
				return
			}
		}
	}
	if v.cursor >= len(rows) {
		v.cursor = len(rows) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
	v.ensureVisible()
}

// cursorKey returns the stable Key of the row under the cursor, or "".
func (v *HistoryDiffView) cursorKey() string {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return ""
	}
	return v.rows[v.cursor].Key
}

// ensureVisible scrolls so the cursor is inside the viewport.
func (v *HistoryDiffView) ensureVisible() {
	if v.height < 1 {
		return
	}
	if v.cursor < v.scroll {
		v.scroll = v.cursor
	} else if v.cursor >= v.scroll+v.height {
		v.scroll = v.cursor - v.height + 1
	}
}

// Render draws the view.
func (v *HistoryDiffView) Render(state *State) string {
	wrapper := NewViewWrapper(state)
	footer := RenderFooter(state.Registry, v.cfg.Context, nil)
	if !v.loaded {
		return wrapper.Render(Dim.Render("  Loading diff..."), footer)
	}
	if v.loadErr != "" {
		return wrapper.Render(Dim.Render("  "+v.loadErr), footer)
	}
	if len(v.versions) == 0 {
		return wrapper.Render(Dim.Render("  No versions to compare"), footer)
	}
	if len(v.versions) < 2 {
		return wrapper.Render(Dim.Render("  Only one version exists; nothing to diff"), footer)
	}
	header := v.renderHeader()
	body := v.renderBody()
	if v.noPrev && body == "" {
		body = Dim.Render("  No previous version to compare; use [/] or ,/. or </> to pick a pair")
	}
	content := header + "\n" + body
	return wrapper.Render(content, footer)
}

// renderHeader shows the from→to pair summary.
func (v *HistoryDiffView) renderHeader() string {
	from := v.versions[v.fromIdx]
	to := v.versions[v.toIdx]
	left := fmt.Sprintf("%s · %s", from.Label, FormatTime(from.Timestamp))
	right := fmt.Sprintf("%s · %s", to.Label, FormatTime(to.Timestamp))
	if from.Author != "" {
		left += " · " + from.Author
	}
	if to.Author != "" {
		right += " · " + to.Author
	}
	arrow := lipgloss.NewStyle().Foreground(TextSecondary).Render(" → ")
	return Bold.Render(left) + arrow + Bold.Render(right)
}

// renderBody renders visible diff rows with prefix coloring.
func (v *HistoryDiffView) renderBody() string {
	if len(v.rows) == 0 {
		return Dim.Render("  No content changes between these versions")
	}
	end := v.scroll + v.height
	if end > len(v.rows) {
		end = len(v.rows)
	}
	if v.scroll < 0 {
		v.scroll = 0
	}
	visible := v.rows[v.scroll:end]
	addedStyle := lipgloss.NewStyle().Foreground(DiffAdded)
	removedStyle := lipgloss.NewStyle().Foreground(DiffRemoved)
	hunkStyle := lipgloss.NewStyle().Foreground(DiffHunkHeader)
	collapsedStyle := lipgloss.NewStyle().Foreground(TextSecondary)
	var lines []string
	for i, row := range visible {
		var rendered string
		switch row.Kind {
		case git.TextDiffHunkHeader:
			rendered = hunkStyle.Render(row.Text)
		case git.TextDiffAdded:
			rendered = addedStyle.Render("+ " + visibleSlice(row.Text, v.scrollH))
		case git.TextDiffRemoved:
			rendered = removedStyle.Render("- " + visibleSlice(row.Text, v.scrollH))
		case git.TextDiffContext:
			rendered = "  " + visibleSlice(row.Text, v.scrollH)
		case git.TextDiffCollapsed:
			rendered = collapsedStyle.Render(fmt.Sprintf("··· %d unchanged lines ···", row.Hidden))
		}
		if v.scroll+i == v.cursor {
			lines = append(lines, Title.Render("▏")+rendered)
		} else {
			lines = append(lines, " "+rendered)
		}
	}
	return strings.Join(lines, "\n")
}

// visibleSlice trims the leading scrollH runes of a line for horizontal scroll.
func visibleSlice(s string, scrollH int) string {
	if scrollH <= 0 {
		return s
	}
	rs := []rune(s)
	if scrollH >= len(rs) {
		return ""
	}
	return string(rs[scrollH:])
}

// Title returns the header title showing the active pair.
func (v *HistoryDiffView) Title() string {
	icon := v.cfg.TitleIcon
	if icon == "" {
		icon = "Δ"
	}
	if !v.loaded || len(v.versions) < 2 {
		return icon + "  " + v.cfg.Title
	}
	return fmt.Sprintf("%s  %s · %s → %s", icon, v.cfg.Title,
		v.versions[v.fromIdx].Label, v.versions[v.toIdx].Label)
}

// IsInputActive returns false (no text input here).
func (v *HistoryDiffView) IsInputActive() bool { return false }

// Bindings returns the keybindings shown in the footer.
// Combined-key entries (e.g. "[/]") are footer-display only; actual key handling
// for the underlying single keys lives in handleKey. This mirrors the pattern
// used by view_diff.go for "[/]:prev/next hunk".
func (v *HistoryDiffView) Bindings() []Binding {
	noop := func(*HandlerContext) (bool, tea.Cmd) { return false, nil }
	ctx := []Context{v.cfg.Context}
	bindings := []Binding{
		{Key: "e/E", Label: "expand", Contexts: ctx, Handler: noop},
		{Key: "[/]", Label: "shift pair", Contexts: ctx, Handler: noop},
		{Key: ",/.", Label: "from anchor", Contexts: ctx, Handler: noop},
		{Key: "</>", Label: "to anchor", Contexts: ctx, Handler: noop},
	}
	if v.cfg.EnablePush {
		bindings = append(bindings, Binding{Key: "p", Label: "push", Contexts: ctx, Handler: func(hc *HandlerContext) (bool, tea.Cmd) {
			if hc.StartPush == nil {
				return false, nil
			}
			return true, hc.StartPush()
		}})
	}
	return bindings
}

// diffMetadataOrder lists header fields shown in a history-diff metadata block,
// in display order with friendly labels. Fields not listed are appended
// alphabetically by raw key, so new fields still surface.
var diffMetadataOrder = []struct{ key, label string }{
	{"state", "State"},
	{"draft", "Draft"},
	{"labels", "Labels"},
	{"assignees", "Assignees"},
	{"reviewers", "Reviewers"},
	{"due", "Due"},
	{"start", "Start"},
	{"end", "End"},
	{"milestone", "Milestone"},
	{"sprint", "Sprint"},
	{"parent", "Parent"},
	{"blocks", "Blocks"},
	{"blocked-by", "Blocked by"},
	{"related", "Related"},
	{"depends-on", "Depends on"},
	{"closes", "Closes"},
	{"base", "Base"},
	{"head", "Head"},
	{"base-tip", "Base tip"},
	{"head-tip", "Head tip"},
	{"tag", "Tag"},
	{"version", "Version"},
	{"prerelease", "Prerelease"},
	{"artifacts", "Artifacts"},
	{"artifact-url", "Artifact URL"},
	{"checksums", "Checksums"},
	{"signed-by", "Signed by"},
	{"sbom", "SBOM"},
	{"original", "Original"},
	{"reply-to", "Reply to"},
}

// diffMetadataExclude are structural header fields never shown in the block.
var diffMetadataExclude = map[string]bool{
	"ext": true, "v": true, "edits": true, "type": true, "retracted": true,
	"origin-author": true, "origin-email": true, "origin-time": true, "origin-url": true,
}

// DiffContentWithMetadata prepends a readable metadata block (header fields such
// as state/labels/tag, plus a retraction marker) to the body, so history diffs
// surface metadata changes (e.g. closing an issue), not just body edits.
func DiffContentWithMetadata(fields map[string]string, retracted bool, body string) string {
	var b strings.Builder
	if retracted {
		b.WriteString("Retracted: true\n")
	}
	shown := make(map[string]bool, len(diffMetadataOrder))
	for _, o := range diffMetadataOrder {
		if val := fields[o.key]; val != "" {
			b.WriteString(o.label + ": " + val + "\n")
			shown[o.key] = true
		}
	}
	var extra []string
	for k, val := range fields {
		if val != "" && !shown[k] && !diffMetadataExclude[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		b.WriteString(k + ": " + fields[k] + "\n")
	}
	meta := b.String()
	switch {
	case meta == "":
		return body
	case body == "":
		return meta
	default:
		return meta + "\n" + body
	}
}

// cloneParams returns a defensive copy of a route params map.
func cloneParams(p map[string]string) map[string]string {
	out := make(map[string]string, len(p))
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out[k] = p[k]
	}
	return out
}
