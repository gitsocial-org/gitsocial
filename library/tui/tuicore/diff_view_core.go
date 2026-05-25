// diff_view_core.go - Shared state machine for commit + PR diff views.
//
// Owns the LogicalDiff/ViewState/DisplayPlan pipeline, cursor + scroll,
// fold mutations, search, and shared keybindings. Wrappers (CommitDiffView,
// tuireview.DiffView) embed this and add their own Activate + extra keys.
package tuicore

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore/diff"
)

// ExtraKey is a wrapper-installed key hook. Return handled=true to consume
// the key; cmd may be nil. Called before shared keys but after the
// search-input/search-active modes.
type ExtraKey func(key string, state *State) (bool, tea.Cmd)

// DiffViewCore is the shared state for any diff view (commit, PR, etc).
type DiffViewCore struct {
	workdir string
	width   int
	height  int

	loaded bool
	diffs  []git.FileDiff
	stats  git.DiffStats

	logical diff.LogicalDiff
	state   diff.ViewState
	plan    diff.DisplayPlan
	layers  []diff.Layer

	cursor       int
	scroll       int
	selectedFile int
	maxScrollH   int

	// Mode 0=unified, 1=split, 2=fullscreen (split + no nav).
	mode int

	// Search
	searchActive    bool
	searchInputMode bool
	searchInput     textinput.Model
	searchQuery     string
	matchPositions  []int
	matchIndex      int

	zonePrefix string
	extraKey   ExtraKey
}

// NewDiffViewCore creates a DiffViewCore for the given workdir.
func NewDiffViewCore(workdir string) *DiffViewCore {
	input := textinput.New()
	input.Placeholder = "Search..."
	input.CharLimit = 100
	input.Prompt = "> "
	StyleTextInput(&input, Title, Title, Dim)
	return &DiffViewCore{
		workdir:     workdir,
		searchInput: input,
		zonePrefix:  zone.NewPrefix(),
	}
}

// SetLayers installs the post-build decoration layers.
func (c *DiffViewCore) SetLayers(layers []diff.Layer) {
	c.layers = layers
	if c.loaded {
		c.rebuild()
	}
}

// SetExtraKey installs a wrapper-specific key hook.
func (c *DiffViewCore) SetExtraKey(fn ExtraKey) { c.extraKey = fn }

// Workdir returns the working directory.
func (c *DiffViewCore) Workdir() string { return c.workdir }

// Loaded returns whether the diff has been loaded.
func (c *DiffViewCore) Loaded() bool { return c.loaded }

// Diffs returns the loaded file diffs.
func (c *DiffViewCore) Diffs() []git.FileDiff { return c.diffs }

// Stats returns the diff stats.
func (c *DiffViewCore) Stats() git.DiffStats { return c.stats }

// Plan returns the current DisplayPlan.
func (c *DiffViewCore) Plan() diff.DisplayPlan { return c.plan }

// SearchActive returns whether search navigation is active.
func (c *DiffViewCore) SearchActive() bool { return c.searchActive }

// IsInputActive returns whether the search input is taking keystrokes.
func (c *DiffViewCore) IsInputActive() bool { return c.searchInputMode }

// CursorAnchor returns the anchor of the row under the cursor.
func (c *DiffViewCore) CursorAnchor() diff.RowAnchor {
	if c.cursor < 0 || c.cursor >= len(c.plan.Rows) {
		return diff.RowAnchor{}
	}
	return c.plan.Rows[c.cursor].Anchor
}

// FilePath returns the user-facing path for the given fileIdx.
func (c *DiffViewCore) FilePath(fileIdx int) string {
	if fileIdx < 0 || fileIdx >= len(c.logical.Files) {
		return ""
	}
	fd := c.logical.Files[fileIdx]
	if fd.NewPath != "" {
		return fd.NewPath
	}
	return fd.OldPath
}

// Reset clears all view state for a fresh Activate.
func (c *DiffViewCore) Reset() {
	c.loaded = false
	c.diffs = nil
	c.stats = git.DiffStats{}
	c.logical = diff.LogicalDiff{}
	c.state = diff.ViewState{}
	c.plan = diff.DisplayPlan{}
	c.cursor = 0
	c.scroll = 0
	c.selectedFile = 0
	c.maxScrollH = 0
	c.searchActive = false
	c.searchInputMode = false
	c.searchQuery = ""
	c.searchInput.SetValue("")
	c.matchPositions = nil
	c.matchIndex = 0
}

// LoadDiffs replaces the diffs and rebuilds the plan.
func (c *DiffViewCore) LoadDiffs(diffs []git.FileDiff, stats git.DiffStats) {
	c.diffs = diffs
	c.stats = stats
	c.loaded = true
	c.logical = diff.BuildLogical(diffs)
	c.state.Folds = nil
	for i := range diffs {
		c.state.Folds = append(c.state.Folds, diff.FoldRegion{
			Start: diff.RowAnchor{FileIdx: i, HunkIdx: -1},
			End:   diff.RowAnchor{FileIdx: i + 1, HunkIdx: -1},
			Kind:  diff.FoldFile,
		})
	}
	c.state.Folds = append(c.state.Folds, diff.ContextAutoFolds(c.logical)...)
	c.state.Layout = c.layoutFromMode()
	c.rebuild()
}

// SetSize updates dimensions and rebuilds if needed.
func (c *DiffViewCore) SetSize(w, h int) {
	if c.width == w && c.height == h {
		return
	}
	c.width = w
	c.height = h
	if c.loaded && len(c.diffs) > 0 {
		c.rebuild()
	}
}

// Update processes messages. Wrappers call this after their own
// loaded-message handling.
func (c *DiffViewCore) Update(msg tea.Msg, state *State) tea.Cmd {
	switch m := msg.(type) {
	case tea.MouseMsg:
		return c.handleMouse(m)
	case tea.KeyPressMsg:
		return c.handleKey(m, state)
	}
	if c.searchInputMode {
		var cmd tea.Cmd
		c.searchInput, cmd = c.searchInput.Update(msg)
		return cmd
	}
	return nil
}

func (c *DiffViewCore) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if c.searchInputMode {
		return nil
	}
	switch msg.(type) {
	case tea.MouseWheelMsg:
		m := msg.Mouse()
		if m.Button == tea.MouseWheelUp {
			c.moveCursor(-1)
		} else {
			c.moveCursor(1)
		}
		return nil
	case tea.MouseClickMsg:
		idx := ZoneClicked(msg, len(c.plan.Rows), c.zonePrefix)
		if idx >= 0 && idx < len(c.plan.Rows) {
			prev := c.cursor
			c.cursor = idx
			c.syncSelectedFile()
			c.ensureCursorVisible()
			if prev == idx {
				c.toggleFileAtCursor()
			}
		}
		return nil
	}
	return nil
}

func (c *DiffViewCore) handleKey(msg tea.KeyPressMsg, state *State) tea.Cmd {
	if c.searchInputMode {
		switch msg.String() {
		case "esc":
			c.exitSearch()
			return nil
		case "enter":
			c.searchInputMode = false
			c.searchInput.Blur()
			if c.searchQuery == "" {
				c.searchActive = false
			}
			return nil
		}
		var cmd tea.Cmd
		c.searchInput, cmd = c.searchInput.Update(msg)
		c.updateSearch()
		return cmd
	}
	key := msg.String()
	if c.extraKey != nil {
		if handled, cmd := c.extraKey(key, state); handled {
			return cmd
		}
	}
	if c.searchActive {
		switch key {
		case "n":
			c.nextMatch()
			return nil
		case "N":
			c.prevMatch()
			return nil
		case "/":
			c.searchInputMode = true
			return c.searchInput.Focus()
		case "esc":
			c.exitSearch()
			return nil
		}
	}
	return c.handleSharedKey(key)
}

func (c *DiffViewCore) handleSharedKey(key string) tea.Cmd {
	switch key {
	case "j", "down":
		c.moveCursor(1)
	case "k", "up":
		c.moveCursor(-1)
	case "ctrl+d", "pgdown":
		c.moveCursor(c.viewportHeight() / 2)
	case "ctrl+u", "pgup":
		c.moveCursor(-c.viewportHeight() / 2)
	case "g":
		c.cursor = 0
		c.syncSelectedFile()
		c.ensureCursorVisible()
	case "G":
		if n := len(c.plan.Rows); n > 0 {
			c.cursor = n - 1
		}
		c.syncSelectedFile()
		c.ensureCursorVisible()
	case "l", "right":
		if !c.state.Wrap {
			c.state.ScrollH += 4
			if c.state.ScrollH > c.maxScrollH {
				c.state.ScrollH = c.maxScrollH
			}
		}
	case "h", "left":
		if !c.state.Wrap && c.state.ScrollH > 0 {
			c.state.ScrollH -= 4
			if c.state.ScrollH < 0 {
				c.state.ScrollH = 0
			}
		}
	case "enter":
		c.toggleFileAtCursor()
	case "tab":
		if c.selectedFile < len(c.diffs)-1 {
			c.selectedFile++
			c.moveCursorToFile(c.selectedFile)
		}
	case "shift+tab":
		if c.selectedFile > 0 {
			c.selectedFile--
			c.moveCursorToFile(c.selectedFile)
		}
	case "v":
		return c.cycleMode()
	case "]":
		c.jumpToNextHunk()
	case "[":
		c.jumpToPrevHunk()
	case "E":
		c.toggleAllFiles()
	case "e":
		c.toggleFoldAtCursor()
	case "w":
		c.state.Wrap = !c.state.Wrap
		if c.state.Wrap {
			c.state.ScrollH = 0
		}
		c.rebuild()
	case "/":
		c.searchActive = true
		c.searchInputMode = true
		c.searchInput.SetValue("")
		return c.searchInput.Focus()
	case "esc":
		if c.mode == 2 {
			c.mode = 1
			c.state.Layout = c.layoutFromMode()
			c.rebuild()
			return func() tea.Msg { return NavVisibilityMsg{Hidden: false} }
		}
		return func() tea.Msg { return NavigateMsg{Action: NavBack} }
	}
	return nil
}

func (c *DiffViewCore) moveCursor(delta int) {
	if len(c.plan.Rows) == 0 {
		return
	}
	c.cursor += delta
	if c.cursor < 0 {
		c.cursor = 0
	}
	if c.cursor > len(c.plan.Rows)-1 {
		c.cursor = len(c.plan.Rows) - 1
	}
	c.syncSelectedFile()
	c.ensureCursorVisible()
}

func (c *DiffViewCore) cycleMode() tea.Cmd {
	prev := c.mode
	anchor := c.CursorAnchor()
	c.mode = (c.mode + 1) % 3
	c.state.Layout = c.layoutFromMode()
	c.rebuild()
	c.restoreCursorByAnchor(anchor)
	if prev == 2 || c.mode == 2 {
		hidden := c.mode == 2
		return func() tea.Msg { return NavVisibilityMsg{Hidden: hidden} }
	}
	return nil
}

func (c *DiffViewCore) layoutFromMode() diff.Layout {
	switch c.mode {
	case 1:
		return diff.LayoutSplit
	case 2:
		return diff.LayoutFullscreen
	}
	return diff.LayoutUnified
}

// rebuild rebuilds plan and decorates with layers.
func (c *DiffViewCore) rebuild() {
	c.plan = diff.BuildPlan(c.logical, c.state, DefaultDiffPalette(), DefaultHighlight())
	for _, lr := range c.layers {
		c.plan = lr.Decorate(c.plan, c.state)
	}
	if c.cursor > len(c.plan.Rows)-1 {
		c.cursor = len(c.plan.Rows) - 1
	}
	if c.cursor < 0 {
		c.cursor = 0
	}
	c.maxScrollH = 0
	contentCols := c.width - 1
	if contentCols < 10 {
		contentCols = 10
	}
	leftWidth := (contentCols - 1) / 2
	rightWidth := contentCols - 1 - leftWidth
	for _, r := range c.plan.Rows {
		var rowWidth int
		if r.IsSplitBody() {
			if r.Left != nil && r.Left.Width() > leftWidth {
				if d := r.Left.Width() - leftWidth; d > rowWidth {
					rowWidth = d
				}
			}
			if r.Right != nil && r.Right.Width() > rightWidth {
				if d := r.Right.Width() - rightWidth; d > rowWidth {
					rowWidth = d
				}
			}
		} else {
			rowWidth = r.Width() - contentCols
		}
		if rowWidth > c.maxScrollH {
			c.maxScrollH = rowWidth
		}
	}
}

// restoreCursorByAnchor finds the plan row matching the given anchor and
// snaps cursor + scroll to it. Falls back to clamp when no match.
func (c *DiffViewCore) restoreCursorByAnchor(a diff.RowAnchor) {
	if idx := findRowByAnchor(c.plan.Rows, a); idx >= 0 {
		c.cursor = idx
	} else if c.cursor >= len(c.plan.Rows) {
		c.cursor = len(c.plan.Rows) - 1
	}
	if c.cursor < 0 {
		c.cursor = 0
	}
	c.ensureCursorVisible()
}

// findRowByAnchor tries exact match (modulo Tag) on the row's outer
// anchor, then on either split-half's anchor, then a (file, hunk)
// fallback for header-style targets.
func findRowByAnchor(rows []diff.DisplayRow, a diff.RowAnchor) int {
	bare := func(x diff.RowAnchor) diff.RowAnchor { x.Tag = ""; return x }
	target := bare(a)
	for i, r := range rows {
		if bare(r.Anchor) == target {
			return i
		}
		if r.Left != nil && bare(r.Left.Anchor) == target {
			return i
		}
		if r.Right != nil && bare(r.Right.Anchor) == target {
			return i
		}
	}
	if a.OldLine == 0 && a.NewLine == 0 {
		for i, r := range rows {
			if r.Anchor.FileIdx == a.FileIdx && r.Anchor.HunkIdx == a.HunkIdx {
				return i
			}
		}
	}
	return -1
}

func (c *DiffViewCore) syncSelectedFile() {
	if c.cursor >= 0 && c.cursor < len(c.plan.Rows) {
		c.selectedFile = c.plan.Rows[c.cursor].Anchor.FileIdx
	}
}

func (c *DiffViewCore) ensureCursorVisible() {
	vh := c.viewportHeight()
	if c.cursor < c.scroll {
		c.scroll = c.cursor
	}
	if c.cursor >= c.scroll+vh {
		c.scroll = c.cursor - vh + 1
	}
	if c.scroll < 0 {
		c.scroll = 0
	}
}

func (c *DiffViewCore) viewportHeight() int {
	vh := c.height - 5
	if vh < 1 {
		vh = 1
	}
	return vh
}

func (c *DiffViewCore) toggleFileAtCursor() {
	a := c.CursorAnchor()
	c.toggleFileFold(a.FileIdx)
}

func (c *DiffViewCore) toggleFileFold(fileIdx int) {
	if fileIdx < 0 || fileIdx >= len(c.diffs) {
		return
	}
	for i := range c.state.Folds {
		r := &c.state.Folds[i]
		if r.Kind == diff.FoldFile && r.Start.FileIdx == fileIdx {
			r.UserOpened = !r.UserOpened
			c.rebuild()
			return
		}
	}
}

func (c *DiffViewCore) toggleAllFiles() {
	anyClosed := false
	for _, r := range c.state.Folds {
		if r.Kind == diff.FoldFile && !r.UserOpened {
			anyClosed = true
			break
		}
	}
	for i := range c.state.Folds {
		if c.state.Folds[i].Kind == diff.FoldFile {
			c.state.Folds[i].UserOpened = anyClosed
		}
	}
	c.rebuild()
}

// toggleFoldAtCursor toggles any fold (context or file) whose Start anchor
// matches the cursor's anchor. Used by `e` so a single key opens whatever
// the user is pointing at.
func (c *DiffViewCore) toggleFoldAtCursor() {
	a := c.CursorAnchor()
	for i := range c.state.Folds {
		r := &c.state.Folds[i]
		if r.Start.FileIdx == a.FileIdx && r.Start.HunkIdx == a.HunkIdx &&
			r.Start.OldLine == a.OldLine && r.Start.NewLine == a.NewLine {
			r.UserOpened = !r.UserOpened
			c.rebuild()
			return
		}
	}
}

func (c *DiffViewCore) moveCursorToFile(idx int) {
	for i, r := range c.plan.Rows {
		if r.Anchor.FileIdx == idx && r.Kind == diff.RowFileHeader {
			c.cursor = i
			c.ensureCursorVisible()
			return
		}
	}
}

func (c *DiffViewCore) jumpToNextHunk() {
	if len(c.plan.Rows) == 0 {
		return
	}
	cur := c.plan.Rows[c.cursor].Anchor
	for i := c.cursor + 1; i < len(c.plan.Rows); i++ {
		a := c.plan.Rows[i].Anchor
		if a.HunkIdx >= 0 && (a.HunkIdx != cur.HunkIdx || a.FileIdx != cur.FileIdx) {
			headerRow := i
			c.cursor = i
			if i+1 < len(c.plan.Rows) {
				next := c.plan.Rows[i+1].Anchor
				if next.HunkIdx == a.HunkIdx && next.FileIdx == a.FileIdx {
					c.cursor = i + 1
				}
			}
			c.syncSelectedFile()
			c.revealHunkAtRow(headerRow)
			return
		}
	}
}

func (c *DiffViewCore) jumpToPrevHunk() {
	if len(c.plan.Rows) == 0 || c.cursor == 0 {
		return
	}
	cur := c.plan.Rows[c.cursor].Anchor
	i := c.cursor - 1
	for i >= 0 {
		a := c.plan.Rows[i].Anchor
		if a.HunkIdx >= 0 && (a.HunkIdx != cur.HunkIdx || a.FileIdx != cur.FileIdx) {
			break
		}
		i--
	}
	if i < 0 {
		return
	}
	target := c.plan.Rows[i].Anchor
	for i > 0 {
		prev := c.plan.Rows[i-1].Anchor
		if prev.HunkIdx != target.HunkIdx || prev.FileIdx != target.FileIdx {
			break
		}
		i--
	}
	headerRow := i
	c.cursor = i
	if i+1 < len(c.plan.Rows) {
		next := c.plan.Rows[i+1].Anchor
		if next.HunkIdx == target.HunkIdx && next.FileIdx == target.FileIdx {
			c.cursor = i + 1
		}
	}
	c.syncSelectedFile()
	c.revealHunkAtRow(headerRow)
}

func (c *DiffViewCore) revealHunkAtRow(headerRow int) {
	if headerRow < 0 || headerRow >= len(c.plan.Rows) {
		c.ensureCursorVisible()
		return
	}
	a := c.plan.Rows[headerRow].Anchor
	end := headerRow + 1
	for end < len(c.plan.Rows) {
		b := c.plan.Rows[end].Anchor
		if b.FileIdx != a.FileIdx || b.HunkIdx != a.HunkIdx {
			break
		}
		end++
	}
	c.scroll = HunkRevealScroll(headerRow, end-headerRow, c.viewportHeight(), len(c.plan.Rows))
}

// CursorIdx returns the current cursor plan-row index.
func (c *DiffViewCore) CursorIdx() int { return c.cursor }

// JumpToRow snaps the cursor to a specific plan-row index and reveals it.
// Used by wrappers' jump-to-next/prev-comment helpers.
func (c *DiffViewCore) JumpToRow(idx int) {
	if idx < 0 || idx >= len(c.plan.Rows) {
		return
	}
	c.cursor = idx
	c.syncSelectedFile()
	c.ensureCursorVisible()
}

// RenderContent renders the visible diff rows.
func (c *DiffViewCore) RenderContent() string {
	if len(c.plan.Rows) == 0 {
		return Dim.Render("No changes")
	}
	summary := fmt.Sprintf("Files: %d changed  %s",
		c.stats.Files, RenderDiffStatsBadge(c.stats.Added, c.stats.Removed))
	vh := c.viewportHeight()
	if c.scroll < 0 {
		c.scroll = 0
	}
	if c.scroll >= len(c.plan.Rows) {
		c.scroll = len(c.plan.Rows) - 1
	}
	contentCols := c.width - 1
	if contentCols < 10 {
		contentCols = 10
	}
	indicator := lipgloss.NewStyle().Foreground(lipgloss.Color(IdentityFollowing)).Render("▌")
	visualBudget := vh
	var visible []string
	for i := c.scroll; i < len(c.plan.Rows) && visualBudget > 0; i++ {
		dr := c.plan.Rows[i]
		var emit []string
		if dr.IsSplitBody() {
			emit = c.renderSplitRow(dr, contentCols)
		} else {
			emit = c.renderSingleRow(dr.Row, contentCols)
		}
		for r, line := range emit {
			if visualBudget <= 0 {
				break
			}
			if c.searchActive && c.searchQuery != "" {
				line = HighlightInText(line, c.searchQuery)
			}
			prefix := " "
			if i == c.cursor && r == 0 {
				prefix = indicator
			}
			line = prefix + line
			if r == 0 {
				line = MarkZone(ZoneID(c.zonePrefix, i), line)
			}
			visible = append(visible, line)
			visualBudget--
		}
	}
	out := summary + "\n\n"
	if pinned := c.renderPinnedFileHeader(); pinned != "" {
		out += pinned + "\n"
	}
	return out + strings.Join(visible, "\n")
}

// renderSingleRow renders one DisplayRow as one or more visual lines
// (multiple when wrap is on).
func (c *DiffViewCore) renderSingleRow(row diff.Row, cols int) []string {
	var emit []diff.Row
	if c.state.Wrap {
		emit = diff.WrapRow(row, cols, diff.BodyPrefixWidth(row))
	} else {
		emit = []diff.Row{diff.SliceRow(row, c.state.ScrollH, cols)}
	}
	out := make([]string, 0, len(emit))
	for _, r := range emit {
		out = append(out, diff.RenderRow(r, cols))
	}
	return out
}

// renderSplitRow renders a split-pair DisplayRow with aligned halves.
// Each half is padded to (cols-1)/2; the separator lands at the same
// column on every row.
func (c *DiffViewCore) renderSplitRow(dr diff.DisplayRow, cols int) []string {
	leftWidth := (cols - 1) / 2
	rightWidth := cols - 1 - leftWidth
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color(DiffLineNum)).Render("│")
	leftLines := c.renderSplitHalf(dr.Left, leftWidth)
	rightLines := c.renderSplitHalf(dr.Right, rightWidth)
	rows := len(leftLines)
	if len(rightLines) > rows {
		rows = len(rightLines)
	}
	out := make([]string, rows)
	for k := 0; k < rows; k++ {
		l := blankHalf(leftWidth)
		r := blankHalf(rightWidth)
		if k < len(leftLines) {
			l = leftLines[k]
		}
		if k < len(rightLines) {
			r = rightLines[k]
		}
		out[k] = l + sep + r
	}
	if len(out) == 0 {
		out = []string{blankHalf(leftWidth) + sep + blankHalf(rightWidth)}
	}
	return out
}

// renderSplitHalf renders one half of a split pair. nil → no lines (caller
// pads with blanks). Non-nil → slice or wrap to halfWidth.
func (c *DiffViewCore) renderSplitHalf(half *diff.Row, halfWidth int) []string {
	if half == nil {
		return nil
	}
	var emit []diff.Row
	if c.state.Wrap {
		emit = diff.WrapRow(*half, halfWidth, diff.BodyPrefixWidth(*half))
	} else {
		emit = []diff.Row{diff.SliceRow(*half, c.state.ScrollH, halfWidth)}
	}
	out := make([]string, 0, len(emit))
	for _, r := range emit {
		out = append(out, diff.RenderRow(r, halfWidth))
	}
	return out
}

// blankHalf returns `n` plain spaces with no background — used when one
// side of a split pair has no content.
func blankHalf(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

// planRowPlainText concatenates the text of all cells in a DisplayRow,
// including both halves for split-pair rows.
func planRowPlainText(r diff.DisplayRow) string {
	var b strings.Builder
	for _, c := range r.Cells {
		b.WriteString(c.Text)
	}
	if r.Left != nil {
		for _, c := range r.Left.Cells {
			b.WriteString(c.Text)
		}
	}
	if r.Right != nil {
		for _, c := range r.Right.Cells {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

func (c *DiffViewCore) renderPinnedFileHeader() string {
	if c.scroll < 0 || c.scroll >= len(c.plan.Rows) {
		return ""
	}
	fileIdx := c.plan.Rows[c.scroll].Anchor.FileIdx
	if fileIdx < 0 || fileIdx >= len(c.diffs) {
		return ""
	}
	for i, r := range c.plan.Rows {
		if r.Anchor.FileIdx != fileIdx {
			continue
		}
		if r.Kind == diff.RowFileHeader || r.Anchor.Tag == "fold-placeholder" {
			if i >= c.scroll {
				return ""
			}
			break
		}
	}
	return Dim.Render("▸ ") + RenderDiffHeader(c.diffs[fileIdx])
}

// SharedBindings returns the keybindings every diff view exposes. Wrappers
// append their own extras (e.g. PR's "c" for inline comment). Labels are
// purely informational — actual key handling lives in handleSharedKey.
func (c *DiffViewCore) SharedBindings(ctx Context) []Binding {
	noop := func(_ *HandlerContext) (bool, tea.Cmd) { return false, nil }
	contexts := []Context{ctx}
	return []Binding{
		{Key: "e/E", Label: "expand", Contexts: contexts, Handler: noop},
		{Key: "w", Label: "wrap", Contexts: contexts, Handler: noop},
		{Key: "v", Label: "view mode", Contexts: contexts, Handler: noop},
		{Key: "tab", Label: "next file", Contexts: contexts, Handler: noop},
		{Key: "[/]", Label: "prev/next hunk", Contexts: contexts, Handler: noop},
		{Key: "/", Label: "search", Contexts: contexts, Handler: noop},
		{Key: "j", Label: "scroll down", Contexts: contexts, Handler: noop},
		{Key: "k", Label: "scroll up", Contexts: contexts, Handler: noop},
		{Key: "ctrl+d", Label: "half-page down", Contexts: contexts, Handler: noop},
		{Key: "ctrl+u", Label: "half-page up", Contexts: contexts, Handler: noop},
		{Key: "esc", Label: "back", Contexts: contexts, Handler: noop},
	}
}

// RenderFooter renders the diff view's footer (search bar or shared bindings).
func (c *DiffViewCore) RenderFooter(state *State, ctx Context, contentWidth int) string {
	if c.searchActive {
		c.searchInput.SetWidth(contentWidth - 5)
		return c.searchInput.View() + "\n" + RenderSearchFooter(c.matchIndex, len(c.matchPositions), c.searchInputMode, c.searchQuery != "")
	}
	return RenderFooter(state.Registry, ctx, nil)
}

func (c *DiffViewCore) updateSearch() {
	c.searchQuery = c.searchInput.Value()
	if c.searchQuery == "" {
		c.matchPositions = nil
		c.matchIndex = 0
		return
	}
	c.matchPositions = nil
	pattern := CompileSearchPattern(c.searchQuery)
	if pattern == nil {
		c.matchIndex = 0
		return
	}
	for i, r := range c.plan.Rows {
		plain := planRowPlainText(r)
		if pattern.MatchString(plain) {
			c.matchPositions = append(c.matchPositions, i)
		}
	}
	if len(c.matchPositions) > 0 {
		c.matchIndex = 1
		c.cursor = c.matchPositions[0]
		c.syncSelectedFile()
		c.ensureCursorVisible()
	} else {
		c.matchIndex = 0
	}
}

func (c *DiffViewCore) nextMatch() {
	if len(c.matchPositions) == 0 {
		return
	}
	c.matchIndex++
	if c.matchIndex > len(c.matchPositions) {
		c.matchIndex = 1
	}
	c.cursor = c.matchPositions[c.matchIndex-1]
	c.syncSelectedFile()
	c.ensureCursorVisible()
}

func (c *DiffViewCore) prevMatch() {
	if len(c.matchPositions) == 0 {
		return
	}
	c.matchIndex--
	if c.matchIndex < 1 {
		c.matchIndex = len(c.matchPositions)
	}
	c.cursor = c.matchPositions[c.matchIndex-1]
	c.syncSelectedFile()
	c.ensureCursorVisible()
}

func (c *DiffViewCore) exitSearch() {
	c.searchActive = false
	c.searchInputMode = false
	c.searchQuery = ""
	c.searchInput.Blur()
	c.matchPositions = nil
	c.matchIndex = 0
}
