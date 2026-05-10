// render_test.go - P1.1: Cell/Row + RenderRow correctness.
package diff

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestRenderRow_widthInvariant_plainCells asserts RenderRow output has
// exactly cols display columns when padded.
func TestRenderRow_widthInvariant_plainCells(t *testing.T) {
	row := Row{Cells: []Cell{{Text: "hello"}}}
	out := RenderRow(row, 12)
	if got := ansi.StringWidth(out); got != 12 {
		t.Errorf("got width %d, want 12 (out=%q)", got, out)
	}
}

// TestRenderRow_widthInvariant_styledCells asserts width counting works
// correctly even when cells emit SGR codes via lipgloss.
func TestRenderRow_widthInvariant_styledCells(t *testing.T) {
	row := Row{
		LineBG: "#1a3524",
		Cells: []Cell{
			{Text: "+ ", FG: "#4ae04a"},
			{Text: "added line", FG: "#cccccc"},
		},
	}
	out := RenderRow(row, 30)
	if got := ansi.StringWidth(out); got != 30 {
		t.Errorf("got width %d, want 30 (out=%q)", got, out)
	}
}

// TestRenderRow_noPadWhenColsZero leaves rows shorter than viewport.
func TestRenderRow_noPadWhenColsZero(t *testing.T) {
	row := Row{Cells: []Cell{{Text: "abc"}}}
	out := RenderRow(row, 0)
	if got := ansi.StringWidth(out); got != 3 {
		t.Errorf("got width %d, want 3 (no padding when cols=0)", got)
	}
}

// TestRenderRow_lineBGAppliesToEmptyBG cells without their own BG inherit
// the row's LineBG.
func TestRenderRow_lineBGAppliesToEmptyBG(t *testing.T) {
	row := Row{
		LineBG: "#3b1a1e", // DiffRemovedBg
		Cells:  []Cell{{Text: "removed"}},
	}
	out := RenderRow(row, 0)
	// Lipgloss may emit the bg as 24-bit; we just check that *some* SGR is
	// present (output isn't plain text).
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("expected ANSI SGR in output, got %q", out)
	}
	if !strings.Contains(out, "removed") {
		t.Errorf("output missing cell text: %q", out)
	}
}

// TestRenderRow_cellBGOverridesLineBG cells with their own BG win against
// the row's LineBG (used for word-diff emphasis).
func TestRenderRow_cellBGOverridesLineBG(t *testing.T) {
	row := Row{
		LineBG: "#1a3524", // base added bg
		Cells: []Cell{
			{Text: "ctx"},
			{Text: "highlit", BG: "#2a5a34"}, // word-diff emphasis
			{Text: "more"},
		},
	}
	out := RenderRow(row, 0)
	// Both bg colors should appear. Lipgloss emits 24-bit SGR for hex.
	if !strings.Contains(out, "26;53;36") {
		t.Errorf("base bg (26;53;36) missing: %q", out)
	}
	if !strings.Contains(out, "42;90;52") {
		t.Errorf("emphasis bg (42;90;52) missing: %q", out)
	}
}

// TestRenderRow_widthInvariant_wideCharacter wide chars (CJK) count as
// 2 columns; padding should compensate.
func TestRenderRow_widthInvariant_wideCharacter(t *testing.T) {
	row := Row{Cells: []Cell{{Text: "日本"}}}
	out := RenderRow(row, 10)
	if got := ansi.StringWidth(out); got != 10 {
		t.Errorf("got width %d, want 10 (wide char) out=%q", got, out)
	}
}

// TestRenderRow_emptyRowPadsToCols
func TestRenderRow_emptyRowPadsToCols(t *testing.T) {
	row := Row{LineBG: "#1a3524"}
	out := RenderRow(row, 8)
	if got := ansi.StringWidth(out); got != 8 {
		t.Errorf("empty row width %d, want 8 (just the bg pad)", got)
	}
}

// TestRow_Width counts cells (not ANSI).
func TestRow_Width(t *testing.T) {
	row := Row{Cells: []Cell{{Text: "abc"}, {Text: "de"}}}
	if got := row.Width(); got != 5 {
		t.Errorf("Row.Width = %d, want 5", got)
	}
}

// TestRenderRow_suppressLineBG_dropsAllBackgrounds asserts that when the
// package-wide flag is set (mimicking a ≤ANSI16 terminal), neither the row's
// LineBG nor per-cell BGs emit SGR codes — only FG / attrs survive.
func TestRenderRow_suppressLineBG_dropsAllBackgrounds(t *testing.T) {
	SetSuppressLineBG(true)
	t.Cleanup(func() { SetSuppressLineBG(false) })
	row := Row{
		LineBG: "#3b1a1e",
		Cells: []Cell{
			{Text: "ctx", FG: "#4ae04a"},
			{Text: "hit", BG: "#6a2a2e"}, // intra-line BG, should also be dropped
		},
	}
	out := RenderRow(row, 12)
	// No background SGRs of any flavor: 24-bit `48;2;...`, 256-color `48;5;...`,
	// or 4-bit `40-47` / `100-107`.
	for _, prefix := range []string{"48;2;", "48;5;"} {
		if strings.Contains(out, prefix) {
			t.Errorf("expected no BG SGR (%q) in output, got %q", prefix, out)
		}
	}
	// FG should still be present (we only suppress BG).
	if !strings.Contains(out, "38;2;") && !strings.Contains(out, "38;5;") {
		t.Errorf("expected FG SGR to remain, got %q", out)
	}
}
