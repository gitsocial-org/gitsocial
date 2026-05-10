// util_diff.go - Surviving non-cell-model helpers used by views outside
// the diff package: file-header / stats badge for summary lines and the
// hunk-reveal scroll math.
//
// Everything else (the legacy ANSI string pipeline) was deleted in the
// cell-model migration; see HIGHLIGHT-DESIGN.md and DIFF-DESIGN.md.
package tuicore

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gitsocial-org/gitsocial/core/git"
)

var (
	diffAddedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(DiffAdded))
	diffRemovedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(DiffRemoved))
)

// RenderDiffHeader renders a file diff header with status icon and path.
// Used by views' renderPinnedFileHeader plus a few stats summary lines
// outside the diff view itself.
func RenderDiffHeader(diff git.FileDiff) string {
	icon := "~"
	switch diff.Status {
	case git.DiffStatusAdded:
		icon = "+"
	case git.DiffStatusDeleted:
		icon = "-"
	case git.DiffStatusRenamed:
		icon = "→"
	}
	var added, removed int
	for _, hunk := range diff.Hunks {
		for _, line := range hunk.Lines {
			switch line.Type {
			case git.LineAdded:
				added++
			case git.LineRemoved:
				removed++
			}
		}
	}
	path := diff.NewPath
	if path == "" {
		path = diff.OldPath
	}
	var iconStyle lipgloss.Style
	switch diff.Status {
	case git.DiffStatusAdded:
		iconStyle = diffAddedStyle
	case git.DiffStatusDeleted:
		iconStyle = diffRemovedStyle
	default:
		iconStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(TextPrimary))
	}
	header := fmt.Sprintf("▾ %s %s", iconStyle.Render(icon), path)
	if diff.Status == git.DiffStatusRenamed && diff.OldPath != diff.NewPath {
		header = fmt.Sprintf("▾ %s %s → %s", iconStyle.Render(icon), diff.OldPath, diff.NewPath)
	}
	if added > 0 || removed > 0 {
		header += "  " + RenderDiffStatsBadge(added, removed)
	}
	return header
}

// RenderDiffStatsBadge renders a compact "+N -M" badge.
func RenderDiffStatsBadge(added, removed int) string {
	var parts []string
	if added > 0 {
		parts = append(parts, diffAddedStyle.Render(fmt.Sprintf("+%d", added)))
	}
	if removed > 0 {
		parts = append(parts, diffRemovedStyle.Render(fmt.Sprintf("-%d", removed)))
	}
	return strings.Join(parts, " ")
}

// HunkRevealScroll computes the scroll position that brings a hunk into
// view with ~25% top padding, clamped so the hunk's bottom doesn't clip
// when it fits. Hunks taller than viewport align their top.
func HunkRevealScroll(targetRow, sectionHeight, viewH, totalLines int) int {
	if viewH <= 0 {
		return 0
	}
	pad := viewH / 4
	scroll := targetRow - pad
	if scroll < 0 {
		scroll = 0
	}
	if sectionHeight <= viewH {
		minBottomVisible := targetRow + sectionHeight - viewH
		if scroll < minBottomVisible {
			scroll = minBottomVisible
		}
	}
	if max := totalLines - viewH; max > 0 && scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	return scroll
}
