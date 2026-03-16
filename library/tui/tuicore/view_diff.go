// view_diff.go - Generic commit diff view for workdir commits
package tuicore

import (
	"fmt"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/core/git"
)

func init() {
	RegisterViewMeta(ViewMeta{Path: "/diff", Context: CommitDiff, Title: "Commit Diff", Icon: "±"})
}

// CommitDiffView displays the diff for a single commit.
type CommitDiffView struct {
	workdir       string
	width         int
	height        int
	loaded        bool
	loadErr       error
	subject       string
	diffs         []git.FileDiff
	stats         git.DiffStats
	selectedFile  int
	expandedFiles map[int]bool
	diffMode      int // 0=unified, 1=split, 2=fullscreen (split + no sidebar)
	cursor        int
	scroll        int
	scrollH       int
	maxScrollH    int
	renderedLines []string
	lineRenderFn  []func(scrollH int) string // parallel to renderedLines; nil = static
	buildScrollH  int
	lineMetadata  []commitDiffLineInfo
	foldedHunks   map[string]bool
	// search
	searchActive    bool
	searchInputMode bool
	searchInput     textinput.Model
	searchQuery     string
	matchPositions  []int
	matchIndex      int
	zonePrefix      string
}

// commitDiffLineInfo maps a rendered line back to its source file/hunk.
type commitDiffLineInfo struct {
	fileIdx int
	file    string
	hunkIdx int
}

// commitDiffLoadedMsg carries loaded diff data.
type commitDiffLoadedMsg struct {
	subject string
	diffs   []git.FileDiff
	stats   git.DiffStats
	err     error
}

// NewCommitDiffView creates a new generic commit diff view.
func NewCommitDiffView(workdir string) *CommitDiffView {
	input := textinput.New()
	input.Placeholder = "Search..."
	input.CharLimit = 100
	input.Prompt = "> "
	StyleTextInput(&input, Title, Title, Dim)
	return &CommitDiffView{
		workdir:       workdir,
		expandedFiles: make(map[int]bool),
		foldedHunks:   make(map[string]bool),
		searchInput:   input,
		zonePrefix:    zone.NewPrefix(),
	}
}

// SetSize sets the view dimensions.
func (v *CommitDiffView) SetSize(w, h int) {
	if v.width == w && v.height == h {
		return
	}
	v.width = w
	v.height = h
	if v.loaded && len(v.diffs) > 0 {
		v.buildRenderedLines()
	}
}

// Activate loads diff data for the commit.
func (v *CommitDiffView) Activate(state *State) tea.Cmd {
	v.cursor = 0
	v.scroll = 0
	v.scrollH = 0
	v.loaded = false
	v.loadErr = nil
	v.selectedFile = 0
	v.expandedFiles = make(map[int]bool)
	v.foldedHunks = make(map[string]bool)
	v.searchActive = false
	v.searchInputMode = false
	v.searchQuery = ""
	v.searchInput.SetValue("")
	v.matchPositions = nil
	v.matchIndex = 0
	commit := state.Router.Location().Param("commit")
	workdir := v.workdir
	return func() tea.Msg {
		if commit == "" {
			return commitDiffLoadedMsg{err: fmt.Errorf("no commit specified")}
		}
		msg, _ := git.GetCommitMessage(workdir, commit)
		subject := strings.SplitN(strings.TrimSpace(msg), "\n", 2)[0]
		diffs, err := git.GetDiff(workdir, commit+"^", commit)
		if err != nil {
			return commitDiffLoadedMsg{err: fmt.Errorf("diff: %w", err)}
		}
		stats, _ := git.GetDiffStats(workdir, commit+"^", commit)
		return commitDiffLoadedMsg{subject: subject, diffs: diffs, stats: stats}
	}
}

// IsInputActive returns true when search input is active.
func (v *CommitDiffView) IsInputActive() bool { return v.searchInputMode }

// Update handles messages.
func (v *CommitDiffView) Update(msg tea.Msg, state *State) tea.Cmd {
	switch msg := msg.(type) {
	case commitDiffLoadedMsg:
		v.loaded = true
		v.loadErr = msg.err
		v.subject = msg.subject
		v.diffs = msg.diffs
		v.stats = msg.stats
		v.buildRenderedLines()
		return nil
	case tea.MouseMsg:
		if v.searchInputMode {
			return nil
		}
		switch msg.(type) {
		case tea.MouseWheelMsg:
			m := msg.Mouse()
			if m.Button == tea.MouseWheelUp {
				if v.cursor > 0 {
					v.cursor--
					v.updateSelectedFileFromCursor()
					v.ensureCursorVisible()
				}
			} else {
				if v.cursor < len(v.renderedLines)-1 {
					v.cursor++
					v.updateSelectedFileFromCursor()
					v.ensureCursorVisible()
				}
			}
			return nil
		case tea.MouseClickMsg:
			idx := ZoneClicked(msg, len(v.renderedLines), v.zonePrefix)
			if idx >= 0 && idx < len(v.renderedLines) {
				prev := v.cursor
				v.cursor = idx
				v.updateSelectedFileFromCursor()
				v.ensureCursorVisible()
				if prev == idx {
					v.toggleFileAtCursor()
					v.buildRenderedLines()
				}
			}
			return nil
		}
	case tea.KeyPressMsg:
		if v.searchInputMode {
			switch msg.String() {
			case "esc":
				v.exitSearch()
				return nil
			case "enter":
				v.searchInputMode = false
				v.searchInput.Blur()
				if v.searchQuery == "" {
					v.searchActive = false
				}
				return nil
			}
			var cmd tea.Cmd
			v.searchInput, cmd = v.searchInput.Update(msg)
			v.updateSearch()
			return cmd
		}
		if v.searchActive {
			switch msg.String() {
			case "n":
				v.nextMatch()
				return nil
			case "N":
				v.prevMatch()
				return nil
			case "/":
				v.searchInputMode = true
				return v.searchInput.Focus()
			case "esc":
				v.exitSearch()
				return nil
			}
		}
		switch msg.String() {
		case "j", "down":
			if v.cursor < len(v.renderedLines)-1 {
				v.cursor++
				v.updateSelectedFileFromCursor()
				v.ensureCursorVisible()
			}
			return nil
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
				v.updateSelectedFileFromCursor()
				v.ensureCursorVisible()
			}
			return nil
		case "ctrl+d", "pgdown":
			half := v.viewportHeight() / 2
			v.cursor += half
			if v.cursor > len(v.renderedLines)-1 {
				v.cursor = len(v.renderedLines) - 1
			}
			if v.cursor < 0 {
				v.cursor = 0
			}
			v.updateSelectedFileFromCursor()
			v.ensureCursorVisible()
			return nil
		case "ctrl+u", "pgup":
			half := v.viewportHeight() / 2
			v.cursor -= half
			if v.cursor < 0 {
				v.cursor = 0
			}
			v.updateSelectedFileFromCursor()
			v.ensureCursorVisible()
			return nil
		case "g":
			v.cursor = 0
			v.updateSelectedFileFromCursor()
			v.ensureCursorVisible()
			return nil
		case "G":
			if len(v.renderedLines) > 0 {
				v.cursor = len(v.renderedLines) - 1
			}
			v.updateSelectedFileFromCursor()
			v.ensureCursorVisible()
			return nil
		case "l", "right":
			v.scrollH += 4
			if v.scrollH > v.maxScrollH {
				v.scrollH = v.maxScrollH
			}
			return nil
		case "h", "left":
			if v.scrollH > 0 {
				v.scrollH -= 4
				if v.scrollH < 0 {
					v.scrollH = 0
				}
			}
			return nil
		case "enter":
			v.toggleFileAtCursor()
			v.buildRenderedLines()
			return nil
		case "tab":
			if v.selectedFile < len(v.diffs)-1 {
				v.selectedFile++
				v.moveCursorToFile(v.selectedFile)
			}
			return nil
		case "shift+tab":
			if v.selectedFile > 0 {
				v.selectedFile--
				v.moveCursorToFile(v.selectedFile)
			}
			return nil
		case "v":
			prev := v.diffMode
			v.diffMode = (v.diffMode + 1) % 3
			v.buildRenderedLines()
			if prev == 2 || v.diffMode == 2 {
				return func() tea.Msg {
					return NavVisibilityMsg{Hidden: v.diffMode == 2}
				}
			}
			return nil
		case "]":
			v.jumpToNextHunk()
			return nil
		case "[":
			v.jumpToPrevHunk()
			return nil
		case "f":
			v.toggleHunkFold()
			return nil
		case "E":
			v.toggleAllFiles()
			return nil
		case "/":
			v.searchActive = true
			v.searchInputMode = true
			v.searchInput.SetValue("")
			return v.searchInput.Focus()
		case "esc":
			if v.diffMode == 2 {
				v.diffMode = 1
				v.buildRenderedLines()
				return func() tea.Msg {
					return NavVisibilityMsg{Hidden: false}
				}
			}
			return func() tea.Msg {
				return NavigateMsg{Action: NavBack}
			}
		}
	}
	if v.searchInputMode {
		var cmd tea.Cmd
		v.searchInput, cmd = v.searchInput.Update(msg)
		return cmd
	}
	return nil
}

// Render renders the view.
func (v *CommitDiffView) Render(state *State) string {
	wrapper := NewViewWrapper(state)
	var content string
	if !v.loaded {
		content = "Loading diff..."
	} else if v.loadErr != nil {
		content = Dim.Render(fmt.Sprintf("Error: %s", v.loadErr.Error()))
	} else if len(v.diffs) == 0 {
		content = Dim.Render("No file changes found")
	} else {
		content = v.renderVisible()
	}
	var footer string
	if v.searchActive {
		v.searchInput.SetWidth(wrapper.ContentWidth() - 5)
		footer = v.searchInput.View() + "\n" + RenderSearchFooter(wrapper.ContentWidth(), v.matchIndex, len(v.matchPositions), v.searchInputMode, v.searchQuery != "")
	} else {
		footer = RenderFooter(state.Registry, CommitDiff, wrapper.ContentWidth(), nil)
	}
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *CommitDiffView) Title() string {
	if v.subject == "" {
		return "±  Commit Diff"
	}
	return fmt.Sprintf("±  %s · %s", TruncateToWidth(v.subject, 40), RenderDiffStatsBadge(v.stats.Added, v.stats.Removed))
}

// Bindings returns keybindings for this view.
func (v *CommitDiffView) Bindings() []Binding {
	noop := func(ctx *HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []Binding{
		{Key: "enter", Label: "expand/collapse", Contexts: []Context{CommitDiff}, Handler: noop},
		{Key: "v", Label: "view mode", Contexts: []Context{CommitDiff}, Handler: noop},
		{Key: "tab", Label: "next file", Contexts: []Context{CommitDiff}, Handler: noop},
		{Key: "[/]", Label: "prev/next hunk", Contexts: []Context{CommitDiff}, Handler: noop},
		{Key: "f", Label: "fold hunk", Contexts: []Context{CommitDiff}, Handler: noop},
		{Key: "E", Label: "expand/collapse all", Contexts: []Context{CommitDiff}, Handler: noop},
		{Key: "/", Label: "search", Contexts: []Context{CommitDiff}, Handler: noop},
		{Key: "j", Label: "scroll down", Contexts: []Context{CommitDiff}, Handler: noop},
		{Key: "k", Label: "scroll up", Contexts: []Context{CommitDiff}, Handler: noop},
		{Key: "ctrl+d", Label: "half-page down", Contexts: []Context{CommitDiff}, Handler: noop},
		{Key: "ctrl+u", Label: "half-page up", Contexts: []Context{CommitDiff}, Handler: noop},
		{Key: "esc", Label: "back", Contexts: []Context{CommitDiff}, Handler: noop},
	}
}

// buildRenderedLines pre-renders all diff content into line slices.
func (v *CommitDiffView) buildRenderedLines() {
	v.renderedLines = nil
	v.lineMetadata = nil
	v.lineRenderFn = nil
	contentWidth := v.width - 1
	opts := DiffRenderOptions{
		Width:   contentWidth,
		ScrollH: v.scrollH,
	}
	maxContentWidth := 0
	minAvailWidth := contentWidth
	for i, diff := range v.diffs {
		expanded := v.expandedFiles[i]
		header := RenderDiffHeader(diff, expanded)
		v.renderedLines = append(v.renderedLines, header)
		v.lineMetadata = append(v.lineMetadata, commitDiffLineInfo{fileIdx: i, hunkIdx: -1})
		v.lineRenderFn = append(v.lineRenderFn, nil)
		if !expanded {
			continue
		}
		if diff.Binary {
			v.renderedLines = append(v.renderedLines, Dim.Render("  Binary file"))
			v.lineMetadata = append(v.lineMetadata, commitDiffLineInfo{fileIdx: i, hunkIdx: -1})
			v.lineRenderFn = append(v.lineRenderFn, nil)
			continue
		}
		lang := opts.Language
		if lang == "" {
			path := diff.NewPath
			if path == "" {
				path = diff.OldPath
			}
			lang = DetectLanguageFromPath(path)
		}
		filePath := diff.NewPath
		if filePath == "" {
			filePath = diff.OldPath
		}
		for hi, hunk := range diff.Hunks {
			foldKey := fmt.Sprintf("%d:%d", i, hi)
			if v.foldedHunks[foldKey] {
				v.renderedLines = append(v.renderedLines, DiffHunkHeaderStyle().Render(hunk.Header))
				v.lineMetadata = append(v.lineMetadata, commitDiffLineInfo{fileIdx: i, file: filePath, hunkIdx: hi})
				v.lineRenderFn = append(v.lineRenderFn, nil)
				v.renderedLines = append(v.renderedLines, Dim.Render(fmt.Sprintf("  ··· (%d lines hidden)", len(hunk.Lines))))
				v.lineMetadata = append(v.lineMetadata, commitDiffLineInfo{fileIdx: i, file: filePath, hunkIdx: hi})
				v.lineRenderFn = append(v.lineRenderFn, nil)
				continue
			}
			gutterVisual := GutterSize(hunk)
			var availWidth int
			if v.diffMode >= 1 {
				availWidth = (contentWidth-1)/2 - gutterVisual - 3
			} else {
				availWidth = contentWidth - gutterVisual - 3
			}
			if availWidth < minAvailWidth {
				minAvailWidth = availWidth
			}
			for _, dl := range hunk.Lines {
				w := len([]rune(strings.ReplaceAll(strings.ReplaceAll(dl.Content, "\r", ""), "\t", "    ")))
				if w > maxContentWidth {
					maxContentWidth = w
				}
			}
			if v.diffMode >= 1 {
				rendered := RenderHunkSplit(hunk, lang, opts)
				hunkLines := strings.Split(rendered, "\n")
				// Shared cache for re-rendering this hunk at different scrollH
				var cachedSplitLines []string
				cachedSplitScrollH := v.scrollH
				for idx, hl := range hunkLines {
					v.renderedLines = append(v.renderedLines, hl)
					v.lineMetadata = append(v.lineMetadata, commitDiffLineInfo{fileIdx: i, file: filePath, hunkIdx: hi})
					lineIdx := idx
					v.lineRenderFn = append(v.lineRenderFn, func(scrollH int) string {
						if cachedSplitScrollH != scrollH || cachedSplitLines == nil {
							o := DiffRenderOptions{Width: contentWidth, ScrollH: scrollH}
							cachedSplitLines = strings.Split(RenderHunkSplit(hunk, lang, o), "\n")
							cachedSplitScrollH = scrollH
						}
						if lineIdx < len(cachedSplitLines) {
							return cachedSplitLines[lineIdx]
						}
						return ""
					})
				}
			} else {
				v.buildHunkLinesUnified(hunk, hi, i, filePath, lang, opts)
			}
		}
	}
	v.maxScrollH = maxContentWidth - minAvailWidth
	if v.maxScrollH < 0 {
		v.maxScrollH = 0
	}
	if v.scrollH > v.maxScrollH {
		v.scrollH = v.maxScrollH
	}
	v.buildScrollH = v.scrollH
}

// buildHunkLinesUnified renders a hunk in unified mode and appends to renderedLines.
func (v *CommitDiffView) buildHunkLinesUnified(hunk git.Hunk, hunkIdx, fileIdx int, filePath, lang string, opts DiffRenderOptions) {
	v.renderedLines = append(v.renderedLines, DiffHunkHeaderStyle().Render(hunk.Header))
	v.lineMetadata = append(v.lineMetadata, commitDiffLineInfo{fileIdx: fileIdx, file: filePath, hunkIdx: hunkIdx})
	v.lineRenderFn = append(v.lineRenderFn, nil)
	gw := GutterSize(hunk)
	w := opts.Width
	for _, dl := range hunk.Lines {
		rendered := RenderDiffLinePublic(dl, lang, gw, w, opts.ScrollH)
		v.renderedLines = append(v.renderedLines, rendered)
		v.lineMetadata = append(v.lineMetadata, commitDiffLineInfo{fileIdx: fileIdx, file: filePath, hunkIdx: hunkIdx})
		v.lineRenderFn = append(v.lineRenderFn, func(scrollH int) string {
			return RenderDiffLinePublic(dl, lang, gw, w, scrollH)
		})
	}
}

// renderVisible renders the visible slice of pre-rendered lines.
func (v *CommitDiffView) renderVisible() string {
	if len(v.renderedLines) == 0 {
		return Dim.Render("No changes")
	}
	summary := fmt.Sprintf("Files: %d changed  %s",
		v.stats.Files, RenderDiffStatsBadge(v.stats.Added, v.stats.Removed))
	vh := v.viewportHeight()
	if v.scroll < 0 {
		v.scroll = 0
	}
	if v.scroll >= len(v.renderedLines) {
		v.scroll = len(v.renderedLines) - 1
	}
	end := v.scroll + vh
	if end > len(v.renderedLines) {
		end = len(v.renderedLines)
	}
	lazyRender := v.scrollH != v.buildScrollH
	indicator := lipgloss.NewStyle().Foreground(lipgloss.Color(IdentityFollowing)).Render("▌")
	var visible []string
	for i := v.scroll; i < end; i++ {
		var line string
		if lazyRender && i < len(v.lineRenderFn) && v.lineRenderFn[i] != nil {
			line = v.lineRenderFn[i](v.scrollH)
		} else {
			line = v.renderedLines[i]
		}
		if v.searchActive && v.searchQuery != "" {
			line = HighlightInText(line, v.searchQuery)
		}
		if i == v.cursor {
			line = indicator + line
		} else {
			line = " " + line
		}
		line = MarkZone(ZoneID(v.zonePrefix, i), line)
		visible = append(visible, line)
	}
	return summary + "\n\n" + strings.Join(visible, "\n")
}

// viewportHeight returns the number of visible diff lines.
func (v *CommitDiffView) viewportHeight() int {
	vh := v.height - 4
	if vh < 1 {
		vh = 1
	}
	return vh
}

// ensureCursorVisible adjusts scroll so the cursor stays in the viewport.
func (v *CommitDiffView) ensureCursorVisible() {
	vh := v.viewportHeight()
	if v.cursor < v.scroll {
		v.scroll = v.cursor
	}
	if v.cursor >= v.scroll+vh {
		v.scroll = v.cursor - vh + 1
	}
}

// updateSelectedFileFromCursor syncs selectedFile to the file under cursor.
func (v *CommitDiffView) updateSelectedFileFromCursor() {
	if v.cursor >= 0 && v.cursor < len(v.lineMetadata) {
		v.selectedFile = v.lineMetadata[v.cursor].fileIdx
	}
}

// toggleAllFiles expands all files if any are collapsed, otherwise collapses all.
func (v *CommitDiffView) toggleAllFiles() {
	anyCollapsed := false
	for i := range v.diffs {
		if !v.expandedFiles[i] {
			anyCollapsed = true
			break
		}
	}
	for i := range v.diffs {
		v.expandedFiles[i] = anyCollapsed
	}
	v.buildRenderedLines()
}

// toggleFileAtCursor expands/collapses the file under the cursor.
func (v *CommitDiffView) toggleFileAtCursor() {
	if v.cursor >= 0 && v.cursor < len(v.lineMetadata) {
		idx := v.lineMetadata[v.cursor].fileIdx
		if idx >= 0 && idx < len(v.diffs) {
			v.expandedFiles[idx] = !v.expandedFiles[idx]
		}
	}
}

// moveCursorToFile moves the cursor to a file header line.
func (v *CommitDiffView) moveCursorToFile(idx int) {
	for i, meta := range v.lineMetadata {
		if meta.fileIdx == idx && meta.file == "" {
			v.cursor = i
			v.ensureCursorVisible()
			return
		}
	}
}

// jumpToNextHunk moves cursor to the next hunk.
func (v *CommitDiffView) jumpToNextHunk() {
	if len(v.lineMetadata) == 0 {
		return
	}
	currentHunk := v.lineMetadata[v.cursor].hunkIdx
	currentFile := v.lineMetadata[v.cursor].fileIdx
	for i := v.cursor + 1; i < len(v.lineMetadata); i++ {
		m := v.lineMetadata[i]
		if m.hunkIdx >= 0 && (m.hunkIdx != currentHunk || m.fileIdx != currentFile) {
			v.cursor = i
			if i+1 < len(v.lineMetadata) && v.lineMetadata[i+1].hunkIdx == m.hunkIdx && v.lineMetadata[i+1].fileIdx == m.fileIdx {
				v.cursor = i + 1
			}
			v.updateSelectedFileFromCursor()
			v.ensureCursorVisible()
			return
		}
	}
}

// jumpToPrevHunk moves cursor to the previous hunk.
func (v *CommitDiffView) jumpToPrevHunk() {
	if len(v.lineMetadata) == 0 || v.cursor == 0 {
		return
	}
	currentHunk := v.lineMetadata[v.cursor].hunkIdx
	currentFile := v.lineMetadata[v.cursor].fileIdx
	i := v.cursor - 1
	for i >= 0 {
		m := v.lineMetadata[i]
		if m.hunkIdx >= 0 && (m.hunkIdx != currentHunk || m.fileIdx != currentFile) {
			break
		}
		i--
	}
	if i < 0 {
		return
	}
	targetHunk := v.lineMetadata[i].hunkIdx
	targetFile := v.lineMetadata[i].fileIdx
	for i > 0 {
		prev := v.lineMetadata[i-1]
		if prev.hunkIdx != targetHunk || prev.fileIdx != targetFile {
			break
		}
		i--
	}
	v.cursor = i
	if i+1 < len(v.lineMetadata) && v.lineMetadata[i+1].hunkIdx == targetHunk && v.lineMetadata[i+1].fileIdx == targetFile {
		v.cursor = i + 1
	}
	v.updateSelectedFileFromCursor()
	v.ensureCursorVisible()
}

// toggleHunkFold folds or unfolds the hunk at the cursor.
func (v *CommitDiffView) toggleHunkFold() {
	if v.cursor >= len(v.lineMetadata) {
		return
	}
	info := v.lineMetadata[v.cursor]
	if info.hunkIdx < 0 {
		return
	}
	key := fmt.Sprintf("%d:%d", info.fileIdx, info.hunkIdx)
	v.foldedHunks[key] = !v.foldedHunks[key]
	v.buildRenderedLines()
}

// updateSearch updates the search query and finds matches.
func (v *CommitDiffView) updateSearch() {
	v.searchQuery = v.searchInput.Value()
	if v.searchQuery == "" {
		v.matchPositions = nil
		v.matchIndex = 0
		return
	}
	v.matchPositions = nil
	pattern := CompileSearchPattern(v.searchQuery)
	for i, line := range v.renderedLines {
		plain := ansiStripPattern.ReplaceAllString(line, "")
		if pattern != nil && pattern.MatchString(plain) {
			v.matchPositions = append(v.matchPositions, i)
		}
	}
	if len(v.matchPositions) > 0 {
		v.matchIndex = 1
		v.cursor = v.matchPositions[0]
		v.updateSelectedFileFromCursor()
		v.ensureCursorVisible()
	} else {
		v.matchIndex = 0
	}
}

// nextMatch moves to the next search match.
func (v *CommitDiffView) nextMatch() {
	if len(v.matchPositions) == 0 {
		return
	}
	v.matchIndex++
	if v.matchIndex > len(v.matchPositions) {
		v.matchIndex = 1
	}
	v.cursor = v.matchPositions[v.matchIndex-1]
	v.updateSelectedFileFromCursor()
	v.ensureCursorVisible()
}

// prevMatch moves to the previous search match.
func (v *CommitDiffView) prevMatch() {
	if len(v.matchPositions) == 0 {
		return
	}
	v.matchIndex--
	if v.matchIndex < 1 {
		v.matchIndex = len(v.matchPositions)
	}
	v.cursor = v.matchPositions[v.matchIndex-1]
	v.updateSelectedFileFromCursor()
	v.ensureCursorVisible()
}

// exitSearch closes search mode.
func (v *CommitDiffView) exitSearch() {
	v.searchActive = false
	v.searchInputMode = false
	v.searchQuery = ""
	v.searchInput.Blur()
	v.matchPositions = nil
	v.matchIndex = 0
}

var ansiStripPattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
