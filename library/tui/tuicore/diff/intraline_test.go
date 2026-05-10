// intraline_test.go - Tests for intra-line word/char highlighting.
package diff

import (
	"strings"
	"testing"
)

// TestIntraLineDiff_basic asserts that a single-word change is captured on
// both sides via char-level LCS.
func TestIntraLineDiff_basic(t *testing.T) {
	old := "foo bar baz"
	nw := "foo qux baz"
	oldSpans, newSpans := intraLineDiff(old, nw, 0)
	if len(oldSpans) != 1 || oldSpans[0] != (intraSpan{4, 7}) {
		t.Errorf("old spans = %+v, want [{4,7}]", oldSpans)
	}
	if len(newSpans) != 1 || newSpans[0] != (intraSpan{4, 7}) {
		t.Errorf("new spans = %+v, want [{4,7}]", newSpans)
	}
}

// TestIntraLineDiff_mergeBridgesShortGaps asserts that adjacent diff spans
// separated by short matches (like a space between two changed words) merge
// into a single span when mergeGap >= the bridge.
func TestIntraLineDiff_mergeBridgesShortGaps(t *testing.T) {
	old := "p Palette"
	nw := "fg string"
	oldSpans, newSpans := intraLineDiff(old, nw, 3)
	// expect ONE span covering the whole differing region on each side
	if len(oldSpans) != 1 || oldSpans[0].Start != 0 || oldSpans[0].End != len(old) {
		t.Errorf("old spans = %+v, want one span covering full string", oldSpans)
	}
	if len(newSpans) != 1 || newSpans[0].Start != 0 || newSpans[0].End != len(nw) {
		t.Errorf("new spans = %+v, want one span covering full string", newSpans)
	}
}

// TestApplyIntraLineToCells_subdividesCell asserts that a span crossing a
// single cell results in three output cells (before/inside/after) with FG
// preserved everywhere and BG stamped only on the highlighted segment.
func TestApplyIntraLineToCells_subdividesCell(t *testing.T) {
	cells := []Cell{
		{Text: "  1"},                      // gutter
		{Text: " "},                        // pad
		{Text: "+", FG: "#4ae04a"},         // sign
		{Text: " "},                        // pad
		{Text: "let foo = bar", FG: "#ff"}, // content (single cell)
	}
	// highlight rune indices [4,7) within content == "foo"
	spans := []intraSpan{{4, 7}}
	out := applyIntraLineToCells(cells, bodyPrefixCells, spans, "#bg")
	// content cells should be: "let ", "foo" (with BG), " = bar"
	contentCells := out[bodyPrefixCells:]
	if len(contentCells) != 3 {
		t.Fatalf("got %d content cells, want 3 (%+v)", len(contentCells), contentCells)
	}
	if contentCells[0].Text != "let " || contentCells[0].BG != "" {
		t.Errorf("seg0 = %+v, want {Text:\"let \", BG:\"\"}", contentCells[0])
	}
	if contentCells[1].Text != "foo" || contentCells[1].BG != "#bg" {
		t.Errorf("seg1 = %+v, want {Text:\"foo\", BG:\"#bg\"}", contentCells[1])
	}
	if contentCells[2].Text != " = bar" || contentCells[2].BG != "" {
		t.Errorf("seg2 = %+v, want {Text:\" = bar\", BG:\"\"}", contentCells[2])
	}
	// FG should be preserved on every content cell
	for i, c := range contentCells {
		if c.FG != "#ff" {
			t.Errorf("seg%d FG = %q, want %q", i, c.FG, "#ff")
		}
	}
}

// TestApplyIntraLineHighlights_unified asserts that paired removed/added
// rows get their differing portions highlighted.
func TestApplyIntraLineHighlights_unified(t *testing.T) {
	plan := &DisplayPlan{Rows: []DisplayRow{
		bodyRow(RowRemoved, "foo bar", "#3b1a1e"),
		bodyRow(RowAdded, "foo qux", "#15291c"),
	}}
	palette := Palette{
		IntraLineAddedBG:   "#2a5a34",
		IntraLineRemovedBG: "#6a2a2e",
	}
	applyIntraLineHighlights(plan, palette)
	if !containsCellWithBG(plan.Rows[0].Cells, "#6a2a2e") {
		t.Errorf("removed row has no IntraLineRemovedBG cell:\n%s", dumpCells(plan.Rows[0].Cells))
	}
	if !containsCellWithBG(plan.Rows[1].Cells, "#2a5a34") {
		t.Errorf("added row has no IntraLineAddedBG cell:\n%s", dumpCells(plan.Rows[1].Cells))
	}
}

// TestApplyIntraLineHighlights_skipsCompletelyDifferentLines asserts the
// >80% threshold: a row pair that has almost no common content is left alone.
func TestApplyIntraLineHighlights_skipsCompletelyDifferentLines(t *testing.T) {
	plan := &DisplayPlan{Rows: []DisplayRow{
		bodyRow(RowRemoved, "abcdefghij", "#3b1a1e"),
		bodyRow(RowAdded, "ZYXWVUTSRQ", "#15291c"),
	}}
	palette := Palette{
		IntraLineAddedBG:   "#2a5a34",
		IntraLineRemovedBG: "#6a2a2e",
	}
	applyIntraLineHighlights(plan, palette)
	if containsCellWithBG(plan.Rows[0].Cells, "#6a2a2e") {
		t.Errorf("removed row should not be highlighted (lines too different)")
	}
	if containsCellWithBG(plan.Rows[1].Cells, "#2a5a34") {
		t.Errorf("added row should not be highlighted (lines too different)")
	}
}

// helpers

func bodyRow(kind RowKind, content, lineBG string) DisplayRow {
	cells := []Cell{
		{Text: "  1"},
		{Text: " "},
		{Text: signOf(kind)},
		{Text: " "},
		{Text: content},
	}
	return DisplayRow{Row: Row{Kind: kind, LineBG: lineBG, Cells: cells}}
}

func signOf(kind RowKind) string {
	switch kind {
	case RowAdded:
		return "+"
	case RowRemoved:
		return "-"
	}
	return " "
}

func containsCellWithBG(cells []Cell, bg string) bool {
	for _, c := range cells {
		if c.BG == bg {
			return true
		}
	}
	return false
}

func dumpCells(cells []Cell) string {
	var b strings.Builder
	for _, c := range cells {
		b.WriteString("  ")
		b.WriteString(c.Text)
		b.WriteString(" [BG=")
		b.WriteString(c.BG)
		b.WriteString("]\n")
	}
	return b.String()
}
