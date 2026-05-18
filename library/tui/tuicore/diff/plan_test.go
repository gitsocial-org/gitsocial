// plan_test.go - P1: BuildPlan correctness.
package diff

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

func planTestPalette() Palette {
	return Palette{
		AddedFG:       "#4ae04a",
		RemovedFG:     "#e06c75",
		AddedBG:       "#1a3524",
		RemovedBG:     "#3b1a1e",
		HunkHeaderFG:  "36",
		LineNumFG:     "240",
		TextSecondary: "242",
	}
}

func planPlainHighlight(line, _ string) []Cell {
	return []Cell{{Text: line}}
}

func TestBuildPlan_unifiedAllVisible(t *testing.T) {
	ld := BuildLogical(fixtureDiff())
	state := ViewState{Layout: LayoutUnified}
	plan := BuildPlan(ld, state, planTestPalette(), planPlainHighlight)
	// Same length as logical rows (no folds, all visible).
	if len(plan.Rows) != len(ld.Rows) {
		t.Errorf("plan has %d rows, want %d", len(plan.Rows), len(ld.Rows))
	}
}

func TestBuildPlan_foldedFileHidesBody(t *testing.T) {
	ld := BuildLogical(fixtureDiff())
	// Fold file 0: hide everything from its header to file 1's header.
	state := ViewState{
		Layout: LayoutUnified,
		Folds: []FoldRegion{{
			Start: ld.Anchor(0), // file header
			End:   ld.Anchor(5), // next file header
			Kind:  FoldFile,
		}},
	}
	plan := BuildPlan(ld, state, planTestPalette(), planPlainHighlight)
	// Expect: file 0 header (placeholder), then file 1 entries.
	wantLen := 1 + 2 // file 0 placeholder + file 1 header + file 1 binary
	if len(plan.Rows) != wantLen {
		t.Errorf("got %d plan rows, want %d (%+v)", len(plan.Rows), wantLen, plan.Rows)
	}
	if plan.Rows[0].Kind != RowCollapsedContext {
		t.Errorf("first row should be placeholder, got %v", plan.Rows[0].Kind)
	}
}

func TestBuildPlan_addedRowCarriesBG(t *testing.T) {
	ld := BuildLogical(fixtureDiff())
	state := ViewState{Layout: LayoutUnified}
	plan := BuildPlan(ld, state, planTestPalette(), planPlainHighlight)
	// Row 4 in fixture is the RowAdded ("B").
	found := false
	for _, r := range plan.Rows {
		if r.Kind == RowAdded {
			found = true
			if r.LineBG != planTestPalette().AddedBG {
				t.Errorf("added row LineBG = %q, want %q", r.LineBG, planTestPalette().AddedBG)
			}
		}
	}
	if !found {
		t.Errorf("no added row in plan")
	}
}

func TestBuildPlan_renderWidthInvariant(t *testing.T) {
	ld := BuildLogical(fixtureDiff())
	state := ViewState{Layout: LayoutUnified}
	plan := BuildPlan(ld, state, planTestPalette(), planPlainHighlight)
	const cols = 80
	for i, r := range plan.Rows {
		clipped := SliceRow(r.Row, 0, cols)
		out := RenderRow(clipped, cols)
		// stripAnsi width check via the test helper in build_test.go.
		plain := stripAnsiPlan(out)
		if got := stringWidth(plain); got != cols {
			t.Errorf("row %d rendered width %d, want %d", i, got, cols)
		}
	}
}

func stripAnsiPlan(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestBuildPlan_unifiedAndSplitProduceSameAnchors(t *testing.T) {
	// Anchors should survive layout changes; mapping a cursor across `v`
	// should land on the same row identity.
	files := []git.FileDiff{{
		Status: git.DiffStatusModified, NewPath: "x.go",
		Hunks: []git.Hunk{{
			Lines: []git.DiffLine{
				{Type: git.LineContext, OldNum: 1, NewNum: 1, Content: "a"},
				{Type: git.LineRemoved, OldNum: 2, Content: "b"},
				{Type: git.LineAdded, NewNum: 2, Content: "B"},
				{Type: git.LineContext, OldNum: 3, NewNum: 3, Content: "c"},
			},
		}},
	}}
	ld := BuildLogical(files)
	uni := BuildPlan(ld, ViewState{Layout: LayoutUnified}, planTestPalette(), planPlainHighlight)
	spl := BuildPlan(ld, ViewState{Layout: LayoutSplit}, planTestPalette(), planPlainHighlight)
	// Both plans should reach every (FileIdx, HunkIdx, OldLine, NewLine)
	// anchor at least once. In split mode, paired removed/added rows have
	// their anchors on the Left/Right halves rather than the outer row.
	collect := func(p DisplayPlan) map[RowAnchor]bool {
		out := map[RowAnchor]bool{}
		bare := func(a RowAnchor) RowAnchor {
			a.Tag = ""
			return a
		}
		for _, r := range p.Rows {
			out[bare(r.Anchor)] = true
			if r.Left != nil {
				out[bare(r.Left.Anchor)] = true
			}
			if r.Right != nil {
				out[bare(r.Right.Anchor)] = true
			}
		}
		return out
	}
	uniSet := collect(uni)
	splSet := collect(spl)
	// All non-header anchors in unified should be present in split.
	for a := range uniSet {
		if a.OldLine == 0 && a.NewLine == 0 {
			continue
		}
		if !splSet[a] {
			t.Errorf("anchor %+v present in unified but not in split", a)
		}
	}
}
