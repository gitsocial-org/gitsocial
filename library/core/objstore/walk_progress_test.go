// walk_progress_test.go - the site index walk reports a PLAIN COUNT (never a
// percentage against the walk budget, which is a per-push cap the walk usually
// won't reach): a 6-commit branch against a 50 000 budget must not read "0%".

package objstore

import (
	"strings"
	"testing"
)

// captureWalkPhase runs fn and returns the "site index <ext>" progress lines it
// emitted, formatted the way the stderr writer would.
func captureWalkPhase(fn func(progress Progress)) []string {
	var lines []string
	fn(func(phase string, done, total int) {
		if strings.HasPrefix(phase, "site index ") {
			lines = append(lines, formatProgress(phase, done, total))
		}
	})
	return lines
}

// TestBootstrapWalk_PlainCount: a bootstrap walk over a branch much smaller than
// the budget must report a plain count, never a percentage against the budget (a
// 6-commit branch against a 50 000-ceiling budget reading "0%" is the misleading
// output this fixes).
func TestBootstrapWalk_PlainCount(t *testing.T) {
	client, _ := testClient(t)
	shas := seedChain(t, client, "", "boot", 6)
	tip := shas[len(shas)-1]

	prev := siteItemsWalkBudget
	siteItemsWalkBudget = 50000 // the real cap: far larger than the branch
	defer func() { siteItemsWalkBudget = prev }()

	var lines []string
	withTestShardCount(func() {
		lines = captureWalkPhase(func(progress Progress) {
			sp := &siteProgress{progress: progress, ext: "social"}
			if err := bootstrapItems(client, "", "social", tip, sp); err != nil {
				t.Fatalf("bootstrapItems: %v", err)
			}
		})
	})
	if len(lines) == 0 {
		t.Fatal("bootstrap emitted no walk progress")
	}
	for _, l := range lines {
		if strings.Contains(l, "%") {
			t.Errorf("bootstrap walk line %q has a percentage; the budget is a cap, so a plain count is the only honest report", l)
		}
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "site index social: 6") {
		t.Errorf("bootstrap walk final line = %q, want the plain count 6", last)
	}
}

// TestAppendGapWalk_PlainCount: the append-gap walk (unbounded by nature) reports
// a plain count with no percentage.
func TestAppendGapWalk_PlainCount(t *testing.T) {
	client, _ := testClient(t)
	// Build a base corpus, then advance the branch and append the gap.
	base := seedChain(t, client, "", "ap", 4)
	baseTip := base[len(base)-1]
	prev := siteItemsWalkBudget
	siteItemsWalkBudget = 1000
	defer func() { siteItemsWalkBudget = prev }()

	withTestShardCount(func() {
		sp := &siteProgress{progress: nil, ext: "social"}
		if err := bootstrapItems(client, "", "social", baseTip, sp); err != nil {
			t.Fatalf("bootstrap base: %v", err)
		}
		// Extend the chain past the base tip.
		ext := seedChain(t, client, baseTip, "ap-gap", 3)
		newTip := ext[len(ext)-1]
		lines := captureWalkPhase(func(progress Progress) {
			sp := &siteProgress{progress: progress, ext: "social"}
			if err := updateSiteItemsIndex(client, "", "social", newTip, sp); err != nil {
				t.Fatalf("append gap: %v", err)
			}
		})
		for _, l := range lines {
			if strings.Contains(l, "%") {
				t.Errorf("append-gap walk line %q has a percentage; the gap is unbounded (plain count only)", l)
			}
		}
	})
}
