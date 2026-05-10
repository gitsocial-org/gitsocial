// util_diffrow_adapter_test.go - Sanity tests for the tuicore→diffrow bridge.
package tuicore

import (
	"strings"
	"testing"
)

// TestDefaultHighlight_returnsCellsWithFG asserts the Chroma adapter
// produces at least one cell with a foreground color for a real code
// snippet. The second arg is a file path; the adapter detects the
// language from it.
func TestDefaultHighlight_returnsCellsWithFG(t *testing.T) {
	hi := DefaultHighlight()
	cells := hi("func main() { println(42) }", "main.go")
	if len(cells) == 0 {
		t.Fatal("got no cells")
	}
	var hasFG bool
	var full string
	for _, c := range cells {
		full += c.Text
		if c.FG != "" {
			hasFG = true
		}
	}
	if !hasFG {
		t.Errorf("expected at least one cell with FG, got %+v", cells)
	}
	if !strings.Contains(full, "func main()") {
		t.Errorf("highlighted text missing source: %q", full)
	}
}

// TestDefaultHighlight_unknownPathFallsBack asserts an empty / unknown
// path still yields a usable cell list.
func TestDefaultHighlight_unknownPathFallsBack(t *testing.T) {
	hi := DefaultHighlight()
	cells := hi("hello world", "")
	if len(cells) == 0 {
		t.Fatal("got no cells")
	}
	var full string
	for _, c := range cells {
		full += c.Text
	}
	if !strings.Contains(full, "hello world") {
		t.Errorf("plain text missing: %q", full)
	}
}

// TestDefaultDiffPalette_populated verifies key fields are set from
// tuicore constants.
func TestDefaultDiffPalette_populated(t *testing.T) {
	p := DefaultDiffPalette()
	if p.AddedFG != DiffAdded {
		t.Errorf("AddedFG = %q, want %q", p.AddedFG, DiffAdded)
	}
	if p.AddedBG != DiffAddedBg {
		t.Errorf("AddedBG = %q, want %q", p.AddedBG, DiffAddedBg)
	}
}
