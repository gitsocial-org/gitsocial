// view_diff.go - Files changed diff view for pull request review
package tuireview

import (
	"fmt"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// DiffView displays files changed in a pull request.
type DiffView struct {
	workdir          string
	width            int
	height           int
	loaded           bool
	pr               *review.PullRequest
	headCommit       string
	diffs            []git.FileDiff
	stats            git.DiffStats
	feedbacks        []review.Feedback
	selectedFile     int
	expandedFiles    map[int]bool
	diffMode         int // 0=unified, 1=split, 2=fullscreen (split + no sidebar)
	cursor           int
	scroll           int
	scrollH          int
	maxScrollH       int
	renderedLines    []string
	lineRenderFn     []func(scrollH int) string // parallel to renderedLines; nil = static
	buildScrollH     int
	lineMetadata     []lineInfo
	foldedHunks      map[string]bool
	expandedContexts map[string]bool
	commentPositions []int
	commentIndex     int
	userEmail        string
	showEmail        bool
	searchActive     bool
	searchInputMode  bool
	searchInput      textinput.Model
	searchQuery      string
	matchPositions   []int
	matchIndex       int
	zonePrefix       string
	diffError        string
}

// lineInfo maps a rendered line back to its source for inline feedback.
type lineInfo struct {
	fileIdx            int
	file               string
	oldLine            int
	newLine            int
	hunkIdx            int    // -1 for file headers, 0+ for hunk lines
	isFeedback         bool   // true for inline feedback lines
	isCollapsedContext bool   // true for collapsed context placeholder
	contextKey         string // key for expandedContexts lookup
}

// NewDiffView creates a new diff view.
func NewDiffView(workdir string) *DiffView {
	input := textinput.New()
	input.Placeholder = "Search..."
	input.CharLimit = 100
	input.Prompt = "> "
	tuicore.StyleTextInput(&input, tuicore.Title, tuicore.Title, tuicore.Dim)
	return &DiffView{
		workdir:          workdir,
		expandedFiles:    make(map[int]bool),
		foldedHunks:      make(map[string]bool),
		expandedContexts: make(map[string]bool),
		searchInput:      input,
		zonePrefix:       zone.NewPrefix(),
	}
}

// SetSize sets the view dimensions.
func (v *DiffView) SetSize(w, h int) {
	if v.width == w && v.height == h {
		return
	}
	v.width = w
	v.height = h
	if v.loaded && len(v.diffs) > 0 {
		v.buildRenderedLines()
	}
}

// Activate loads diff data for the pull request.
func (v *DiffView) Activate(state *tuicore.State) tea.Cmd {
	v.userEmail = state.UserEmail
	v.showEmail = state.ShowEmailOnCards
	v.cursor = 0
	v.scroll = 0
	v.scrollH = 0
	v.loaded = false
	v.selectedFile = 0
	v.expandedFiles = make(map[int]bool)
	v.foldedHunks = make(map[string]bool)
	v.expandedContexts = make(map[string]bool)
	v.commentPositions = nil
	v.commentIndex = 0
	v.searchActive = false
	v.searchInputMode = false
	v.searchQuery = ""
	v.searchInput.SetValue("")
	v.matchPositions = nil
	v.matchIndex = 0
	prID := state.Router.Location().Param("prID")
	commit := state.Router.Location().Param("commit")
	cacheDir := state.CacheDir
	return func() tea.Msg {
		if err := review.SyncWorkspaceToCache(v.workdir); err != nil {
			log.Debug("review sync before diff load failed", "error", err)
		}
		res := review.GetPR(prID)
		if !res.Success {
			return diffLoadedMsg{}
		}
		pr := res.Data
		diffCtx := review.ResolvePRDiff(v.workdir, cacheDir, &pr, commit)
		forkFetched := diffCtx.Workdir != v.workdir
		if diffCtx.Base == "" || diffCtx.Head == "" {
			return diffLoadedMsg{pr: &pr, diffError: diffCtx.Error}
		}
		diffs, _ := git.GetDiff(diffCtx.Workdir, diffCtx.Base, diffCtx.Head)
		stats, _ := git.GetDiffStats(diffCtx.Workdir, diffCtx.Base, diffCtx.Head)
		headCommit, _ := git.ReadRef(diffCtx.Workdir, diffCtx.Head)
		hash := extractHashFromID(pr.ID)
		var feedbacks []review.Feedback
		fbRes := review.GetFeedbackForPR(pr.Repository, hash, pr.Branch)
		if fbRes.Success {
			feedbacks = fbRes.Data
		}
		return diffLoadedMsg{pr: &pr, headCommit: headCommit, diffs: diffs, stats: stats, feedbacks: feedbacks, forkFetched: forkFetched}
	}
}

// Deactivate is called when the view is hidden.
func (v *DiffView) Deactivate() {}

// IsInputActive returns true when search input is active.
func (v *DiffView) IsInputActive() bool { return v.searchInputMode }

// Update handles messages.
func (v *DiffView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case diffLoadedMsg:
		v.loaded = true
		v.pr = msg.pr
		v.headCommit = msg.headCommit
		v.diffs = msg.diffs
		v.stats = msg.stats
		v.feedbacks = msg.feedbacks
		v.diffError = msg.diffError
		v.buildRenderedLines()
		if msg.forkFetched {
			return refreshCacheSize(state.CacheDir)
		}
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
				maxCursor := len(v.renderedLines) - 1
				if v.cursor < maxCursor {
					v.cursor++
					v.updateSelectedFileFromCursor()
					v.ensureCursorVisible()
				}
			}
			return nil
		case tea.MouseClickMsg:
			idx := tuicore.ZoneClicked(msg, len(v.renderedLines), v.zonePrefix)
			if idx >= 0 && idx < len(v.renderedLines) {
				prevCursor := v.cursor
				v.cursor = idx
				v.updateSelectedFileFromCursor()
				v.ensureCursorVisible()
				if prevCursor == idx {
					v.toggleFileAtCursor()
					v.buildRenderedLines()
				}
			}
			return nil
		}
	case tea.KeyPressMsg:
		// Search input mode takes priority
		if v.searchInputMode {
			switch msg.String() {
			case "esc":
				v.exitDiffSearch()
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
			v.updateDiffSearch()
			return cmd
		}
		// Search navigation mode
		if v.searchActive {
			switch msg.String() {
			case "n":
				v.nextDiffMatch()
				return nil
			case "N":
				v.prevDiffMatch()
				return nil
			case "/":
				v.searchInputMode = true
				return v.searchInput.Focus()
			case "esc":
				v.exitDiffSearch()
				return nil
			}
		}
		switch msg.String() {
		case "j", "down":
			maxCursor := len(v.renderedLines) - 1
			if v.cursor < maxCursor {
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
			maxCursor := len(v.renderedLines) - 1
			v.cursor += half
			if v.cursor > maxCursor {
				v.cursor = maxCursor
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
					return tuicore.NavVisibilityMsg{Hidden: v.diffMode == 2}
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
		case "e":
			v.expandContextAtCursor()
			return nil
		case "E":
			v.toggleAllFiles()
			return nil
		case "n":
			if !v.searchActive {
				v.jumpToNextComment()
			}
			return nil
		case "N":
			if !v.searchActive {
				v.jumpToPrevComment()
			}
			return nil
		case "/":
			v.searchActive = true
			v.searchInputMode = true
			v.searchInput.SetValue("")
			return v.searchInput.Focus()
		case "c":
			if v.pr != nil && v.cursor < len(v.lineMetadata) {
				info := v.lineMetadata[v.cursor]
				if info.file != "" && (info.oldLine > 0 || info.newLine > 0) {
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocReviewFeedbackInline(v.pr.ID, info.file, info.oldLine, info.newLine, v.headCommit),
							Action:   tuicore.NavPush,
						}
					}
				}
			}
			return nil
		case "esc":
			if v.diffMode == 2 {
				v.diffMode = 1
				v.buildRenderedLines()
				return func() tea.Msg {
					return tuicore.NavVisibilityMsg{Hidden: false}
				}
			}
			if v.pr != nil {
				return func() tea.Msg {
					return tuicore.NavigateMsg{Action: tuicore.NavBack}
				}
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

// jumpToNextHunk moves cursor to the first diff line of the next hunk.
func (v *DiffView) jumpToNextHunk() {
	if len(v.lineMetadata) == 0 {
		return
	}
	currentHunk := v.lineMetadata[v.cursor].hunkIdx
	currentFile := v.lineMetadata[v.cursor].fileIdx
	// Scan forward for a different hunk
	for i := v.cursor + 1; i < len(v.lineMetadata); i++ {
		m := v.lineMetadata[i]
		if m.hunkIdx >= 0 && (m.hunkIdx != currentHunk || m.fileIdx != currentFile) {
			// Move to first diff line (skip hunk header)
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

// jumpToPrevHunk moves cursor to the first diff line of the previous hunk.
func (v *DiffView) jumpToPrevHunk() {
	if len(v.lineMetadata) == 0 || v.cursor == 0 {
		return
	}
	currentHunk := v.lineMetadata[v.cursor].hunkIdx
	currentFile := v.lineMetadata[v.cursor].fileIdx
	// Scan backward past current hunk
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
	// Now find the start of that hunk
	targetHunk := v.lineMetadata[i].hunkIdx
	targetFile := v.lineMetadata[i].fileIdx
	for i > 0 {
		prev := v.lineMetadata[i-1]
		if prev.hunkIdx != targetHunk || prev.fileIdx != targetFile {
			break
		}
		i--
	}
	// i is the hunk header line; move to first diff line
	v.cursor = i
	if i+1 < len(v.lineMetadata) && v.lineMetadata[i+1].hunkIdx == targetHunk && v.lineMetadata[i+1].fileIdx == targetFile {
		v.cursor = i + 1
	}
	v.updateSelectedFileFromCursor()
	v.ensureCursorVisible()
}

// toggleHunkFold folds or unfolds the hunk at the cursor.
func (v *DiffView) toggleHunkFold() {
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

// expandContextAtCursor expands a collapsed context region at the cursor.
func (v *DiffView) expandContextAtCursor() {
	if v.cursor >= len(v.lineMetadata) {
		return
	}
	info := v.lineMetadata[v.cursor]
	if !info.isCollapsedContext || info.contextKey == "" {
		return
	}
	v.expandedContexts[info.contextKey] = true
	v.buildRenderedLines()
}

// jumpToNextComment moves cursor to the next inline feedback.
func (v *DiffView) jumpToNextComment() {
	if len(v.commentPositions) == 0 {
		return
	}
	for _, pos := range v.commentPositions {
		if pos > v.cursor {
			v.cursor = pos
			v.updateSelectedFileFromCursor()
			v.ensureCursorVisible()
			return
		}
	}
	// Wrap around
	v.cursor = v.commentPositions[0]
	v.updateSelectedFileFromCursor()
	v.ensureCursorVisible()
}

// jumpToPrevComment moves cursor to the previous inline feedback.
func (v *DiffView) jumpToPrevComment() {
	if len(v.commentPositions) == 0 {
		return
	}
	for i := len(v.commentPositions) - 1; i >= 0; i-- {
		if v.commentPositions[i] < v.cursor {
			v.cursor = v.commentPositions[i]
			v.updateSelectedFileFromCursor()
			v.ensureCursorVisible()
			return
		}
	}
	// Wrap around
	v.cursor = v.commentPositions[len(v.commentPositions)-1]
	v.updateSelectedFileFromCursor()
	v.ensureCursorVisible()
}

// updateDiffSearch updates the search query and finds matches.
func (v *DiffView) updateDiffSearch() {
	v.searchQuery = v.searchInput.Value()
	if v.searchQuery == "" {
		v.matchPositions = nil
		v.matchIndex = 0
		return
	}
	v.matchPositions = nil
	pattern := tuicore.CompileSearchPattern(v.searchQuery)
	for i, line := range v.renderedLines {
		plain := stripAnsi(line)
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

// nextDiffMatch moves to the next search match.
func (v *DiffView) nextDiffMatch() {
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

// prevDiffMatch moves to the previous search match.
func (v *DiffView) prevDiffMatch() {
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

// exitDiffSearch closes search mode.
func (v *DiffView) exitDiffSearch() {
	v.searchActive = false
	v.searchInputMode = false
	v.searchQuery = ""
	v.searchInput.Blur()
	v.matchPositions = nil
	v.matchIndex = 0
}

// stripAnsi removes ANSI escape sequences for plain text matching.
func stripAnsi(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// Render renders the view.
func (v *DiffView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	var content string
	if !v.loaded {
		content = "Loading diff..."
	} else if v.pr == nil {
		content = "Pull request not found"
	} else if v.diffError != "" {
		content = tuicore.Dim.Render(v.diffError)
	} else if len(v.diffs) == 0 {
		content = tuicore.Dim.Render("No file changes found")
	} else {
		content = v.renderVisible(wrapper.ContentWidth(), wrapper.ContentHeight())
	}
	var footer string
	if v.searchActive {
		v.searchInput.SetWidth(wrapper.ContentWidth() - 5)
		footer = v.searchInput.View() + "\n" + tuicore.RenderSearchFooter(wrapper.ContentWidth(), v.matchIndex, len(v.matchPositions), v.searchInputMode, v.searchQuery != "")
	} else {
		footer = tuicore.RenderFooter(state.Registry, tuicore.ReviewDiff, wrapper.ContentWidth(), nil)
	}
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *DiffView) Title() string {
	if v.pr == nil {
		return "⑂  Files Changed"
	}
	return fmt.Sprintf("⑂  Files Changed · %s · %s", tuicore.TruncateToWidth(v.pr.Subject, 30), tuicore.RenderDiffStatsBadge(v.stats.Added, v.stats.Removed))
}

// Bindings returns keybindings for this view.
func (v *DiffView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "c", Label: "comment", Contexts: []tuicore.Context{tuicore.ReviewDiff}, Handler: noop},
		{Key: "enter", Label: "expand/collapse", Contexts: []tuicore.Context{tuicore.ReviewDiff}, Handler: noop},
		{Key: "v", Label: "view mode", Contexts: []tuicore.Context{tuicore.ReviewDiff}, Handler: noop},
		{Key: "tab", Label: "next file", Contexts: []tuicore.Context{tuicore.ReviewDiff}, Handler: noop},
		{Key: "[/]", Label: "prev/next hunk", Contexts: []tuicore.Context{tuicore.ReviewDiff}, Handler: noop},
		{Key: "n/N", Label: "next/prev comment", Contexts: []tuicore.Context{tuicore.ReviewDiff}, Handler: noop},
		{Key: "f", Label: "fold hunk", Contexts: []tuicore.Context{tuicore.ReviewDiff}, Handler: noop},
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.ReviewDiff}, Handler: noop},
		{Key: "e", Label: "expand context", Contexts: []tuicore.Context{tuicore.ReviewDiff}, Handler: noop},
		{Key: "E", Label: "expand/collapse all", Contexts: []tuicore.Context{tuicore.ReviewDiff}, Handler: noop},
		{Key: "j", Label: "scroll down", Contexts: []tuicore.Context{tuicore.ReviewDiff}, Handler: noop},
		{Key: "k", Label: "scroll up", Contexts: []tuicore.Context{tuicore.ReviewDiff}, Handler: noop},
		{Key: "ctrl+d", Label: "half-page down", Contexts: []tuicore.Context{tuicore.ReviewDiff}, Handler: noop},
		{Key: "ctrl+u", Label: "half-page up", Contexts: []tuicore.Context{tuicore.ReviewDiff}, Handler: noop},
		{Key: "esc", Label: "exit mode", Contexts: []tuicore.Context{tuicore.ReviewDiff}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.ReviewDiff}, Handler: push},
	}
}

// toggleFile toggles expand/collapse of a file.
func (v *DiffView) toggleFile(idx int) {
	if idx >= 0 && idx < len(v.diffs) {
		v.expandedFiles[idx] = !v.expandedFiles[idx]
	}
}

// toggleAllFiles expands all files if any are collapsed, otherwise collapses all.
func (v *DiffView) toggleAllFiles() {
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

// buildRenderedLines pre-renders all diff content into line slices.
func (v *DiffView) buildRenderedLines() {
	v.renderedLines = nil
	v.lineMetadata = nil
	v.lineRenderFn = nil
	v.commentPositions = nil
	contentWidth := v.width - 1 // 1 char reserved for cursor indicator
	opts := tuicore.DiffRenderOptions{
		Width:   contentWidth,
		ScrollH: v.scrollH,
	}
	maxContentWidth := 0
	minAvailWidth := contentWidth
	for i, diff := range v.diffs {
		expanded := v.expandedFiles[i]
		header := tuicore.RenderDiffHeader(diff, expanded)
		v.renderedLines = append(v.renderedLines, header)
		v.lineMetadata = append(v.lineMetadata, lineInfo{fileIdx: i, hunkIdx: -1})
		v.lineRenderFn = append(v.lineRenderFn, nil)
		if !expanded {
			continue
		}
		if diff.Binary {
			v.renderedLines = append(v.renderedLines, tuicore.Dim.Render("  Binary file"))
			v.lineMetadata = append(v.lineMetadata, lineInfo{fileIdx: i, hunkIdx: -1})
			v.lineRenderFn = append(v.lineRenderFn, nil)
			continue
		}
		lang := opts.Language
		if lang == "" {
			path := diff.NewPath
			if path == "" {
				path = diff.OldPath
			}
			lang = tuicore.DetectLanguageFromPath(path)
		}
		filePath := diff.NewPath
		if filePath == "" {
			filePath = diff.OldPath
		}
		for hi, hunk := range diff.Hunks {
			foldKey := fmt.Sprintf("%d:%d", i, hi)
			if v.foldedHunks[foldKey] {
				v.renderedLines = append(v.renderedLines, tuicore.DiffHunkHeaderStyle().Render(hunk.Header))
				v.lineMetadata = append(v.lineMetadata, lineInfo{fileIdx: i, file: filePath, hunkIdx: hi})
				v.lineRenderFn = append(v.lineRenderFn, nil)
				lineCount := len(hunk.Lines)
				v.renderedLines = append(v.renderedLines, tuicore.Dim.Render(fmt.Sprintf("  ··· (%d lines hidden)", lineCount)))
				v.lineMetadata = append(v.lineMetadata, lineInfo{fileIdx: i, file: filePath, hunkIdx: hi})
				v.lineRenderFn = append(v.lineRenderFn, nil)
				continue
			}
			gutterVisual := tuicore.GutterSize(hunk)
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
				rendered := tuicore.RenderHunkSplit(hunk, lang, opts)
				hunkLines := strings.Split(rendered, "\n")
				pairs := tuicore.PairHunkLines(hunk.Lines)
				// Shared cache for re-rendering this split hunk at different scrollH
				var cachedSplitLines []string
				cachedSplitScrollH := v.scrollH
				for idx, hl := range hunkLines {
					v.renderedLines = append(v.renderedLines, hl)
					info := lineInfo{fileIdx: i, file: filePath, hunkIdx: hi}
					pairIdx := idx - 1
					if pairIdx >= 0 && pairIdx < len(pairs) {
						p := pairs[pairIdx]
						if p.Left != nil {
							info.oldLine = p.Left.OldNum
						}
						if p.Right != nil {
							info.newLine = p.Right.NewNum
						}
					}
					v.lineMetadata = append(v.lineMetadata, info)
					lineIdx := idx
					v.lineRenderFn = append(v.lineRenderFn, func(scrollH int) string {
						if cachedSplitScrollH != scrollH || cachedSplitLines == nil {
							o := tuicore.DiffRenderOptions{Width: contentWidth, ScrollH: scrollH}
							cachedSplitLines = strings.Split(tuicore.RenderHunkSplit(hunk, lang, o), "\n")
							cachedSplitScrollH = scrollH
						}
						if lineIdx < len(cachedSplitLines) {
							return cachedSplitLines[lineIdx]
						}
						return ""
					})
				}
			} else {
				hunkLines, hunkMeta, hunkFns := v.buildHunkLinesUnified(hunk, hi, i, filePath, lang, opts)
				for idx, hl := range hunkLines {
					v.renderedLines = append(v.renderedLines, hl)
					v.lineMetadata = append(v.lineMetadata, hunkMeta[idx])
					v.lineRenderFn = append(v.lineRenderFn, hunkFns[idx])
				}
			}
		}
		// Render inline feedbacks for this file
		for _, fb := range v.feedbacks {
			if fb.File == filePath {
				v.commentPositions = append(v.commentPositions, len(v.renderedLines))
				v.renderedLines = append(v.renderedLines, renderInlineFeedback(fb, v.userEmail, v.showEmail))
				v.lineMetadata = append(v.lineMetadata, lineInfo{fileIdx: i, file: filePath, hunkIdx: -1, isFeedback: true})
				v.lineRenderFn = append(v.lineRenderFn, nil)
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

// buildHunkLinesUnified renders a hunk in unified mode with context collapsing.
func (v *DiffView) buildHunkLinesUnified(hunk git.Hunk, hunkIdx, fileIdx int, filePath, lang string, opts tuicore.DiffRenderOptions) ([]string, []lineInfo, []func(scrollH int) string) {
	var lines []string
	var meta []lineInfo
	var fns []func(scrollH int) string
	// Hunk header
	lines = append(lines, tuicore.DiffHunkHeaderStyle().Render(hunk.Header))
	meta = append(meta, lineInfo{fileIdx: fileIdx, file: filePath, hunkIdx: hunkIdx})
	fns = append(fns, nil)
	// Identify context runs for collapsing
	type contextRun struct {
		start, end int // indices into hunk.Lines
	}
	var runs []contextRun
	runStart := -1
	for j, dl := range hunk.Lines {
		if dl.Type == git.LineContext {
			if runStart < 0 {
				runStart = j
			}
		} else {
			if runStart >= 0 {
				if j-runStart > 5 {
					runs = append(runs, contextRun{runStart, j})
				}
				runStart = -1
			}
		}
	}
	if runStart >= 0 && len(hunk.Lines)-runStart > 5 {
		runs = append(runs, contextRun{runStart, len(hunk.Lines)})
	}
	// Build a set of line indices that should be collapsed
	collapsedRanges := make(map[int]contextRun) // maps startIdx of collapsed block -> run
	for _, r := range runs {
		// Keep first 2 and last 2 context lines, collapse the middle
		if r.end-r.start <= 5 {
			continue
		}
		collapsedRanges[r.start+2] = contextRun{r.start + 2, r.end - 2}
	}
	gw := tuicore.GutterSize(hunk)
	w := opts.Width
	j := 0
	for j < len(hunk.Lines) {
		if cr, ok := collapsedRanges[j]; ok {
			ctxKey := fmt.Sprintf("%d:%d:%d", fileIdx, hunkIdx, j)
			if !v.expandedContexts[ctxKey] {
				hidden := cr.end - cr.start
				lines = append(lines, tuicore.Dim.Render(fmt.Sprintf("  ··· (%d lines) press e to expand", hidden)))
				meta = append(meta, lineInfo{
					fileIdx: fileIdx, file: filePath, hunkIdx: hunkIdx,
					isCollapsedContext: true, contextKey: ctxKey,
				})
				fns = append(fns, nil)
				j = cr.end
				continue
			}
		}
		dl := hunk.Lines[j]
		rendered := tuicore.RenderDiffLinePublic(dl, lang, gw, w, opts.ScrollH)
		lines = append(lines, rendered)
		info := lineInfo{fileIdx: fileIdx, file: filePath, hunkIdx: hunkIdx}
		switch dl.Type {
		case git.LineAdded:
			info.newLine = dl.NewNum
		case git.LineRemoved:
			info.oldLine = dl.OldNum
		case git.LineContext:
			info.oldLine = dl.OldNum
			info.newLine = dl.NewNum
		}
		meta = append(meta, info)
		fns = append(fns, func(scrollH int) string {
			return tuicore.RenderDiffLinePublic(dl, lang, gw, w, scrollH)
		})
		j++
	}
	return lines, meta, fns
}

// renderInlineFeedback renders a feedback card inline in the diff.
func renderInlineFeedback(fb review.Feedback, userEmail string, showEmail bool) string {
	icon := "↩"
	switch fb.ReviewState {
	case review.ReviewStateApproved:
		icon = "✓"
	case review.ReviewStateChangesRequested:
		icon = "✗"
	}
	fbAuthorName := fb.Author.Name
	if showEmail && fb.Author.Email != "" {
		fbAuthorName += " <" + fb.Author.Email + ">"
	}
	authorStyle := tuicore.AuthorStyle(fb.Author.Email, userEmail, tuicore.Dim)
	styledAuthor := authorStyle.Render(fbAuthorName)
	header := fmt.Sprintf("  %s %s  %s", icon, styledAuthor, tuicore.Dim.Render(tuicore.FormatTime(fb.Timestamp)))
	if fb.Content == "" {
		return header
	}
	lines := []string{header}
	for _, line := range strings.Split(fb.Content, "\n") {
		lines = append(lines, "    "+line)
	}
	return strings.Join(lines, "\n")
}

// viewportHeight returns the number of diff lines visible in the viewport.
func (v *DiffView) viewportHeight() int {
	// 4 lines reserved: summary header (2) + footer area (2)
	vh := v.height - 4
	if vh < 1 {
		vh = 1
	}
	return vh
}

// ensureCursorVisible adjusts scroll so the cursor stays in the viewport.
func (v *DiffView) ensureCursorVisible() {
	vh := v.viewportHeight()
	if v.cursor < v.scroll {
		v.scroll = v.cursor
	}
	if v.cursor >= v.scroll+vh {
		v.scroll = v.cursor - vh + 1
	}
}

// updateSelectedFileFromCursor syncs selectedFile to whichever file the cursor is on.
func (v *DiffView) updateSelectedFileFromCursor() {
	if v.cursor >= 0 && v.cursor < len(v.lineMetadata) {
		v.selectedFile = v.lineMetadata[v.cursor].fileIdx
	}
}

// toggleFileAtCursor expands/collapses the file under the cursor.
func (v *DiffView) toggleFileAtCursor() {
	if v.cursor >= 0 && v.cursor < len(v.lineMetadata) {
		v.toggleFile(v.lineMetadata[v.cursor].fileIdx)
	}
}

// moveCursorToFile moves the cursor to a file header line.
func (v *DiffView) moveCursorToFile(idx int) {
	for i, meta := range v.lineMetadata {
		if meta.fileIdx == idx && meta.file == "" {
			v.cursor = i
			v.ensureCursorVisible()
			return
		}
	}
}

// renderVisible renders the visible slice of pre-rendered lines.
func (v *DiffView) renderVisible(_, _ int) string {
	if len(v.renderedLines) == 0 {
		return tuicore.Dim.Render("No changes")
	}
	summary := fmt.Sprintf("Files: %d changed  %s",
		v.stats.Files, tuicore.RenderDiffStatsBadge(v.stats.Added, v.stats.Removed))
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
	indicator := lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.IdentityFollowing)).Render("▌")
	var visible []string
	for i := v.scroll; i < end; i++ {
		var line string
		if lazyRender && i < len(v.lineRenderFn) && v.lineRenderFn[i] != nil {
			line = v.lineRenderFn[i](v.scrollH)
		} else {
			line = v.renderedLines[i]
		}
		if v.searchActive && v.searchQuery != "" {
			line = tuicore.HighlightInText(line, v.searchQuery)
		}
		if i == v.cursor {
			line = indicator + line
		} else {
			line = " " + line
		}
		line = tuicore.MarkZone(tuicore.ZoneID(v.zonePrefix, i), line)
		visible = append(visible, line)
	}
	return summary + "\n\n" + strings.Join(visible, "\n")
}

type diffLoadedMsg struct {
	pr          *review.PullRequest
	headCommit  string
	diffs       []git.FileDiff
	stats       git.DiffStats
	feedbacks   []review.Feedback
	forkFetched bool
	diffError   string
}
