// assert.go - Render assertion helpers for TUI tests
package test

import (
	"regexp"
	"strings"
	"testing"
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

// truncate shortens a string to max characters for readable error messages.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
