// render.go - The single ANSI emitter for the cell row model.
package diff

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// suppressBG, when true, drops every background color (row LineBG + per-cell
// BG) from RenderRow output. Used on terminals whose color profile is too
// coarse (≤ANSI16) to render the muted diff BGs distinctly — in 16-color
// mode the dark green/red/blue BGs all collapse to "dark gray", losing the
// added/removed/header distinction. With BGs suppressed the diff falls back
// to FG-only styling plus the `+`/`-` sign, which is what most legacy diff
// tools do.
var suppressBG bool

// SetSuppressLineBG toggles the package-wide BG-suppression flag. The
// adapter in tuicore detects the terminal color profile at startup and
// flips this on for profiles ≤ ANSI; tests can set it explicitly.
func SetSuppressLineBG(suppress bool) { suppressBG = suppress }

// RenderRow emits the ANSI-encoded string for a row, padded with the row's
// LineBG to `cols` display columns. cols <= 0 disables right-padding.
//
// This is the only function in the package that emits SGR sequences.
// Everything else operates on the structured cell model.
func RenderRow(row Row, cols int) string {
	var b strings.Builder
	rendered := 0
	lineBG := row.LineBG
	if suppressBG {
		lineBG = ""
	}
	for _, c := range row.Cells {
		if c.Text == "" {
			continue
		}
		bg := c.BG
		if bg == "" {
			bg = lineBG
		}
		if suppressBG {
			bg = ""
		}
		b.WriteString(styleCell(c, bg).Render(c.Text))
		rendered += stringWidth(c.Text)
	}
	if cols > 0 && rendered < cols {
		b.WriteString(padCells(cols-rendered, lineBG))
	}
	return b.String()
}

// styleCell builds the lipgloss style for a single cell. effectiveBG is
// already resolved (caller picks Cell.BG or Row.LineBG).
func styleCell(c Cell, effectiveBG string) lipgloss.Style {
	s := lipgloss.NewStyle()
	if c.FG != "" {
		s = s.Foreground(lipgloss.Color(c.FG))
	}
	if effectiveBG != "" {
		s = s.Background(lipgloss.Color(effectiveBG))
	}
	if c.Bold {
		s = s.Bold(true)
	}
	if c.Dim {
		s = s.Faint(true)
	}
	if c.Italic {
		s = s.Italic(true)
	}
	if c.Underline {
		s = s.Underline(true)
	}
	return s
}

// padCells emits `n` spaces with the given background color (or plain
// spaces if bg is empty). The whole pad is one styled segment.
func padCells(n int, bg string) string {
	if n <= 0 {
		return ""
	}
	pad := strings.Repeat(" ", n)
	if bg == "" {
		return pad
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(bg)).Render(pad)
}

// stringWidth returns the display width of plain text (no ANSI). Wraps
// ansi.StringWidth so the model's transforms can call a single helper.
func stringWidth(s string) int {
	return ansi.StringWidth(s)
}
