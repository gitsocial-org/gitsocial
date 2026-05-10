// palette.go - Color palette + syntax-highlight callback for the row model.
//
// The diff package is decoupled from tuicore's color constants and the
// Chroma engine: callers supply both. Tests can inject fake palettes
// and a trivial highlighter that returns a single plain cell.
package diff

// Palette names every color the row builders need. Callers wire these
// from their own theme constants.
type Palette struct {
	AddedFG            string
	RemovedFG          string
	AddedBG            string
	RemovedBG          string
	IntraLineAddedBG   string
	IntraLineRemovedBG string
	FileHeaderBG       string
	HunkHeaderFG       string
	LineNumFG          string
	TextSecondary      string
}

// Highlight returns one cell per token in the source line, with FG / attrs
// from the active syntax-highlight theme. `path` is the file the line came
// from; the adapter is responsible for mapping path → language. For tests,
// a trivial impl that returns `[]Cell{{Text: line}}` is enough.
type Highlight func(line, path string) []Cell
