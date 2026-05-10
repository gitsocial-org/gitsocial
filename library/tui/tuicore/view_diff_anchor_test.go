// view_diff_anchor_test.go - Tests for row-anchor lookup and hunk-reveal
// scroll math against the cell-model row model.
package tuicore

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/tui/tuicore/diff"
)

func mkRow(file, hunk, old, new int) diff.DisplayRow {
	return diff.DisplayRow{Row: diff.Row{Anchor: diff.RowAnchor{
		FileIdx: file, HunkIdx: hunk, OldLine: old, NewLine: new,
	}}}
}

// TestFindRowByAnchor_exact verifies an exact line-number match wins.
func TestFindRowByAnchor_exact(t *testing.T) {
	rows := []diff.DisplayRow{
		mkRow(0, -1, 0, 0),
		mkRow(0, 0, 0, 0),
		mkRow(0, 0, 10, 10),
		mkRow(0, 0, 11, 0),
		mkRow(0, 0, 0, 11),
		mkRow(0, 0, 12, 12),
	}
	idx := findRowByAnchor(rows, diff.RowAnchor{FileIdx: 0, HunkIdx: 0, OldLine: 11})
	if idx != 3 {
		t.Errorf("got %d, want 3 (exact match on removed line)", idx)
	}
}

// TestFindRowByAnchor_tagIgnored verifies Tag mismatch falls through to
// the equality check (Tag dropped on both sides).
func TestFindRowByAnchor_tagIgnored(t *testing.T) {
	rows := []diff.DisplayRow{
		mkRow(0, 0, 10, 10),
		mkRow(0, 0, 11, 11),
	}
	rows[1].Anchor.Tag = "feedback"
	idx := findRowByAnchor(rows, diff.RowAnchor{FileIdx: 0, HunkIdx: 0, OldLine: 11, NewLine: 11, Tag: ""})
	if idx != 1 {
		t.Errorf("got %d, want 1 (tag ignored)", idx)
	}
}

// TestFindRowByAnchor_headerFallback verifies hunk-header anchors match
// the first row of the same (fileIdx, hunkIdx).
func TestFindRowByAnchor_headerFallback(t *testing.T) {
	rows := []diff.DisplayRow{
		mkRow(0, 0, 0, 0),
		mkRow(0, 0, 1, 1),
		mkRow(1, 0, 0, 0),
		mkRow(1, 0, 1, 1),
	}
	idx := findRowByAnchor(rows, diff.RowAnchor{FileIdx: 1, HunkIdx: 0})
	if idx != 2 {
		t.Errorf("got %d, want 2 (header anchor for file 1)", idx)
	}
}

// TestFindRowByAnchor_noMatch verifies -1 when nothing matches.
func TestFindRowByAnchor_noMatch(t *testing.T) {
	rows := []diff.DisplayRow{mkRow(0, 0, 1, 1)}
	idx := findRowByAnchor(rows, diff.RowAnchor{FileIdx: 99, HunkIdx: 99, OldLine: 50, NewLine: 50})
	if idx != -1 {
		t.Errorf("got %d, want -1", idx)
	}
}

// TestHunkRevealScroll_smallHunkTopPadded asserts a small hunk is positioned
// with ~25% top padding when there's room.
func TestHunkRevealScroll_smallHunkTopPadded(t *testing.T) {
	got := HunkRevealScroll(100, 5, 40, 200)
	want := 100 - 40/4 // = 90
	if got != want {
		t.Errorf("HunkRevealScroll small-hunk = %d, want %d", got, want)
	}
}

// TestHunkRevealScroll_smallHunkBottomKept asserts that when applying the
// preferred top padding would clip the hunk's bottom, scroll is bumped down
// so the entire hunk fits.
func TestHunkRevealScroll_smallHunkBottomKept(t *testing.T) {
	got := HunkRevealScroll(100, 30, 40, 200)
	if got != 90 {
		t.Errorf("got %d, want 90 (= target − pad)", got)
	}
	got = HunkRevealScroll(100, 35, 40, 200)
	if got != 95 {
		t.Errorf("got %d, want 95 (bottom-visible bump)", got)
	}
}

// TestHunkRevealScroll_tallHunk asserts hunks taller than viewport align top.
func TestHunkRevealScroll_tallHunk(t *testing.T) {
	got := HunkRevealScroll(100, 200, 40, 500)
	if got != 90 {
		t.Errorf("got %d, want 90 (= target − pad, ignore bottom for tall hunk)", got)
	}
}

// TestHunkRevealScroll_atDocStart asserts scroll never goes negative.
func TestHunkRevealScroll_atDocStart(t *testing.T) {
	got := HunkRevealScroll(2, 5, 40, 200)
	if got != 0 {
		t.Errorf("got %d, want 0 (clamped to doc start)", got)
	}
}

// TestHunkRevealScroll_atDocEnd asserts scroll is clamped to keep the
// viewport in bounds.
func TestHunkRevealScroll_atDocEnd(t *testing.T) {
	got := HunkRevealScroll(195, 5, 40, 200)
	want := 200 - 40 // = 160
	if got != want {
		t.Errorf("got %d, want %d (clamped to doc end)", got, want)
	}
}
