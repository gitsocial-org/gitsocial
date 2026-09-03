// assert.go - Render assertion helpers for TUI tests
package test

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

var (
	ansiCSI = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	ansiOSC = regexp.MustCompile(`\x1b\]8;;[^\x07]*\x07`)
)

// stripANSI removes ANSI escape codes (CSI and OSC 8 hyperlinks) from a string.
func stripANSI(s string) string {
	s = ansiOSC.ReplaceAllString(s, "")
	s = ansiCSI.ReplaceAllString(s, "")
	return s
}

// rendered returns the harness output with ANSI codes stripped.
func rendered(h *Harness) string {
	return stripANSI(h.Rendered())
}

// assertContains checks that substr appears in the stripped output.
func assertContains(t *testing.T, output, substr string) {
	t.Helper()
	stripped := stripANSI(output)
	if !strings.Contains(stripped, substr) {
		t.Errorf("expected output to contain %q, got:\n%s", substr, truncate(stripped, 500))
	}
}

// assertRendersItem navigates to a location and asserts that every fragment of
// seeded content appears in the view. Unlike assertNotEmpty, a view that draws
// only its own chrome and an empty-state message fails.
func assertRendersItem(t *testing.T, h *Harness, loc tuicore.Location, want ...string) {
	t.Helper()
	h.NavigateTo(loc)
	out := renderedAfterLoad(h, want)
	for _, substr := range want {
		// An empty expectation (a fixture field that was never seeded) would
		// match anything, so treat it as a failure rather than a free pass.
		if substr == "" {
			t.Errorf("%s: empty expectation, the fixture field it comes from is unset", loc.Path)
			continue
		}
		if !strings.Contains(out, substr) {
			t.Errorf("%s: expected rendered item content %q, got:\n%s", loc.Path, substr, out)
		}
	}
}

// assertNotEmpty checks that the output is non-empty after stripping ANSI.
func assertNotEmpty(t *testing.T, output string) {
	t.Helper()
	stripped := strings.TrimSpace(stripANSI(output))
	if stripped == "" {
		t.Error("expected non-empty rendered output")
	}
}

// assertLineCount checks that the output doesn't exceed maxLines.
func assertLineCount(t *testing.T, output string, maxLines int) {
	t.Helper()
	stripped := stripANSI(output)
	lines := strings.Split(stripped, "\n")
	if len(lines) > maxLines {
		t.Errorf("got %d lines, max %d", len(lines), maxLines)
	}
}

// lineWidth returns the visible column count of one already ANSI-stripped line.
// Runes, not bytes, because the UI is full of multi-byte box-drawing characters;
// trailing padding is ignored, since views pad rows out with spaces.
func lineWidth(line string) int {
	return utf8.RuneCountInString(strings.TrimRight(line, " "))
}

// assertMaxWidth checks that no rendered line is wider than maxCols. This is the
// horizontal counterpart to assertLineCount: without it a panel border pushed
// past the terminal edge passes every assertion except a golden diff.
func assertMaxWidth(t *testing.T, output string, maxCols int) {
	t.Helper()
	for i, line := range strings.Split(stripANSI(output), "\n") {
		if w := lineWidth(line); w > maxCols {
			t.Errorf("line %d is %d columns wide, max %d:\n%s", i+1, w, maxCols, line)
		}
	}
}

// assertFitsTerminal checks the current render against the harness terminal width.
func assertFitsTerminal(t *testing.T, h *Harness) {
	t.Helper()
	assertMaxWidth(t, h.Rendered(), h.width)
}

// truncate shortens a string to max characters for readable error messages.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// renderedAfterLoad samples the view, and if anything wanted is missing gives
// the async load one more drain before sampling again. A view whose load
// overran the harness command budget renders its loading frame, so a single
// sample makes the assertion a race against the machine.
func renderedAfterLoad(h *Harness, want []string) string {
	out := rendered(h)
	if !missingAny(out, want) {
		return out
	}
	h.DrainCmds()
	return rendered(h)
}

// missingAny reports whether any non-empty wanted substring is absent.
func missingAny(out string, want []string) bool {
	for _, substr := range want {
		if substr != "" && !strings.Contains(out, substr) {
			return true
		}
	}
	return false
}
