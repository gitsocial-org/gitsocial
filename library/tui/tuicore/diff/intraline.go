// intraline.go - Word/char-level highlighting within paired removed/added lines.
package diff

import "strings"

// intraSpan describes a rune-index range [Start, End) in a line's content
// (i.e., after the gutter/sign prefix is skipped).
type intraSpan struct {
	Start, End int
}

// bodyPrefixCells is the count of fixed prefix cells on a body row:
// gutterCell + " " + signCell + " ". intra-line spans index into rune
// positions starting after these cells.
const bodyPrefixCells = 4

// applyIntraLineHighlights post-processes plan rows to add brighter BG cells
// on the differing portions of paired RowRemoved/RowAdded lines within hunks.
// Unified-mode pairs are consecutive removed[] + added[] runs sharing a hunk
// anchor; split-mode pairs come from a single DisplayRow with both halves.
func applyIntraLineHighlights(plan *DisplayPlan, palette Palette) {
	if palette.IntraLineAddedBG == "" || palette.IntraLineRemovedBG == "" {
		return
	}
	// Unified-mode pairs: consecutive RowRemoved...RowAdded runs.
	for i := 0; i < len(plan.Rows); {
		if plan.Rows[i].IsSplitBody() || plan.Rows[i].Kind != RowRemoved {
			i++
			continue
		}
		removeStart := i
		anchor := plan.Rows[removeStart].Anchor
		for i < len(plan.Rows) && plan.Rows[i].Kind == RowRemoved && !plan.Rows[i].IsSplitBody() &&
			sameHunk(plan.Rows[i].Anchor, anchor) {
			i++
		}
		removeEnd := i
		addedStart := i
		for i < len(plan.Rows) && plan.Rows[i].Kind == RowAdded && !plan.Rows[i].IsSplitBody() &&
			sameHunk(plan.Rows[i].Anchor, anchor) {
			i++
		}
		addedEnd := i
		nR := removeEnd - removeStart
		nA := addedEnd - addedStart
		pairs := nR
		if nA < pairs {
			pairs = nA
		}
		for k := 0; k < pairs; k++ {
			highlightPair(&plan.Rows[removeStart+k].Row, &plan.Rows[addedStart+k].Row, palette)
		}
	}
	// Split-mode pairs: each split-body row owns its Left/Right halves.
	for i := range plan.Rows {
		dr := &plan.Rows[i]
		if !dr.IsSplitBody() || dr.Left == nil || dr.Right == nil {
			continue
		}
		highlightPair(dr.Left, dr.Right, palette)
	}
}

// highlightPair computes the intra-line diff between two body rows and rewrites
// their cells with brighter BGs on the differing spans. No-op if the lines are
// identical or differ too much to highlight usefully.
func highlightPair(removed, added *Row, palette Palette) {
	oldText := contentText(removed.Cells)
	newText := contentText(added.Cells)
	if oldText == newText {
		return
	}
	oldSpans, newSpans := intraLineDiff(oldText, newText, 3)
	if shouldSkipIntraLine(oldText, newText, oldSpans, newSpans) {
		return
	}
	removed.Cells = applyIntraLineToCells(removed.Cells, bodyPrefixCells, oldSpans, palette.IntraLineRemovedBG)
	added.Cells = applyIntraLineToCells(added.Cells, bodyPrefixCells, newSpans, palette.IntraLineAddedBG)
}

func sameHunk(a, b RowAnchor) bool {
	return a.FileIdx == b.FileIdx && a.HunkIdx == b.HunkIdx
}

// contentText concatenates the text of content cells (skipping the prefix).
func contentText(cells []Cell) string {
	var b strings.Builder
	for i := bodyPrefixCells; i < len(cells); i++ {
		b.WriteString(cells[i].Text)
	}
	return b.String()
}

// shouldSkipIntraLine returns true when intra-line highlights would dominate
// the line (most of it differs), in which case the standard add/remove BG is
// enough and intra-line just adds noise. Threshold: >80% of either side.
func shouldSkipIntraLine(oldText, newText string, oldSpans, newSpans []intraSpan) bool {
	oldLen := len([]rune(oldText))
	newLen := len([]rune(newText))
	oldDiff := spanLength(oldSpans)
	newDiff := spanLength(newSpans)
	if oldLen > 0 && oldDiff*5 >= oldLen*4 {
		return true
	}
	if newLen > 0 && newDiff*5 >= newLen*4 {
		return true
	}
	return false
}

func spanLength(spans []intraSpan) int {
	n := 0
	for _, s := range spans {
		n += s.End - s.Start
	}
	return n
}

// intraLineDiff returns differing rune-index spans on both sides via char-level
// LCS. Adjacent spans separated by <= mergeGap chars are merged so visually-
// related edits don't fragment into many short highlights (e.g., a space between
// two changed words stays inside the highlight).
func intraLineDiff(removed, added string, mergeGap int) (oldSpans, newSpans []intraSpan) {
	o := []rune(removed)
	n := []rune(added)
	if len(o) == 0 || len(n) == 0 {
		if len(o) > 0 {
			oldSpans = []intraSpan{{0, len(o)}}
		}
		if len(n) > 0 {
			newSpans = []intraSpan{{0, len(n)}}
		}
		return
	}
	m, k := len(o), len(n)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, k+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= k; j++ {
			if o[i-1] == n[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	// Backtrack into forward edit script.
	i, j := m, k
	type op struct {
		kind       byte // 'c'=context, '-'=removed, '+'=added
		oIdx, nIdx int
	}
	var rev []op
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && o[i-1] == n[j-1]:
			rev = append(rev, op{kind: 'c', oIdx: i - 1, nIdx: j - 1})
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]):
			rev = append(rev, op{kind: '+', nIdx: j - 1})
			j--
		default:
			rev = append(rev, op{kind: '-', oIdx: i - 1})
			i--
		}
	}
	// Walk forward, building per-side span runs.
	oldStart := -1
	newStart := -1
	flushOld := func(end int) {
		if oldStart >= 0 {
			oldSpans = append(oldSpans, intraSpan{oldStart, end})
			oldStart = -1
		}
	}
	flushNew := func(end int) {
		if newStart >= 0 {
			newSpans = append(newSpans, intraSpan{newStart, end})
			newStart = -1
		}
	}
	for x := len(rev) - 1; x >= 0; x-- {
		o := rev[x]
		switch o.kind {
		case 'c':
			flushOld(o.oIdx)
			flushNew(o.nIdx)
		case '-':
			if oldStart < 0 {
				oldStart = o.oIdx
			}
		case '+':
			if newStart < 0 {
				newStart = o.nIdx
			}
		}
	}
	flushOld(m)
	flushNew(k)
	if mergeGap > 0 {
		oldSpans = mergeNearbySpans(oldSpans, mergeGap)
		newSpans = mergeNearbySpans(newSpans, mergeGap)
	}
	return
}

// mergeNearbySpans coalesces spans separated by at most `gap` chars.
func mergeNearbySpans(spans []intraSpan, gap int) []intraSpan {
	if len(spans) <= 1 {
		return spans
	}
	out := []intraSpan{spans[0]}
	for _, s := range spans[1:] {
		last := &out[len(out)-1]
		if s.Start-last.End <= gap {
			last.End = s.End
		} else {
			out = append(out, s)
		}
	}
	return out
}

// applyIntraLineToCells walks content cells (after `prefixCells`), splitting
// each cell where it crosses a span boundary and stamping `bg` onto the
// segments that fall inside a span. Cell FG/attrs are preserved.
func applyIntraLineToCells(cells []Cell, prefixCells int, spans []intraSpan, bg string) []Cell {
	if len(spans) == 0 || prefixCells > len(cells) {
		return cells
	}
	out := make([]Cell, 0, len(cells)+len(spans)*2)
	out = append(out, cells[:prefixCells]...)
	runeOffset := 0
	for _, cell := range cells[prefixCells:] {
		if cell.Text == "" {
			out = append(out, cell)
			continue
		}
		runes := []rune(cell.Text)
		cellLen := len(runes)
		segStart := 0
		segHighlighted := isInSpan(runeOffset, spans)
		for i := 1; i < cellLen; i++ {
			cur := isInSpan(runeOffset+i, spans)
			if cur != segHighlighted {
				seg := cell
				seg.Text = string(runes[segStart:i])
				if segHighlighted {
					seg.BG = bg
				}
				out = append(out, seg)
				segStart = i
				segHighlighted = cur
			}
		}
		seg := cell
		seg.Text = string(runes[segStart:cellLen])
		if segHighlighted {
			seg.BG = bg
		}
		out = append(out, seg)
		runeOffset += cellLen
	}
	return out
}

// isInSpan reports whether rune index `idx` falls inside any span. Spans are
// assumed sorted by Start.
func isInSpan(idx int, spans []intraSpan) bool {
	for _, s := range spans {
		if idx < s.Start {
			return false
		}
		if idx < s.End {
			return true
		}
	}
	return false
}
