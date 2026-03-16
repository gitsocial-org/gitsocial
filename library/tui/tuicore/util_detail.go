// util_detail.go - Detail view rendering utilities (progress bars, separators)
package tuicore

import (
	"fmt"
	"strings"
)

// RenderProgressBar renders a progress bar with completed/total count.
// Example output: ████████░░░░░░░░  8/15 (53%)
func RenderProgressBar(completed, total, barWidth int) string {
	if total == 0 {
		return Dim.Render("No items")
	}
	percent := 0
	if total > 0 {
		percent = (completed * 100) / total
	}
	filled := 0
	if barWidth > 0 && total > 0 {
		filled = (completed * barWidth) / total
	}
	empty := barWidth - filled
	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	return fmt.Sprintf("%s  %d/%d (%d%%)", bar, completed, total, percent)
}

// RenderSectionSeparator renders a section separator ( ═══).
func RenderSectionSeparator(width int) string {
	if width < 3 {
		return ""
	}
	return " " + Dim.Render(strings.Repeat("═", width-3))
}

// RenderItemSeparator renders an item separator (───) matching a given depth indent.
func RenderItemSeparator(width, depth int) string {
	if depth > 8 {
		depth = 8
	}
	indent := " "
	sepWidth := width - 3
	if depth > 0 {
		indent = strings.Repeat("    ", depth)
		sepWidth = width - len(indent) - 2
	}
	if sepWidth < 1 {
		sepWidth = 1
	}
	return indent + Dim.Render(strings.Repeat("─", sepWidth))
}
