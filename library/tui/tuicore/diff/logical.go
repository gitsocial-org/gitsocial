// logical.go - The pure-data layer: LogicalDiff is the source of truth.
//
// One LogicalRow per source line of every file; no styling, no layout,
// no fold state. BuildPlan transforms LogicalDiff + ViewState into a
// DisplayPlan that the renderer consumes.
package diff

import (
	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// LogicalRow is the smallest stable unit: one source line of one file.
// Re-buildable from git.FileDiff alone. Anchor uniquely identifies a
// logical row across view-state changes.
type LogicalRow struct {
	FileIdx int
	HunkIdx int     // -1 for file headers / binary placeholders
	Kind    RowKind // RowFileHeader / RowHunkHeader / RowContext / RowAdded / RowRemoved / RowBinary
	OldLine int     // 0 when N/A
	NewLine int
	Content string // raw source content; tabs not expanded
}

// LogicalDiff is the immutable source of truth for one commit / PR diff.
// Rows is a flat sequence (cheap to index); the (FileIdx, HunkIdx) tuple
// groups them.
type LogicalDiff struct {
	Files []git.FileDiff
	Rows  []LogicalRow
}

// BuildLogical flattens a sequence of file diffs into LogicalRows. Each
// file contributes one RowFileHeader, then either a RowBinary or one
// hunk-header + body-row sequence per hunk.
func BuildLogical(files []git.FileDiff) LogicalDiff {
	out := LogicalDiff{Files: files}
	for i, f := range files {
		out.Rows = append(out.Rows, LogicalRow{
			FileIdx: i, HunkIdx: -1, Kind: RowFileHeader,
		})
		if f.Binary {
			out.Rows = append(out.Rows, LogicalRow{
				FileIdx: i, HunkIdx: -1, Kind: RowBinary,
			})
			continue
		}
		for hi, h := range f.Hunks {
			out.Rows = append(out.Rows, LogicalRow{
				FileIdx: i, HunkIdx: hi, Kind: RowHunkHeader,
				Content: h.Header,
			})
			for _, dl := range h.Lines {
				row := LogicalRow{
					FileIdx: i, HunkIdx: hi,
					Content: dl.Content,
					OldLine: dl.OldNum,
					NewLine: dl.NewNum,
				}
				switch dl.Type {
				case git.LineAdded:
					row.Kind = RowAdded
				case git.LineRemoved:
					row.Kind = RowRemoved
				default:
					row.Kind = RowContext
				}
				out.Rows = append(out.Rows, row)
			}
		}
	}
	return out
}

// Anchor returns a stable identity for the row at the given index. The
// returned anchor survives any view-state change: rebuilding the same
// LogicalDiff and looking the anchor up again yields the same row.
func (d LogicalDiff) Anchor(rowIdx int) RowAnchor {
	if rowIdx < 0 || rowIdx >= len(d.Rows) {
		return RowAnchor{}
	}
	r := d.Rows[rowIdx]
	return RowAnchor{
		FileIdx: r.FileIdx,
		HunkIdx: r.HunkIdx,
		OldLine: r.OldLine,
		NewLine: r.NewLine,
	}
}

// IndexOf returns the index of the row that exactly matches the given
// anchor's (FileIdx, HunkIdx, OldLine, NewLine), or -1 if none. Tag is
// ignored — that's a render-time concern.
func (d LogicalDiff) IndexOf(a RowAnchor) int {
	for i, r := range d.Rows {
		if r.FileIdx == a.FileIdx && r.HunkIdx == a.HunkIdx &&
			r.OldLine == a.OldLine && r.NewLine == a.NewLine {
			return i
		}
	}
	return -1
}

// EffectiveEnd resolves a FoldRegion End anchor to a row index, treating
// any unresolvable anchor as "past the last row" (len(Rows)). This lets
// FoldFile regions for the final file use a sentinel `End` anchor (e.g.
// next-file-header) that doesn't actually exist in the diff.
func (d LogicalDiff) EffectiveEnd(a RowAnchor) int {
	if idx := d.IndexOf(a); idx >= 0 {
		return idx
	}
	return len(d.Rows)
}

// HunkBounds returns [start, end) row indices for the hunk identified by
// (fileIdx, hunkIdx). Used by hunk-fold and reveal-scroll. Returns -1,-1
// if no such hunk.
func (d LogicalDiff) HunkBounds(fileIdx, hunkIdx int) (int, int) {
	start := -1
	for i, r := range d.Rows {
		if r.FileIdx == fileIdx && r.HunkIdx == hunkIdx && r.Kind == RowHunkHeader {
			start = i
			break
		}
	}
	if start < 0 {
		return -1, -1
	}
	end := len(d.Rows)
	for i := start + 1; i < len(d.Rows); i++ {
		r := d.Rows[i]
		if r.FileIdx != fileIdx || r.HunkIdx != hunkIdx {
			end = i
			break
		}
	}
	return start, end
}
