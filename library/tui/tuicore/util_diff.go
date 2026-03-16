// util_diff.go - Diff rendering with syntax highlighting and ANSI colors
package tuicore

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"

	"github.com/gitsocial-org/gitsocial/core/git"
)

// DiffRenderOptions controls diff rendering behavior.
type DiffRenderOptions struct {
	Width        int
	ContextLines int // default 3
	Language     string
	CollapseAt   int // collapse context regions > N lines
	ScrollH      int // horizontal scroll offset in characters
}

var (
	diffHunkStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(DiffHunkHeader))
	diffLineNumStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(DiffLineNum))
	diffAddedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(DiffAdded))
	diffRemovedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(DiffRemoved))
)

// RenderFileDiff renders a complete file diff with header and all hunks.
func RenderFileDiff(diff git.FileDiff, opts DiffRenderOptions) string {
	var b strings.Builder
	b.WriteString(RenderDiffHeader(diff, true))
	b.WriteString("\n")
	lang := opts.Language
	if lang == "" {
		path := diff.NewPath
		if path == "" {
			path = diff.OldPath
		}
		lang = DetectLanguageFromPath(path)
	}
	if diff.Binary {
		b.WriteString(Dim.Render("  Binary file"))
		return b.String()
	}
	for i, hunk := range diff.Hunks {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(RenderHunk(hunk, lang, opts))
	}
	return b.String()
}

// RenderHunk renders a single diff hunk with line numbers and syntax highlighting.
func RenderHunk(hunk git.Hunk, lang string, opts DiffRenderOptions) string {
	var b strings.Builder
	b.WriteString(diffHunkStyle.Render(hunk.Header))
	b.WriteString("\n")
	gutterWidth := GutterSize(hunk)
	// Build partner map for word-level diff on adjacent removed/added pairs
	partners := buildPartnerMap(hunk.Lines)
	for i, line := range hunk.Lines {
		if partner, ok := partners[i]; ok {
			b.WriteString(renderDiffLineWithWordDiff(line, partner, lang, gutterWidth, opts.Width, opts.ScrollH))
		} else {
			b.WriteString(renderDiffLine(line, lang, gutterWidth, opts.Width, opts.ScrollH))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// RenderDiffHeader renders a file diff header with status icon and path.
func RenderDiffHeader(diff git.FileDiff, expanded bool) string {
	icon := "~"
	switch diff.Status {
	case git.DiffStatusAdded:
		icon = "+"
	case git.DiffStatusDeleted:
		icon = "-"
	case git.DiffStatusRenamed:
		icon = "→"
	}
	var added, removed int
	for _, hunk := range diff.Hunks {
		for _, line := range hunk.Lines {
			switch line.Type {
			case git.LineAdded:
				added++
			case git.LineRemoved:
				removed++
			}
		}
	}
	path := diff.NewPath
	if path == "" {
		path = diff.OldPath
	}
	var iconStyle lipgloss.Style
	switch diff.Status {
	case git.DiffStatusAdded:
		iconStyle = diffAddedStyle
	case git.DiffStatusDeleted:
		iconStyle = diffRemovedStyle
	default:
		iconStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(TextPrimary))
	}
	expandIcon := "▸"
	if expanded {
		expandIcon = "▾"
	}
	header := fmt.Sprintf("%s %s %s", expandIcon, iconStyle.Render(icon), path)
	if diff.Status == git.DiffStatusRenamed && diff.OldPath != diff.NewPath {
		header = fmt.Sprintf("%s %s %s → %s", expandIcon, iconStyle.Render(icon), diff.OldPath, diff.NewPath)
	}
	if added > 0 || removed > 0 {
		header += "  " + RenderDiffStatsBadge(added, removed)
	}
	return header
}

// RenderDiffStatsBadge renders a compact "+N -M" badge.
func RenderDiffStatsBadge(added, removed int) string {
	var parts []string
	if added > 0 {
		parts = append(parts, diffAddedStyle.Render(fmt.Sprintf("+%d", added)))
	}
	if removed > 0 {
		parts = append(parts, diffRemovedStyle.Render(fmt.Sprintf("-%d", removed)))
	}
	return strings.Join(parts, " ")
}

// DiffHunkHeaderStyle returns the style used for hunk headers.
func DiffHunkHeaderStyle() lipgloss.Style { return diffHunkStyle }

// RenderDiffLinePublic renders a single diff line with gutter and syntax highlighting.
func RenderDiffLinePublic(line git.DiffLine, lang string, gutterWidth, totalWidth, scrollH int) string {
	return renderDiffLine(line, lang, gutterWidth, totalWidth, scrollH)
}

// renderDiffLine renders a single diff line with gutter and syntax highlighting.
func renderDiffLine(line git.DiffLine, lang string, gutterWidth, totalWidth, scrollH int) string {
	// Build gutter
	var gutterText string
	switch line.Type {
	case git.LineContext:
		gutterText = fmt.Sprintf("%*d", gutterWidth, line.NewNum)
	case git.LineAdded:
		gutterText = fmt.Sprintf("%*d", gutterWidth, line.NewNum)
	case git.LineRemoved:
		gutterText = fmt.Sprintf("%*d", gutterWidth, line.OldNum)
	}
	gutter := diffLineNumStyle.Render(gutterText)
	gutterVisual := AnsiWidth(gutter)
	contentWidth := totalWidth - gutterVisual - 3 // space + marker + space
	if contentWidth < 0 {
		contentWidth = 0
	}
	// Expand tabs and highlight content; detect CR for display on changed lines
	hasCR := strings.ContainsRune(line.Content, '\r')
	content := strings.ReplaceAll(line.Content, "\r", "")
	content = strings.ReplaceAll(content, "\t", "    ")
	content = shiftLeft(content, scrollH)
	content = truncateRunes(content, contentWidth)
	highlighted := HighlightLine(content, lang, false)
	crSuffix := ""
	if hasCR {
		crSuffix = Dim.Render("^M")
	}
	// Apply marker and persistent background for added/removed lines
	switch line.Type {
	case git.LineAdded:
		marker := diffAddedStyle.Render("+")
		fullLine := padToWidth(gutter+" "+marker+" "+stripAnsiBg(highlighted)+crSuffix, totalWidth)
		return withPersistentBg(fullLine, DiffAddedBg)
	case git.LineRemoved:
		marker := diffRemovedStyle.Render("-")
		fullLine := padToWidth(gutter+" "+marker+" "+stripAnsiBg(highlighted)+crSuffix, totalWidth)
		return withPersistentBg(fullLine, DiffRemovedBg)
	default:
		return gutter + "   " + highlighted + crSuffix
	}
}

// GutterSize computes the width needed for line numbers in a hunk.
func GutterSize(hunk git.Hunk) int {
	maxOld := hunk.OldStart + hunk.OldCount
	maxNew := hunk.NewStart + hunk.NewCount
	max := maxOld
	if maxNew > max {
		max = maxNew
	}
	width := 1
	for n := max; n >= 10; n /= 10 {
		width++
	}
	return width
}

// truncateRunes hard-truncates a string to maxWidth characters without adding ellipsis.
func truncateRunes(s string, maxWidth int) string {
	runes := []rune(s)
	if len(runes) <= maxWidth {
		return s
	}
	return string(runes[:maxWidth])
}

// shiftLeft drops the first n characters from a string for horizontal scrolling.
func shiftLeft(s string, n int) string {
	if n <= 0 {
		return s
	}
	runes := []rune(s)
	if n >= len(runes) {
		return ""
	}
	return string(runes[n:])
}

// padToWidth pads a string with spaces to reach the target width.
func padToWidth(s string, width int) string {
	current := AnsiWidth(s)
	if current >= width {
		return s
	}
	return s + strings.Repeat(" ", width-current)
}

// withPersistentBg applies a background color that survives ANSI resets.
func withPersistentBg(s string, bgHex string) string {
	var r, g, b int
	_, _ = fmt.Sscanf(bgHex, "#%02x%02x%02x", &r, &g, &b)
	bgSeq := fmt.Sprintf("\033[48;2;%d;%d;%dm", r, g, b)
	result := strings.ReplaceAll(s, "\033[0m", "\033[0m"+bgSeq)
	return bgSeq + result + "\033[0m"
}

// stripAnsiBg removes ANSI background color sequences from styled text,
// preserving foreground colors and other attributes.
func stripAnsiBg(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if i+1 < len(s) && s[i] == '\033' && s[i+1] == '[' {
			end := i + 2
			for end < len(s) && s[end] != 'm' {
				end++
			}
			if end < len(s) {
				filtered := filterBgParams(s[i+2 : end])
				if filtered != "" {
					b.WriteString("\033[" + filtered + "m")
				}
				i = end + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// filterBgParams removes background-related SGR parameters from a param string.
func filterBgParams(params string) string {
	if params == "" || params == "0" {
		return params
	}
	parts := strings.Split(params, ";")
	var kept []string
	for i := 0; i < len(parts); {
		n, _ := strconv.Atoi(parts[i])
		switch {
		case (n >= 40 && n <= 47) || n == 49 || (n >= 100 && n <= 107):
			i++
		case n == 48 && i+1 < len(parts):
			sub, _ := strconv.Atoi(parts[i+1])
			switch sub {
			case 5:
				i += 3 // 48;5;N
			case 2:
				i += 5 // 48;2;R;G;B
			default:
				i += 2
			}
		default:
			kept = append(kept, parts[i])
			i++
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, ";")
}

// SplitPair holds a left/right pair of diff lines for side-by-side rendering.
type SplitPair struct {
	Left  *git.DiffLine
	Right *git.DiffLine
}

// PairHunkLines pairs diff lines for side-by-side display.
func PairHunkLines(lines []git.DiffLine) []SplitPair {
	var pairs []SplitPair
	i := 0
	for i < len(lines) {
		line := lines[i]
		if line.Type == git.LineContext {
			pairs = append(pairs, SplitPair{Left: &lines[i], Right: &lines[i]})
			i++
			continue
		}
		// Collect contiguous removed block
		var removed []int
		for i < len(lines) && lines[i].Type == git.LineRemoved {
			removed = append(removed, i)
			i++
		}
		// Collect contiguous added block
		var added []int
		for i < len(lines) && lines[i].Type == git.LineAdded {
			added = append(added, i)
			i++
		}
		// Pair removed/added 1:1, excess gets nil on the other side
		maxLen := len(removed)
		if len(added) > maxLen {
			maxLen = len(added)
		}
		for j := 0; j < maxLen; j++ {
			p := SplitPair{}
			if j < len(removed) {
				p.Left = &lines[removed[j]]
			}
			if j < len(added) {
				p.Right = &lines[added[j]]
			}
			pairs = append(pairs, p)
		}
	}
	return pairs
}

// RenderHunkSplit renders a hunk in side-by-side (split) mode.
func RenderHunkSplit(hunk git.Hunk, lang string, opts DiffRenderOptions) string {
	var b strings.Builder
	b.WriteString(diffHunkStyle.Render(hunk.Header))
	b.WriteString("\n")
	pairs := PairHunkLines(hunk.Lines)
	gw := GutterSize(hunk)
	sideWidth := (opts.Width - 1) / 2
	if sideWidth < 1 {
		sideWidth = 1
	}
	for _, p := range pairs {
		var leftSegs, rightSegs []DiffSegment
		if p.Left != nil && p.Right != nil && p.Left.Type == git.LineRemoved && p.Right.Type == git.LineAdded {
			leftSegs, rightSegs = ComputeWordDiff(p.Left.Content, p.Right.Content)
		}
		left := renderSplitSideWithWordDiff(p.Left, true, lang, gw, sideWidth, opts.ScrollH, leftSegs)
		right := renderSplitSideWithWordDiff(p.Right, false, lang, gw, sideWidth, opts.ScrollH, rightSegs)
		b.WriteString(left + "│" + right)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderSplitSideWithWordDiff renders one side with optional word-level highlighting.
func renderSplitSideWithWordDiff(line *git.DiffLine, isLeft bool, lang string, gutterWidth, sideWidth, scrollH int, segments []DiffSegment) string {
	if line == nil {
		return strings.Repeat(" ", sideWidth)
	}
	if len(segments) == 0 || len([]rune(line.Content)) > 500 {
		return renderSplitSide(line, isLeft, lang, gutterWidth, sideWidth, scrollH)
	}
	var num int
	if isLeft {
		num = line.OldNum
	} else {
		num = line.NewNum
	}
	numStr := fmt.Sprintf("%*d", gutterWidth, num)
	if (isLeft && line.Type == git.LineAdded) || (!isLeft && line.Type == git.LineRemoved) {
		numStr = strings.Repeat(" ", gutterWidth)
	}
	gutter := diffLineNumStyle.Render(numStr)
	gutterVisual := AnsiWidth(gutter)
	contentWidth := sideWidth - gutterVisual - 3
	if contentWidth < 0 {
		contentWidth = 0
	}
	content := strings.ReplaceAll(line.Content, "\r", "")
	content = strings.ReplaceAll(content, "\t", "    ")
	content = shiftLeft(content, scrollH)
	content = truncateRunes(content, contentWidth)
	highlighted := renderWithWordHighlight(content, segments, line.Type, lang, scrollH)
	padded := padToWidth(highlighted, contentWidth)
	switch line.Type {
	case git.LineAdded:
		marker := diffAddedStyle.Render("+")
		fullSide := padToWidth(gutter+" "+marker+" "+stripAnsiBg(padded), sideWidth)
		return withPersistentBg(fullSide, DiffAddedBg)
	case git.LineRemoved:
		marker := diffRemovedStyle.Render("-")
		fullSide := padToWidth(gutter+" "+marker+" "+stripAnsiBg(padded), sideWidth)
		return withPersistentBg(fullSide, DiffRemovedBg)
	default:
		return gutter + "   " + padded
	}
}

// renderSplitSide renders one side of a split diff pair.
func renderSplitSide(line *git.DiffLine, isLeft bool, lang string, gutterWidth, sideWidth, scrollH int) string {
	if line == nil {
		return strings.Repeat(" ", sideWidth)
	}
	// Build gutter text: single line number
	var num int
	if isLeft {
		num = line.OldNum
	} else {
		num = line.NewNum
	}
	numStr := fmt.Sprintf("%*d", gutterWidth, num)
	if (isLeft && line.Type == git.LineAdded) || (!isLeft && line.Type == git.LineRemoved) {
		numStr = strings.Repeat(" ", gutterWidth)
	}
	gutter := diffLineNumStyle.Render(numStr)
	gutterVisual := AnsiWidth(gutter)
	contentWidth := sideWidth - gutterVisual - 3 // space + marker + space
	if contentWidth < 0 {
		contentWidth = 0
	}
	// Expand tabs and truncate raw content before highlighting; detect CR for changed lines
	hasCR := strings.ContainsRune(line.Content, '\r')
	content := strings.ReplaceAll(line.Content, "\r", "")
	content = strings.ReplaceAll(content, "\t", "    ")
	content = shiftLeft(content, scrollH)
	content = truncateRunes(content, contentWidth)
	highlighted := HighlightLine(content, lang, false)
	padded := padToWidth(highlighted, contentWidth)
	crSuffix := ""
	if hasCR {
		crSuffix = Dim.Render("^M")
	}
	// Apply marker and persistent background for added/removed lines
	switch line.Type {
	case git.LineAdded:
		marker := diffAddedStyle.Render("+")
		fullSide := padToWidth(gutter+" "+marker+" "+stripAnsiBg(padded)+crSuffix, sideWidth)
		return withPersistentBg(fullSide, DiffAddedBg)
	case git.LineRemoved:
		marker := diffRemovedStyle.Render("-")
		fullSide := padToWidth(gutter+" "+marker+" "+stripAnsiBg(padded)+crSuffix, sideWidth)
		return withPersistentBg(fullSide, DiffRemovedBg)
	default:
		return gutter + "   " + padded + crSuffix
	}
}

// DiffSegment represents a segment of text that may or may not have changed.
type DiffSegment struct {
	Text    string
	Changed bool
}

// ComputeWordDiff finds changed segments between two lines using token-level LCS.
func ComputeWordDiff(oldContent, newContent string) (oldSegments, newSegments []DiffSegment) {
	oldTokens := tokenizeWords(oldContent)
	newTokens := tokenizeWords(newContent)
	if len(oldTokens) > 50 || len(newTokens) > 50 {
		return []DiffSegment{{Text: oldContent, Changed: true}}, []DiffSegment{{Text: newContent, Changed: true}}
	}
	lcs := computeLCS(oldTokens, newTokens)
	oldSegments = buildSegments(oldTokens, lcs)
	newSegments = buildSegments(newTokens, lcs)
	return
}

// tokenizeWords splits text on word boundaries (transitions between alphanumeric and non-alphanumeric).
func tokenizeWords(s string) []string {
	var tokens []string
	runes := []rune(s)
	if len(runes) == 0 {
		return nil
	}
	start := 0
	prevAlnum := unicode.IsLetter(runes[0]) || unicode.IsDigit(runes[0])
	for i := 1; i < len(runes); i++ {
		curAlnum := unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i])
		if curAlnum != prevAlnum {
			tokens = append(tokens, string(runes[start:i]))
			start = i
			prevAlnum = curAlnum
		}
	}
	tokens = append(tokens, string(runes[start:]))
	return tokens
}

// computeLCS returns the longest common subsequence of two token slices.
func computeLCS(a, b []string) []string {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	// Backtrack
	result := make([]string, dp[m][n])
	k := dp[m][n] - 1
	i, j := m, n
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			result[k] = a[i-1]
			k--
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			i--
		} else {
			j--
		}
	}
	return result
}

// buildSegments builds diff segments by comparing tokens against the LCS.
func buildSegments(tokens, lcs []string) []DiffSegment {
	var segments []DiffSegment
	li := 0
	for _, tok := range tokens {
		if li < len(lcs) && tok == lcs[li] {
			segments = append(segments, DiffSegment{Text: tok, Changed: false})
			li++
		} else {
			segments = append(segments, DiffSegment{Text: tok, Changed: true})
		}
	}
	return segments
}

// buildPartnerMap identifies adjacent removed/added pairs in hunk lines for word diff.
func buildPartnerMap(lines []git.DiffLine) map[int]*git.DiffLine {
	partners := make(map[int]*git.DiffLine)
	i := 0
	for i < len(lines) {
		if lines[i].Type != git.LineRemoved {
			i++
			continue
		}
		var removed []int
		for i < len(lines) && lines[i].Type == git.LineRemoved {
			removed = append(removed, i)
			i++
		}
		var added []int
		for i < len(lines) && lines[i].Type == git.LineAdded {
			added = append(added, i)
			i++
		}
		// Pair 1:1
		pairCount := len(removed)
		if len(added) < pairCount {
			pairCount = len(added)
		}
		for j := 0; j < pairCount; j++ {
			ri, ai := removed[j], added[j]
			if len([]rune(lines[ri].Content)) <= 500 && len([]rune(lines[ai].Content)) <= 500 {
				partners[ri] = &lines[ai]
				partners[ai] = &lines[ri]
			}
		}
	}
	return partners
}

// renderDiffLineWithWordDiff renders a diff line with word-level change highlighting.
func renderDiffLineWithWordDiff(line git.DiffLine, partner *git.DiffLine, lang string, gutterWidth, totalWidth, scrollH int) string {
	var gutterText string
	switch line.Type {
	case git.LineContext:
		gutterText = fmt.Sprintf("%*d", gutterWidth, line.NewNum)
	case git.LineAdded:
		gutterText = fmt.Sprintf("%*d", gutterWidth, line.NewNum)
	case git.LineRemoved:
		gutterText = fmt.Sprintf("%*d", gutterWidth, line.OldNum)
	}
	gutter := diffLineNumStyle.Render(gutterText)
	gutterVisual := AnsiWidth(gutter)
	contentWidth := totalWidth - gutterVisual - 3
	if contentWidth < 0 {
		contentWidth = 0
	}
	content := strings.ReplaceAll(line.Content, "\r", "")
	content = strings.ReplaceAll(content, "\t", "    ")
	content = shiftLeft(content, scrollH)
	content = truncateRunes(content, contentWidth)
	var segments []DiffSegment
	if line.Type == git.LineRemoved {
		segments, _ = ComputeWordDiff(line.Content, partner.Content)
	} else {
		_, segments = ComputeWordDiff(partner.Content, line.Content)
	}
	highlighted := renderWithWordHighlight(content, segments, line.Type, lang, scrollH)
	switch line.Type {
	case git.LineAdded:
		marker := diffAddedStyle.Render("+")
		fullLine := padToWidth(gutter+" "+marker+" "+stripAnsiBg(highlighted), totalWidth)
		return withPersistentBg(fullLine, DiffAddedBg)
	case git.LineRemoved:
		marker := diffRemovedStyle.Render("-")
		fullLine := padToWidth(gutter+" "+marker+" "+stripAnsiBg(highlighted), totalWidth)
		return withPersistentBg(fullLine, DiffRemovedBg)
	default:
		return gutter + "   " + highlighted
	}
}

// renderWithWordHighlight applies word-level highlighting to content based on diff segments.
func renderWithWordHighlight(content string, segments []DiffSegment, lineType git.LineType, lang string, scrollH int) string {
	if len(segments) == 0 {
		return HighlightLine(content, lang, false)
	}
	// Reconstruct the original content from segments, expanded
	var fullContent strings.Builder
	for _, seg := range segments {
		fullContent.WriteString(seg.Text)
	}
	expanded := strings.ReplaceAll(fullContent.String(), "\r", "")
	expanded = strings.ReplaceAll(expanded, "\t", "    ")
	// Map character positions in expanded content to changed/unchanged
	expandedRunes := []rune(expanded)
	changedMap := make([]bool, len(expandedRunes))
	pos := 0
	for _, seg := range segments {
		segExpanded := strings.ReplaceAll(seg.Text, "\r", "")
		segExpanded = strings.ReplaceAll(segExpanded, "\t", "    ")
		segRunes := []rune(segExpanded)
		for range segRunes {
			if pos < len(changedMap) {
				changedMap[pos] = seg.Changed
			}
			pos++
		}
	}
	// Apply scrollH offset to changedMap
	if scrollH > 0 && scrollH < len(changedMap) {
		changedMap = changedMap[scrollH:]
	} else if scrollH >= len(changedMap) {
		changedMap = nil
	}
	// Truncate to content length
	contentRunes := []rune(content)
	if len(changedMap) > len(contentRunes) {
		changedMap = changedMap[:len(contentRunes)]
	}
	// Build output with word-level background highlights
	var wordBg string
	if lineType == git.LineAdded {
		wordBg = DiffWordAddedBg
	} else {
		wordBg = DiffWordRemovedBg
	}
	wordBgStyle := lipgloss.NewStyle().Background(lipgloss.Color(wordBg))
	var b strings.Builder
	i := 0
	for i < len(contentRunes) {
		isChanged := i < len(changedMap) && changedMap[i]
		j := i + 1
		for j < len(contentRunes) {
			nextChanged := j < len(changedMap) && changedMap[j]
			if nextChanged != isChanged {
				break
			}
			j++
		}
		chunk := string(contentRunes[i:j])
		highlighted := HighlightLine(chunk, lang, false)
		if isChanged {
			b.WriteString(wordBgStyle.Render(stripAnsiBg(highlighted)))
		} else {
			b.WriteString(highlighted)
		}
		i = j
	}
	return b.String()
}
