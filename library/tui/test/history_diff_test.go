// history_diff_test.go - Verifies history-diff views register cleanly and render
// with a non-conflicting, non-duplicated footer hint set.
package test

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestHistoryDiffFooter asserts each history-diff context registers exactly the
// expected footer entries, without duplicates and without conflicting with the
// global key set.
func TestHistoryDiffFooter(t *testing.T) {
	f := getFixture(t)
	h := New(t, f.Workdir, f.CacheDir)

	cases := []struct {
		name string
		ctx  tuicore.Context
		path string
	}{
		{"social", tuicore.HistoryDiff, "/social/history/diff"},
		{"pm.issue", tuicore.PMIssueHistoryDiff, "/pm/issue/history/diff"},
		{"pm.milestone", tuicore.PMMilestoneHistoryDiff, "/pm/milestone/history/diff"},
		{"pm.sprint", tuicore.PMSprintHistoryDiff, "/pm/sprint/history/diff"},
		{"review.pr", tuicore.ReviewPRHistoryDiff, "/review/pr/history/diff"},
	}
	wantKeys := []string{"[/]", ",/.", "</>", "e/E"}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bindings := h.BindingsForContext(c.ctx)
			gotByKey := map[string][]string{}
			for _, b := range bindings {
				gotByKey[b.Key] = append(gotByKey[b.Key], b.Label)
			}
			// New combined-key entries must each appear exactly once.
			for _, k := range wantKeys {
				if got := len(gotByKey[k]); got != 1 {
					t.Errorf("expected 1 binding for %q in %s, got %d (labels=%v)",
						k, c.ctx, got, gotByKey[k])
				}
			}
			// The view must not register the previous per-key entries that
			// either conflicted with globals or duplicated combined entries.
			for _, k := range []string{"F", "t", "T", "j/k", "h/l", "g/G", "e", "E"} {
				if _, ok := gotByKey[k]; ok {
					t.Errorf("unexpected per-key binding %q for %s", k, c.ctx)
				}
			}
			// "f" remains valid as the global fetch binding, not as our "from".
			if labels, ok := gotByKey["f"]; ok {
				if len(labels) != 1 || labels[0] != "fetch" {
					t.Errorf("f binding for %s should be only the global fetch, got %v",
						c.ctx, labels)
				}
			}
		})
	}

	t.Run("PostHistoryDiffRenders", func(t *testing.T) {
		h.NavigateTo(tuicore.LocHistoryDiff(f.PostID, "", ""))
		out := h.Rendered()
		if out == "" {
			t.Fatal("rendered output empty")
		}
		// Footer should contain at least the new key labels.
		for _, want := range []string{"[/]", "shift pair", "expand"} {
			if !strings.Contains(out, want) {
				t.Errorf("footer missing %q\n%s", want, out)
			}
		}
		// Footer must NOT show the misrouted "from -" label that was in the
		// global slot before the fix.
		if strings.Contains(out, "from -") || strings.Contains(out, "to -") {
			t.Errorf("footer still contains old label form:\n%s", out)
		}
	})
}
