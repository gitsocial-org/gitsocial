// state.go - ViewState (immutable user-controllable diff state) and the
// uniform FoldRegion that subsumes file/hunk/context folds.
package diff

// Layout chooses how DisplayPlan composes logical rows. The cell model
// is the same in all layouts; only composition differs.
type Layout int

const (
	// LayoutUnified emits one DisplayRow per LogicalRow.
	LayoutUnified Layout = iota
	// LayoutSplit pairs adjacent removed/added rows side-by-side.
	LayoutSplit
	// LayoutFullscreen is split layout with the nav panel hidden.
	LayoutFullscreen
)

// FoldRegion replaces three fold systems (file collapse, hunk fold,
// context auto-collapse) with one primitive. A region is "active" (rows
// hidden) when UserOpened is false. The Placeholder text shows in place
// of the hidden rows.
type FoldRegion struct {
	Start       RowAnchor
	End         RowAnchor // exclusive
	Placeholder string
	Kind        FoldKind
	UserOpened  bool // true once the user pressed e/enter on this region
}

// FoldKind is informational; the FoldRegion behaves the same regardless,
// but the kind helps tests + key handlers decide how to mutate.
type FoldKind int

const (
	// FoldFile collapses a whole file (its hunks + headers).
	FoldFile FoldKind = iota
	// FoldHunk collapses a single hunk's body, keeping its header.
	FoldHunk
	// FoldContext collapses a long unchanged-context run inside a hunk.
	FoldContext
)

// ViewState is the mutable user-controllable state of a diff view.
// Small struct, easy to copy and test. Used by BuildPlan and by the
// view to render h-scroll / wrap. Cursor/Scroll/search live on the
// host (DiffViewCore) because they don't shape the plan itself.
type ViewState struct {
	Layout  Layout
	Wrap    bool
	Folds   []FoldRegion
	ScrollH int // 0 when Wrap is true; consumed by the view's render path
}

// IsHidden returns whether the logical row at rowIdx is inside an active
// (not user-opened) FoldRegion. Used by BuildPlan to drop folded rows.
//
// "Hidden" means: the row should not appear in the DisplayPlan at all,
// except via its region's placeholder row.
func (s ViewState) IsHidden(ld LogicalDiff, rowIdx int) (hidden bool, regionIdx int) {
	if rowIdx < 0 || rowIdx >= len(ld.Rows) {
		return false, -1
	}
	for i, r := range s.Folds {
		if r.UserOpened {
			continue
		}
		startIdx := ld.IndexOf(r.Start)
		if startIdx < 0 {
			continue
		}
		endIdx := ld.EffectiveEnd(r.End)
		// Hidden interior: (startIdx, endIdx). The Start row itself is the
		// placeholder anchor and stays visible; End is exclusive of the
		// region, so it's also visible (or beyond the last row).
		if rowIdx > startIdx && rowIdx < endIdx {
			return true, i
		}
	}
	return false, -1
}

// IsPlaceholderRow returns the FoldRegion index for which `rowIdx` is the
// Start anchor (and the region is currently folded), or -1 otherwise.
// Such rows render as "··· N lines hidden ···" placeholders.
func (s ViewState) IsPlaceholderRow(ld LogicalDiff, rowIdx int) int {
	if rowIdx < 0 || rowIdx >= len(ld.Rows) {
		return -1
	}
	for i, r := range s.Folds {
		if r.UserOpened {
			continue
		}
		if ld.IndexOf(r.Start) == rowIdx {
			return i
		}
	}
	return -1
}

// ContextAutoFolds returns FoldRegions that auto-collapse long unchanged
// context runs inside hunks. A run is a sequence of >threshold consecutive
// RowContext rows in the same (FileIdx, HunkIdx); the middle portion folds,
// keeping `keepEdge` rows visible at each end so the change is still in
// view.
func ContextAutoFolds(d LogicalDiff) []FoldRegion {
	const (
		threshold = 7
		keepEdge  = 3
	)
	var out []FoldRegion
	i := 0
	for i < len(d.Rows) {
		if d.Rows[i].Kind != RowContext {
			i++
			continue
		}
		start := i
		fileIdx, hunkIdx := d.Rows[i].FileIdx, d.Rows[i].HunkIdx
		for i < len(d.Rows) && d.Rows[i].Kind == RowContext &&
			d.Rows[i].FileIdx == fileIdx && d.Rows[i].HunkIdx == hunkIdx {
			i++
		}
		if i-start <= threshold {
			continue
		}
		out = append(out, FoldRegion{
			Start: d.Anchor(start + keepEdge),
			End:   d.Anchor(i - keepEdge),
			Kind:  FoldContext,
		})
	}
	return out
}
