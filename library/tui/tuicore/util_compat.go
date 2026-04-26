// util_compat.go - Terminal compatibility utilities for width-mismatched terminals
package tuicore

import "github.com/mattn/go-runewidth"

// NeedsWidthMargin is true for terminals (like macOS Terminal.app) that render
// East Asian Ambiguous-width Unicode symbols wider than go-runewidth calculates.
var NeedsWidthMargin bool

// ambiguousFallbacks maps East Asian Ambiguous-width icon runes to ASCII
// replacements that render correctly in Terminal.app. Only ambiguous-width
// characters need replacement; neutral-width icons (⌕, ⚑, ⏱, ⏏, ⑂, etc.)
// render at single-width in Terminal.app and are left unchanged.
var ambiguousFallbacks = map[rune]string{
	'※': "*", // REFERENCE MARK (config/core)
	'○': "o", // WHITE CIRCLE (issues)
	'◇': "~", // WHITE DIAMOND (milestones)
	'◉': "@", // FISHEYE (post detail)
	'▦': "#", // SQUARE WITH CROSSHATCH (board)
	'♥': "*", // BLACK HEART SUIT (my repo)
	'⚿': "=", // SQUARED KEY (identity/verified)
	'⛨': "+", // BLACK CROSS ON SHIELD (security)
	'⛫': "^", // CASTLE (infrastructure)
}

// SafeIcon returns a safe ASCII replacement when the icon contains East Asian
// Ambiguous-width characters that Terminal.app renders as double-width but
// go-runewidth counts as single. Neutral-width Unicode icons are left unchanged.
func SafeIcon(icon string) string {
	if !NeedsWidthMargin || icon == "" {
		return icon
	}
	for _, r := range icon {
		if r > 127 && runewidth.IsAmbiguousWidth(r) {
			if fb, ok := ambiguousFallbacks[r]; ok {
				return fb
			}
			return ">"
		}
	}
	return icon
}
