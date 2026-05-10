// transform.go - Pure row→row(s) transformations: slice + wrap. The
// build pipeline composes its own split rows inline (see plan.go).
package diff

import (
	"fmt"
)

// SliceRow returns a row clipped to the column window [scrollH, scrollH+cols).
// Cells are subdivided as needed; cells fully outside the window are dropped.
// Wide characters are not split mid-grapheme — if a wide char straddles a
// boundary it's dropped on the side it would be cut on.
//
// Use cols <= 0 to disable the right clip (open-ended; returns content from
// scrollH to end-of-row).
func SliceRow(row Row, scrollH, cols int) Row {
	if scrollH <= 0 && cols <= 0 {
		return row
	}
	out := Row{Kind: row.Kind, LineBG: row.LineBG, Anchor: row.Anchor}
	col := 0
	end := scrollH + cols
	for _, c := range row.Cells {
		w := stringWidth(c.Text)
		cellStart := col
		cellEnd := col + w
		col = cellEnd
		if cellEnd <= scrollH {
			continue
		}
		if cols > 0 && cellStart >= end {
			break
		}
		take := c
		if cellStart < scrollH || (cols > 0 && cellEnd > end) {
			lo := scrollH - cellStart
			if lo < 0 {
				lo = 0
			}
			hi := w
			if cols > 0 && cellEnd > end {
				hi = end - cellStart
			}
			if hi > w {
				hi = w
			}
			take.Text = sliceText(c.Text, lo, hi)
		}
		if take.Text != "" {
			out.Cells = append(out.Cells, take)
		}
	}
	return out
}

// WrapRow splits a row into multiple rows, each at most `cols` display
// columns wide. Cells are subdivided at column boundaries; LineBG and Anchor
// are inherited (continuation rows get Anchor.Tag annotated with a
// wrap-cont marker so they stay distinguishable from the source row).
//
// `indent` (>=0) applies a hanging indent: continuation rows start with a
// blank cell of that width (using the row's LineBG) and their content area
// shrinks to cols-indent. The first row still uses the full cols. Use 0
// for no indent.
//
// cols <= 0 returns the input row unchanged.
func WrapRow(row Row, cols, indent int) []Row {
	if cols <= 0 || row.Width() <= cols {
		return []Row{row}
	}
	if indent < 0 || indent >= cols {
		indent = 0
	}
	var out []Row
	current := Row{Kind: row.Kind, LineBG: row.LineBG, Anchor: row.Anchor}
	used := 0
	targetCols := func() int {
		if len(out) == 0 {
			return cols
		}
		return cols - indent
	}
	flush := func() {
		idx := len(out)
		if idx > 0 {
			a := current.Anchor
			a.Tag = appendWrapTag(a.Tag, idx)
			current.Anchor = a
			if indent > 0 {
				current.Cells = append([]Cell{{Text: spaces(indent), BG: row.LineBG}}, current.Cells...)
			}
		}
		out = append(out, current)
		current = Row{Kind: row.Kind, LineBG: row.LineBG, Anchor: row.Anchor}
		used = 0
	}
	for _, c := range row.Cells {
		text := c.Text
		for text != "" {
			tc := targetCols()
			room := tc - used
			if room <= 0 {
				flush()
				tc = targetCols()
				room = tc
			}
			w := stringWidth(text)
			if w <= room {
				current.Cells = append(current.Cells, Cell{
					Text: text, FG: c.FG, BG: c.BG,
					Bold: c.Bold, Dim: c.Dim, Italic: c.Italic, Underline: c.Underline,
				})
				used += w
				break
			}
			head := sliceText(text, 0, room)
			tail := sliceText(text, room, w)
			current.Cells = append(current.Cells, Cell{
				Text: head, FG: c.FG, BG: c.BG,
				Bold: c.Bold, Dim: c.Dim, Italic: c.Italic, Underline: c.Underline,
			})
			used += stringWidth(head)
			text = tail
			if used >= tc {
				flush()
			}
		}
	}
	if used > 0 || len(out) == 0 {
		flush()
	}
	return out
}

// BodyPrefixWidth returns the indent width to use for hanging-indent wraps
// on a body row (RowAdded/RowRemoved/RowContext). Returns 0 for non-body
// rows so headers and placeholders keep their natural left edge.
func BodyPrefixWidth(row Row) int {
	switch row.Kind {
	case RowAdded, RowRemoved, RowContext:
	default:
		return 0
	}
	// The body-row prefix (gutter + " " + sign + " ") is the first four cells.
	w := 0
	n := 4
	if len(row.Cells) < n {
		n = len(row.Cells)
	}
	for i := 0; i < n; i++ {
		w += stringWidth(row.Cells[i].Text)
	}
	return w
}

// appendWrapTag adds a "wrap-cont:N" suffix to an anchor tag.
func appendWrapTag(existing string, idx int) string {
	if existing == "" {
		return fmt.Sprintf("wrap-cont:%d", idx)
	}
	return existing + ";" + fmt.Sprintf("wrap-cont:%d", idx)
}

// sliceText returns the substring of s spanning display columns [lo, hi).
// Walks runes; wide characters that would straddle the cut are omitted on
// the cut side. lo/hi are clamped to [0, Width(s)].
func sliceText(s string, lo, hi int) string {
	if lo <= 0 && hi >= stringWidth(s) {
		return s
	}
	if hi <= lo {
		return ""
	}
	var b []rune
	col := 0
	for _, r := range s {
		w := stringWidth(string(r))
		nextCol := col + w
		if nextCol <= lo {
			col = nextCol
			continue
		}
		if col >= hi {
			break
		}
		if col < lo || nextCol > hi {
			col = nextCol
			continue
		}
		b = append(b, r)
		col = nextCol
	}
	return string(b)
}

// spaces returns a string of n spaces; n<=0 returns "".
func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = ' '
	}
	return string(out)
}
