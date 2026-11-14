// safeline.go - One line of remote text that a notification can show
package text

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// SafeLine returns the first line of s that is not empty, with no control, bidi or zero-width character and its spaces collapsed, cut to limit code points with "..." inside the limit; a limit of 0 does not cut. safeLine in gs-render.js is its copy.
func SafeLine(s string, limit int) string {
	for _, line := range strings.FieldsFunc(strings.ToValidUTF8(s, string(utf8.RuneError)), isLineEnd) {
		if clean := cleanLine(line); clean != "" {
			return cutLine(clean, limit)
		}
	}
	return ""
}

// cleanLine changes a tab to a space, removes each control and hidden character, collapses each run of spaces to one and trims the line.
func cleanLine(line string) string {
	var b strings.Builder
	space := false
	for _, r := range line {
		switch {
		case r == '\t' || unicode.Is(unicode.Zs, r):
			space = true
			continue
		case unicode.Is(unicode.Cc, r) || isHiddenMark(r):
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// cutLine cuts a line to limit code points with Fit, and keeps no joiner or variation selector before the "...".
func cutLine(line string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(line) <= limit {
		return line
	}
	if limit < 4 {
		return Fit(line, limit)
	}
	kept := strings.TrimSuffix(Fit(line, limit), "...")
	return strings.TrimRightFunc(kept, isJoinMark) + "..."
}

// isLineEnd reports whether r ends a line: LF, CR, VT, FF, NEL, LS or PS.
func isLineEnd(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

// isHiddenMark reports whether r is a bidi control, a zero-width space or a byte order mark.
func isHiddenMark(r rune) bool {
	switch r {
	case 0x061C, 0x200B, 0x200E, 0x200F, 0xFEFF:
		return true
	}
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}

// isJoinMark reports whether r is a zero-width joiner or a variation selector.
func isJoinMark(r rune) bool {
	return r == 0x200D || (r >= 0xFE00 && r <= 0xFE0F) || (r >= 0xE0100 && r <= 0xE01EF)
}
