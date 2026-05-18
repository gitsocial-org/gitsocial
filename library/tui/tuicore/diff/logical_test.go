// logical_test.go - P1: LogicalDiff anchor round-trip and indexing.
package diff

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// fixtureDiff builds a small two-file diff for tests.
func fixtureDiff() []git.FileDiff {
	return []git.FileDiff{
		{
			Status: git.DiffStatusModified, NewPath: "main.go",
			Hunks: []git.Hunk{{
				Header: "@@ -1,3 +1,3 @@",
				Lines: []git.DiffLine{
					{Type: git.LineContext, OldNum: 1, NewNum: 1, Content: "a"},
					{Type: git.LineRemoved, OldNum: 2, Content: "b"},
					{Type: git.LineAdded, NewNum: 2, Content: "B"},
				},
			}},
		},
		{
			Status: git.DiffStatusAdded, NewPath: "img.png", Binary: true,
		},
	}
}

func TestBuildLogical_kindsAndAnchors(t *testing.T) {
	ld := BuildLogical(fixtureDiff())
	wantKinds := []RowKind{
		RowFileHeader,
		RowHunkHeader,
		RowContext, RowRemoved, RowAdded,
		RowFileHeader, RowBinary,
	}
	if len(ld.Rows) != len(wantKinds) {
		t.Fatalf("got %d rows, want %d: %+v", len(ld.Rows), len(wantKinds), ld.Rows)
	}
	for i, r := range ld.Rows {
		if r.Kind != wantKinds[i] {
			t.Errorf("rows[%d].Kind = %v, want %v", i, r.Kind, wantKinds[i])
		}
	}
}

func TestLogicalDiff_AnchorRoundTrip(t *testing.T) {
	ld := BuildLogical(fixtureDiff())
	for i := range ld.Rows {
		anchor := ld.Anchor(i)
		got := ld.IndexOf(anchor)
		// Header rows all have (oldLine=0, newLine=0); they may not be
		// unique. Skip those for the round-trip test.
		if ld.Rows[i].Kind == RowFileHeader || ld.Rows[i].Kind == RowHunkHeader || ld.Rows[i].Kind == RowBinary {
			continue
		}
		if got != i {
			t.Errorf("row %d: anchor %+v → index %d, want %d", i, anchor, got, i)
		}
	}
}

func TestLogicalDiff_HunkBounds(t *testing.T) {
	ld := BuildLogical(fixtureDiff())
	start, end := ld.HunkBounds(0, 0)
	if start != 1 || end != 5 {
		t.Errorf("hunk bounds = [%d,%d), want [1,5)", start, end)
	}
}
