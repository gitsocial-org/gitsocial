// util_width.go - Fast ANSI-aware string width measurement
package tuicore

import (
	"github.com/mattn/go-runewidth"
)

// AnsiWidth returns the printable cell width of a string containing ANSI escape sequences.
// Uses go-runewidth for character width (simple table lookup) instead of lipgloss.Width()
// which uses UAX#29 grapheme cluster segmentation. This is significantly faster for
// strings that contain only standard terminal output (text + ANSI escape codes).
// Handles both CSI sequences (\x1b[...letter) and OSC sequences (\x1b]...\x07).
func AnsiWidth(s string) int {
	var n int
	var ansi, osc bool
	for _, c := range s {
		if c == '\x1b' {
			ansi = true
			osc = false
		} else if osc {
			if c == '\x07' {
				osc = false
			}
		} else if ansi {
			if c == ']' {
				// OSC sequence (\x1b]...\x07): consume everything until BEL
				osc = true
				ansi = false
			} else if (c >= 0x40 && c <= 0x5a) || (c >= 0x61 && c <= 0x7a) {
				// CSI sequences end with a letter (0x40-0x5a or 0x61-0x7a)
				ansi = false
			}
		} else if c == '\x07' {
			continue
		} else if c == '\x00' || c == '\x01' {
			// Control chars used as internal placeholders
			continue
		} else {
			n += runewidth.RuneWidth(c)
		}
	}
	return n
}
