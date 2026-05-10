// transform_test.go - Tests for SliceRow + WrapRow.
package diff

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func cellText(row Row) string {
	var b strings.Builder
	for _, c := range row.Cells {
		b.WriteString(c.Text)
	}
	return b.String()
}

// TestSliceRow_basic clips a row to a column window across cell boundaries.
func TestSliceRow_basic(t *testing.T) {
	row := Row{Cells: []Cell{
		{Text: "abc"}, {Text: "def"}, {Text: "ghi"},
	}}
	out := SliceRow(row, 2, 5)
	if got := cellText(out); got != "cdefg" {
		t.Errorf("got %q, want %q", got, "cdefg")
	}
}

// TestSliceRow_emptyWindow returns no cells when the slice window is empty.
func TestSliceRow_emptyWindow(t *testing.T) {
	row := Row{Cells: []Cell{{Text: "abc"}}}
	out := SliceRow(row, 5, 3)
	if got := cellText(out); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestSliceRow_dropsWideCharStraddlingBoundary
func TestSliceRow_dropsWideCharStraddlingBoundary(t *testing.T) {
	row := Row{Cells: []Cell{{Text: "a日bc"}}}
	out := SliceRow(row, 0, 2)
	if got := cellText(out); got != "a" {
		t.Errorf("got %q, want %q (wide char dropped)", got, "a")
	}
}

// TestSliceRow_preservesCellAttributes
func TestSliceRow_preservesCellAttributes(t *testing.T) {
	row := Row{Cells: []Cell{
		{Text: "abc", FG: "#ff0000"},
		{Text: "def", FG: "#00ff00", Bold: true},
	}}
	out := SliceRow(row, 1, 3)
	if len(out.Cells) != 2 {
		t.Fatalf("expected 2 cells, got %d (%+v)", len(out.Cells), out.Cells)
	}
	if out.Cells[0].FG != "#ff0000" || out.Cells[0].Text != "bc" {
		t.Errorf("first cell wrong: %+v", out.Cells[0])
	}
	if out.Cells[1].FG != "#00ff00" || !out.Cells[1].Bold || out.Cells[1].Text != "d" {
		t.Errorf("second cell wrong: %+v", out.Cells[1])
	}
}

// TestWrapRow_noWrapWhenFits returns input unchanged.
func TestWrapRow_noWrapWhenFits(t *testing.T) {
	row := Row{Cells: []Cell{{Text: "hello"}}}
	out := WrapRow(row, 10, 0)
	if len(out) != 1 || cellText(out[0]) != "hello" {
		t.Errorf("expected 1 row of 'hello', got %+v", out)
	}
}

// TestWrapRow_splitsAcrossCells preserves cell colors per output row.
func TestWrapRow_splitsAcrossCells(t *testing.T) {
	row := Row{Cells: []Cell{
		{Text: "abcde", FG: "#ff0000"},
		{Text: "fghij", FG: "#00ff00"},
	}}
	out := WrapRow(row, 4, 0)
	if len(out) != 3 {
		t.Fatalf("expected 3 rows, got %d:\n%+v", len(out), out)
	}
	wantTexts := []string{"abcd", "efgh", "ij"}
	for i, w := range wantTexts {
		if got := cellText(out[i]); got != w {
			t.Errorf("row %d text %q, want %q", i, got, w)
		}
	}
	if out[1].Cells[0].FG != "#ff0000" {
		t.Errorf("wrap continuation lost FG on tail of cell")
	}
}

// TestWrapRow_continuationAnchorTagged annotates continuation rows so the
// view layer can identify them (e.g. to skip cursor zone marking).
func TestWrapRow_continuationAnchorTagged(t *testing.T) {
	row := Row{
		Cells:  []Cell{{Text: strings.Repeat("x", 25)}},
		Anchor: RowAnchor{FileIdx: 1, HunkIdx: 0, OldLine: 5},
	}
	out := WrapRow(row, 10, 0)
	if len(out) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(out))
	}
	if out[0].Anchor.Tag != "" {
		t.Errorf("first row should keep clean tag, got %q", out[0].Anchor.Tag)
	}
	if !strings.Contains(out[1].Anchor.Tag, "wrap-cont:1") {
		t.Errorf("second row missing wrap-cont tag: %q", out[1].Anchor.Tag)
	}
	if !strings.Contains(out[2].Anchor.Tag, "wrap-cont:2") {
		t.Errorf("third row missing wrap-cont tag: %q", out[2].Anchor.Tag)
	}
}

// TestWrapRow_widthInvariantThroughRender — every wrapped row, when rendered
// with cols=wrapWidth, must produce exactly wrapWidth display columns.
func TestWrapRow_widthInvariantThroughRender(t *testing.T) {
	row := Row{
		LineBG: "#1a3524",
		Cells: []Cell{
			{Text: strings.Repeat("ab", 30), FG: "#ffffff"},
		},
	}
	const cols = 14
	for i, r := range WrapRow(row, cols, 0) {
		out := RenderRow(r, cols)
		if got := ansi.StringWidth(out); got != cols {
			t.Errorf("row %d rendered width %d, want %d", i, got, cols)
		}
	}
}
