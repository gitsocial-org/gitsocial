// stack_test.go - Stacked PR visualization and navigation tests
package test

import (
	"strings"
	"sync"
	"testing"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

var stackSetupOnce sync.Once
var stackChildID string

// setupStack creates a stacked PR on top of the fixture's existing PR if not already created.
// Idempotent: safe to call across multiple tests in the same package run.
func setupStack(t *testing.T, f *Fixture) string {
	stackSetupOnce.Do(func() {
		// Create the stacked branch so CreatePR's tip resolution can find it.
		if _, err := git.ExecGit(f.Workdir, []string{"branch", "dark-mode-ui", "dark-mode"}); err != nil {
			t.Fatalf("git branch dark-mode-ui: %v", err)
		}
		res := review.CreatePR(f.Workdir, "Stacked: add dark mode toggle UI", "Adds the toggle UI layer on top of dark mode support.", review.CreatePROptions{
			Base:      "dark-mode",
			Head:      "dark-mode-ui",
			DependsOn: []string{f.PRID},
		})
		if !res.Success {
			t.Fatalf("CreatePR for stack child failed: %s", res.Error.Message)
		}
		stackChildID = res.Data.ID
	})
	return stackChildID
}

func TestStackDisplay(t *testing.T) {
	f := getFixture(t)
	setupStack(t, f)
	h := New(t, f.Workdir, f.CacheDir)

	t.Run("BadgeOnPRList", func(t *testing.T) {
		h.Navigate("/review/prs")
		out := rendered(h)
		assertNotEmpty(t, out)
		if !strings.Contains(out, "stacked") {
			t.Errorf("expected PR list to show \"stacked\" badge, full output:\n%s", out)
		}
	})
}

func TestStackBindings(t *testing.T) {
	f := getFixture(t)
	h := New(t, f.Workdir, f.CacheDir)

	bindings := h.BindingsForContext(tuicore.ReviewPRDetail)
	wantKeys := map[string]bool{"[": false, "]": false}
	for _, b := range bindings {
		if _, ok := wantKeys[b.Key]; ok {
			wantKeys[b.Key] = true
		}
	}
	for key, found := range wantKeys {
		if !found {
			t.Errorf("expected %q binding to be registered on ReviewPRDetail context", key)
		}
	}
	// Also ensure Registration doesn't panic for the stacked PR
	_ = f
}

// TestStackNavigationBackend verifies the underlying navigation functions work.
// Full TUI rendering of the stack section is covered by the review extension's
// GetStack tests — the TUI harness's 50ms execCmd timeout makes async detail
// view rendering flaky to assert on.
func TestStackNavigationBackend(t *testing.T) {
	f := getFixture(t)
	childID := setupStack(t, f)

	stackRes := review.GetStack(childID)
	if !stackRes.Success {
		t.Fatalf("GetStack(child) failed: %s", stackRes.Error.Message)
	}
	if len(stackRes.Data) != 2 {
		t.Errorf("expected stack of 2 PRs, got %d", len(stackRes.Data))
	}
	// Root should be the fixture PR (the parent)
	if stackRes.Data[0].PullRequest.ID != f.PRID {
		t.Errorf("expected root to be fixture PRID, got %q", stackRes.Data[0].PullRequest.ID)
	}
	// Top should be the stacked child
	if stackRes.Data[1].PullRequest.ID != childID {
		t.Errorf("expected top to be child PR, got %q", stackRes.Data[1].PullRequest.ID)
	}
}
