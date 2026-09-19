// truncate.go - Rune-safe string truncation shared across packages
package text

// Truncate cuts s to n characters and appends an ellipsis, so the result is at
// most n+3. Counts runes, not bytes, so a cut never splits a character. For a
// hard output budget, pass n as the budget minus 3.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// Fit cuts s to at most width characters, counting the ellipsis inside width.
// Counts runes, not bytes, so a cut never splits a character.
func Fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width < 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}
