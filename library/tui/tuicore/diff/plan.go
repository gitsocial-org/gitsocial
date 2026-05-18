// plan.go - BuildPlan transforms (LogicalDiff, ViewState) into a DisplayPlan
// of fully-styled cell rows ready for Render. Pure function.
package diff

import (
	"fmt"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// DisplayRow is a cell-model Row, optionally with split-mode halves.
// Left/Right are populated only for split-mode body rows: each half is
// a complete Row (with its own LineBG and cells) that the renderer pads
// to half the viewport width and joins with a separator. The outer Row
// carries the anchor + kind for cursor / fold purposes.
type DisplayRow struct {
	Row
	Left  *Row
	Right *Row
}

// IsSplitBody reports whether this row should render as a paired
// left/separator/right composition rather than as a single Row.
func (d DisplayRow) IsSplitBody() bool { return d.Left != nil || d.Right != nil }

// DisplayPlan is the linear sequence of visible rows derived from a
// LogicalDiff under a particular ViewState. Folded rows are dropped;
// fold regions become single placeholder rows; layout (split) pairs
// adjacent removed/added rows. The Logical back-pointer lets Layers
// read file metadata (paths, hunks) without re-parsing cell text.
type DisplayPlan struct {
	Rows    []DisplayRow
	Logical LogicalDiff
}

// BuildPlan is the pure transform: same (ld, state, palette, highlight)
// always yields the same DisplayPlan. Layer.Decorate may add or modify
// rows afterward.
func BuildPlan(
	ld LogicalDiff,
	state ViewState,
	palette Palette,
	highlight Highlight,
) DisplayPlan {
	plan := DisplayPlan{Rows: make([]DisplayRow, 0, len(ld.Rows)), Logical: ld}
	// Per-file gutter widths so line numbers don't wobble across the
	// boundaries of long files. Computed once up front.
	gutters := make(map[int]int, len(ld.Files))
	for i, f := range ld.Files {
		gutters[i] = fileGutterWidth(f)
	}
	switch state.Layout {
	case LayoutUnified:
		buildUnified(ld, state, palette, highlight, gutters, &plan)
	case LayoutSplit, LayoutFullscreen:
		buildSplit(ld, state, palette, highlight, gutters, &plan)
	default:
		buildUnified(ld, state, palette, highlight, gutters, &plan)
	}
	applyIntraLineHighlights(&plan, palette)
	return plan
}

// advanceFolds emits a fold placeholder or skips a hidden row at index i.
// Returns (nextI, true) when the caller should `continue`; otherwise
// (i, false) and the row at i is visible.
func advanceFolds(ld LogicalDiff, state ViewState, palette Palette, plan *DisplayPlan, i int) (int, bool) {
	if foldIdx := state.IsPlaceholderRow(ld, i); foldIdx >= 0 {
		plan.Rows = append(plan.Rows, foldPlaceholderRow(ld, state.Folds[foldIdx], i, palette))
		if endIdx := ld.EffectiveEnd(state.Folds[foldIdx].End); endIdx > i+1 {
			return endIdx, true
		}
		return i + 1, true
	}
	if hidden, _ := state.IsHidden(ld, i); hidden {
		return i + 1, true
	}
	return i, false
}

// buildSplit emits split-mode rows. Non-body rows render full-width.
// Context rows duplicate to both halves. Runs of consecutive removed +
// added rows in the same hunk pair up: removed[k] with added[k], with
// orphan rows getting an empty other half. Each pair is one DisplayRow.
func buildSplit(
	ld LogicalDiff,
	state ViewState,
	palette Palette,
	highlight Highlight,
	gutters map[int]int,
	plan *DisplayPlan,
) {
	i := 0
	for i < len(ld.Rows) {
		if next, skip := advanceFolds(ld, state, palette, plan, i); skip {
			i = next
			continue
		}
		r := ld.Rows[i]
		if r.Kind == RowFileHeader || r.Kind == RowHunkHeader || r.Kind == RowBinary {
			plan.Rows = append(plan.Rows, buildLogicalDisplayRow(r, gutters[r.FileIdx], ld, palette, highlight))
			i++
			continue
		}
		if r.Kind == RowContext {
			body := buildLogicalDisplayRow(r, gutters[r.FileIdx], ld, palette, highlight).Row
			plan.Rows = append(plan.Rows, splitPairRow(&body, &body, r))
			i++
			continue
		}
		removeStart, removeEnd := i, i
		for removeEnd < len(ld.Rows) && sameHunkBody(ld.Rows[removeEnd], r) && ld.Rows[removeEnd].Kind == RowRemoved {
			removeEnd++
		}
		addedStart, addedEnd := removeEnd, removeEnd
		for addedEnd < len(ld.Rows) && sameHunkBody(ld.Rows[addedEnd], r) && ld.Rows[addedEnd].Kind == RowAdded {
			addedEnd++
		}
		nR := removeEnd - removeStart
		nA := addedEnd - addedStart
		pairs := nR
		if nA > pairs {
			pairs = nA
		}
		for k := 0; k < pairs; k++ {
			var left, right *Row
			anchorSrc := r
			if k < nR {
				lr := ld.Rows[removeStart+k]
				row := buildLogicalDisplayRow(lr, gutters[lr.FileIdx], ld, palette, highlight).Row
				left = &row
				anchorSrc = lr
			}
			if k < nA {
				ar := ld.Rows[addedStart+k]
				row := buildLogicalDisplayRow(ar, gutters[ar.FileIdx], ld, palette, highlight).Row
				right = &row
				if k >= nR {
					anchorSrc = ar
				}
			}
			plan.Rows = append(plan.Rows, splitPairRow(left, right, anchorSrc))
		}
		i = addedEnd
	}
}

// sameHunkBody reports whether row r belongs to the same (file, hunk) as
// origin and is a body row (not header/binary/etc). Used to limit a
// removed/added run to a single hunk.
func sameHunkBody(r, origin LogicalRow) bool {
	if r.FileIdx != origin.FileIdx || r.HunkIdx != origin.HunkIdx {
		return false
	}
	return r.Kind == RowRemoved || r.Kind == RowAdded || r.Kind == RowContext
}

// splitPairRow packages a (left, right) split pair into a DisplayRow.
// The outer Row carries only the anchor; rendering uses Left/Right.
func splitPairRow(left, right *Row, anchorSrc LogicalRow) DisplayRow {
	return DisplayRow{
		Row: Row{
			Kind: anchorSrc.Kind,
			Anchor: RowAnchor{
				FileIdx: anchorSrc.FileIdx, HunkIdx: anchorSrc.HunkIdx,
				OldLine: anchorSrc.OldLine, NewLine: anchorSrc.NewLine,
			},
		},
		Left:  left,
		Right: right,
	}
}

// buildUnified emits one DisplayRow per visible LogicalRow.
func buildUnified(
	ld LogicalDiff,
	state ViewState,
	palette Palette,
	highlight Highlight,
	gutters map[int]int,
	plan *DisplayPlan,
) {
	i := 0
	for i < len(ld.Rows) {
		if next, skip := advanceFolds(ld, state, palette, plan, i); skip {
			i = next
			continue
		}
		r := ld.Rows[i]
		plan.Rows = append(plan.Rows, buildLogicalDisplayRow(r, gutters[r.FileIdx], ld, palette, highlight))
		i++
	}
}

// foldPlaceholderRow returns a placeholder row for an active fold region.
// The host's keybindings (which key expands which fold kind) are not the
// diff package's concern, so the text stays generic — callers can override
// via FoldRegion.Placeholder. FoldFile placeholders include the file
// header so the user always sees which file is collapsed.
func foldPlaceholderRow(ld LogicalDiff, region FoldRegion, rowIdx int, palette Palette) DisplayRow {
	endIdx := ld.IndexOf(region.End)
	hidden := 0
	if endIdx > rowIdx {
		hidden = endIdx - rowIdx
	}
	r := ld.Rows[rowIdx]
	anchor := RowAnchor{
		FileIdx: r.FileIdx, HunkIdx: r.HunkIdx,
		OldLine: r.OldLine, NewLine: r.NewLine,
		Tag: "fold-placeholder",
	}
	if region.Placeholder == "" && region.Kind == FoldFile && r.FileIdx >= 0 && r.FileIdx < len(ld.Files) {
		return DisplayRow{Row: Row{
			Kind:   RowCollapsedContext,
			LineBG: palette.FileHeaderBG,
			Cells:  fileHeaderCells(ld.Files[r.FileIdx], palette),
			Anchor: anchor,
		}}
	}
	text := region.Placeholder
	if text == "" {
		text = fmt.Sprintf("  ··· (%d rows hidden)", hidden)
	}
	return DisplayRow{Row: Row{
		Kind:   RowCollapsedContext,
		Cells:  []Cell{{Text: text, FG: palette.TextSecondary}},
		Anchor: anchor,
	}}
}

// buildLogicalDisplayRow renders one LogicalRow as a DisplayRow, applying
// syntax highlight and diff backgrounds.
func buildLogicalDisplayRow(
	r LogicalRow,
	gutterWidth int,
	ld LogicalDiff,
	palette Palette,
	highlight Highlight,
) DisplayRow {
	anchor := RowAnchor{
		FileIdx: r.FileIdx, HunkIdx: r.HunkIdx,
		OldLine: r.OldLine, NewLine: r.NewLine,
	}
	switch r.Kind {
	case RowFileHeader:
		fd := git.FileDiff{}
		if r.FileIdx >= 0 && r.FileIdx < len(ld.Files) {
			fd = ld.Files[r.FileIdx]
		}
		return DisplayRow{Row: Row{
			Kind:   RowFileHeader,
			LineBG: palette.FileHeaderBG,
			Cells:  fileHeaderCells(fd, palette),
			Anchor: anchor,
		}}
	case RowHunkHeader:
		return DisplayRow{Row: Row{
			Kind:   RowHunkHeader,
			Cells:  []Cell{{Text: r.Content, FG: palette.HunkHeaderFG}},
			Anchor: anchor,
		}}
	case RowBinary:
		return DisplayRow{Row: Row{
			Kind:   RowBinary,
			Cells:  []Cell{{Text: "  Binary file", FG: palette.TextSecondary, Dim: true}},
			Anchor: anchor,
		}}
	}
	// Body line (Context/Added/Removed)
	row := Row{Anchor: anchor}
	var signCell Cell
	switch r.Kind {
	case RowAdded:
		row.Kind = RowAdded
		row.LineBG = palette.AddedBG
		row.Cells = append(row.Cells, gutterCell(r.NewLine, gutterWidth, palette.AddedFG))
		signCell = Cell{Text: "+", FG: palette.AddedFG}
	case RowRemoved:
		row.Kind = RowRemoved
		row.LineBG = palette.RemovedBG
		row.Cells = append(row.Cells, gutterCell(r.OldLine, gutterWidth, palette.RemovedFG))
		signCell = Cell{Text: "-", FG: palette.RemovedFG}
	default:
		row.Kind = RowContext
		lineNum := r.NewLine
		if lineNum == 0 {
			lineNum = r.OldLine
		}
		row.Cells = append(row.Cells, gutterCell(lineNum, gutterWidth, palette.LineNumFG))
		signCell = Cell{Text: " "}
	}
	row.Cells = append(row.Cells, Cell{Text: " "}, signCell, Cell{Text: " "})
	content := expandTabs(r.Content)
	if highlight != nil {
		row.Cells = append(row.Cells, highlight(content, filePathOf(ld, r.FileIdx))...)
	} else {
		row.Cells = append(row.Cells, Cell{Text: content})
	}
	return DisplayRow{Row: row}
}

// filePathOf returns the user-facing path (NewPath, falling back to
// OldPath) for the given file index, or "" if out of range.
func filePathOf(ld LogicalDiff, fileIdx int) string {
	if fileIdx < 0 || fileIdx >= len(ld.Files) {
		return ""
	}
	if p := ld.Files[fileIdx].NewPath; p != "" {
		return p
	}
	return ld.Files[fileIdx].OldPath
}

// fileHeaderCells renders the file diff summary as styled cells:
// bold "icon path" plus colored "(+N -M)". Used by both the expanded
// RowFileHeader and FoldFile placeholders.
func fileHeaderCells(d git.FileDiff, palette Palette) []Cell {
	icon, path, added, removed := fileHeaderParts(d)
	return []Cell{
		{Text: "  "},
		{Text: icon + " " + path, Bold: true},
		{Text: "  (+"},
		{Text: fmt.Sprintf("%d", added), FG: palette.AddedFG},
		{Text: " -"},
		{Text: fmt.Sprintf("%d", removed), FG: palette.RemovedFG},
		{Text: ")"},
	}
}

func fileHeaderParts(d git.FileDiff) (icon, path string, added, removed int) {
	icon = "~"
	switch d.Status {
	case git.DiffStatusAdded:
		icon = "+"
	case git.DiffStatusDeleted:
		icon = "-"
	case git.DiffStatusRenamed:
		icon = "→"
	}
	path = d.NewPath
	if path == "" {
		path = d.OldPath
	}
	for _, h := range d.Hunks {
		for _, l := range h.Lines {
			switch l.Type {
			case git.LineAdded:
				added++
			case git.LineRemoved:
				removed++
			}
		}
	}
	return
}

// fileGutterWidth picks a per-file gutter width from the largest line
// number across that file's hunks.
func fileGutterWidth(f git.FileDiff) int {
	max := 0
	for _, h := range f.Hunks {
		if n := h.OldStart + h.OldCount; n > max {
			max = n
		}
		if n := h.NewStart + h.NewCount; n > max {
			max = n
		}
	}
	w := 1
	for n := max; n >= 10; n /= 10 {
		w++
	}
	return w
}

// gutterCell builds a right-aligned line-number gutter cell with the given FG.
func gutterCell(n, width int, fg string) Cell {
	text := fmt.Sprintf("%*d", width, n)
	if n == 0 {
		text = spaces(width)
	}
	return Cell{Text: text, FG: fg}
}

// expandTabs replaces tabs with 4 spaces and strips carriage returns.
func expandTabs(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\t':
			out = append(out, ' ', ' ', ' ', ' ')
		case '\r':
			continue
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}
