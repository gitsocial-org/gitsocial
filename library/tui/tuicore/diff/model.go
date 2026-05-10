// Package diff renders unified or split diff views as a pure pipeline:
//
//	[]git.FileDiff
//	      ↓ BuildLogical
//	LogicalDiff (immutable source of truth — one LogicalRow per source line)
//	      ↓ BuildPlan(state)
//	DisplayPlan (styled cell rows; folds applied; split pairs composed)
//	      ↓ Layer.Decorate (optional — e.g. PR feedback)
//	DisplayPlan
//	      ↓ caller renders via SliceRow / WrapRow / RenderRow
//	ANSI string
//
// Each stage is a pure function over its inputs; no globals, no I/O.
// ANSI escape codes are produced exclusively by RenderRow; every other
// operation works on the structured Cell model.
//
// model.go defines the data primitives: Cell, Row, RowKind, RowAnchor.
package diff

// Cell is the indivisible unit of a row: a plain-text segment with
// optional foreground / background / attribute overrides. Text contains
// no ANSI escape codes — the renderer adds them.
type Cell struct {
	Text      string
	FG        string // "" inherits row default fg
	BG        string // "" inherits Row.LineBG
	Bold      bool
	Dim       bool
	Italic    bool
	Underline bool
}

// RowKind classifies a row for routing in the view layer.
type RowKind int

const (
	// RowContext is an unchanged context line.
	RowContext RowKind = iota
	// RowAdded is an added line; LineBG should be DiffAddedBg.
	RowAdded
	// RowRemoved is a removed line; LineBG should be DiffRemovedBg.
	RowRemoved
	// RowHunkHeader is a "@@ … @@" hunk header.
	RowHunkHeader
	// RowFileHeader is a per-file header row.
	RowFileHeader
	// RowCollapsedContext is a "··· N lines hidden ···" placeholder.
	RowCollapsedContext
	// RowFeedback is an inline PR feedback row.
	RowFeedback
	// RowBinary is the "Binary file" placeholder.
	RowBinary
)

// Row is a single logical row of diff content. The renderer concatenates
// Cells in order; LineBG fills behind cells whose BG is empty AND extends
// past the last cell to the column boundary so the diff bg reaches the
// edge of the viewport.
type Row struct {
	Kind   RowKind
	Cells  []Cell
	LineBG string
	Anchor RowAnchor
}

// RowAnchor identifies a row across rebuilds for cursor preservation,
// auto-collapse expansion, feedback navigation, and split-mode pairing.
// Two rows with equal Anchor refer to the same logical line.
//
// Tag is an optional discriminator used by synthetic rows that share
// their (FileIdx, HunkIdx, OldLine, NewLine) with a real row: today
// "fold-placeholder", "feedback", and "wrap-cont:<N>" (continuation
// of a wrapped row).
type RowAnchor struct {
	FileIdx int
	HunkIdx int // -1 for non-hunk rows
	OldLine int // 0 when not applicable
	NewLine int // 0 when not applicable
	Tag     string
}

// Width returns the display width of a row in cells. It walks Cells and
// sums their text widths; ANSI is not counted because Cell.Text is plain.
func (r Row) Width() int {
	w := 0
	for _, c := range r.Cells {
		w += stringWidth(c.Text)
	}
	return w
}
