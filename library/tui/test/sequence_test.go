// sequence_test.go - Multi-step user interaction flows
package test

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

func TestSequence(t *testing.T) {
	f := getFixture(t)
	h := New(t, f.Workdir, f.CacheDir)

	t.Run("AllExtensions", func(t *testing.T) {
		steps := []struct {
			key  string
			path string
		}{
			{"T", "/social/timeline"},
			{"B", "/pm/board"},
			{"P", "/review/prs"},
			{"R", "/release/list"},
			{"T", "/social/timeline"},
		}
		for _, step := range steps {
			h.SendKey(step.key)
			if got := h.CurrentPath(); got != step.path {
				t.Errorf("after %q: path = %q, want %q", step.key, got, step.path)
			}
			assertNotEmpty(t, h.Rendered())
		}
	})
	t.Run("BrowseAndReturn", func(t *testing.T) {
		h.Navigate("/social/timeline")
		timelineOut := rendered(h)
		h.SendKey("enter")
		h.SendKey("esc")
		if h.CurrentPath() != "/social/timeline" {
			t.Errorf("after enter+esc: path = %q, want /social/timeline", h.CurrentPath())
		}
		afterOut := rendered(h)
		if timelineOut != afterOut {
			t.Log("timeline content changed after browse — acceptable if data refreshed")
		}
	})
	t.Run("IssuesFlow", func(t *testing.T) {
		h.SendKey("B")
		assertNotEmpty(t, h.Rendered())
		h.Navigate("/pm/issues")
		assertContains(t, h.Rendered(), f.IssueSubject)
		h.SendKey("enter")
		h.SendKey("esc")
		assertNotEmpty(t, h.Rendered())
	})
	t.Run("SettingsAndBack", func(t *testing.T) {
		h.Navigate("/social/timeline")
		h.Navigate("/settings")
		assertContains(t, h.Rendered(), "Settings")
		h.Navigate("/cache")
		assertContains(t, h.Rendered(), "Cache")
		h.SendKey("esc")
		if h.CurrentPath() != "/settings" {
			t.Errorf("expected /settings after esc, got %q", h.CurrentPath())
		}
	})
	t.Run("QuickJumpOverridesHistory", func(t *testing.T) {
		h.Navigate("/social/timeline")
		h.Navigate("/pm/issues")
		h.Navigate("/pm/board")
		h.SendKey("R")
		if h.CurrentPath() != "/release/list" {
			t.Errorf("after R: path = %q, want /release/list", h.CurrentPath())
		}
		assertNotEmpty(t, h.Rendered())
	})
	t.Run("PostEditTriggersEditor", func(t *testing.T) {
		h.NavigateTo(tuicore.LocDetail(f.PostID))
		if h.CurrentPath() != "/social/detail" {
			t.Fatalf("expected /social/detail, got %q", h.CurrentPath())
		}
		assertContains(t, h.Rendered(), f.EditedContent)
		before := h.SkippedExecN
		h.SendKey("e")
		if h.SkippedExecN <= before {
			t.Error("pressing 'e' on post detail did not trigger editor (execMsg not produced)")
		}
	})
	t.Run("PostCommentTriggersEditor", func(t *testing.T) {
		h.NavigateTo(tuicore.LocDetail(f.PostID))
		before := h.SkippedExecN
		h.SendKey("c")
		if h.SkippedExecN <= before {
			t.Error("pressing 'c' on post detail did not trigger editor")
		}
	})
	t.Run("PostRepostTriggersEditor", func(t *testing.T) {
		h.NavigateTo(tuicore.LocDetail(f.PostID))
		before := h.SkippedExecN
		h.SendKey("y")
		if h.SkippedExecN <= before {
			t.Error("pressing 'y' on post detail did not trigger editor")
		}
	})
	t.Run("PostRetractShowsConfirm", func(t *testing.T) {
		h.NavigateTo(tuicore.LocDetail(f.PostID))
		h.SendKey("X")
		assertContains(t, h.Rendered(), "[y/n]")
		h.SendKey("n") // dismiss
	})
	t.Run("PostHistoryNavigates", func(t *testing.T) {
		h.NavigateTo(tuicore.LocDetail(f.PostID))
		h.SendKey("h")
		if h.CurrentPath() != "/social/history" {
			t.Errorf("pressing 'h' on edited post: path = %q, want /social/history", h.CurrentPath())
		}
	})
	t.Run("SearchFlow", func(t *testing.T) {
		h.Navigate("/search")
		if h.CurrentPath() != "/search" {
			t.Fatalf("expected /search, got %q", h.CurrentPath())
		}
		// Type query and submit
		h.SendKeys("f", "i", "x", "t", "u", "r", "e")
		h.SendKey("enter")
		out := rendered(h)
		// Search should return results containing fixture posts
		if out == "" {
			t.Error("search rendered empty after query")
		}
	})
	t.Run("PRDiffNavigates", func(t *testing.T) {
		h.NavigateTo(tuicore.LocReviewPRDetail(f.PRID))
		h.SendKey("d")
		if h.CurrentPath() != "/review/diff" {
			t.Errorf("pressing 'd' on PR detail: path = %q, want /review/diff", h.CurrentPath())
		}
	})
	t.Run("IssueEditNavigates", func(t *testing.T) {
		h.NavigateTo(tuicore.LocPMIssueDetail(f.IssueID))
		if h.CurrentPath() != "/pm/issue" {
			t.Fatalf("expected /pm/issue, got %q", h.CurrentPath())
		}
		h.SendKey("e")
		if h.CurrentPath() != "/pm/edit-issue" {
			t.Errorf("pressing 'e' on issue detail: path = %q, want /pm/edit-issue", h.CurrentPath())
		}
	})
	t.Run("IssueCommentTriggersEditor", func(t *testing.T) {
		h.NavigateTo(tuicore.LocPMIssueDetail(f.IssueID))
		before := h.SkippedExecN
		h.SendKey("c")
		if h.SkippedExecN <= before {
			t.Error("pressing 'c' on issue detail did not trigger editor")
		}
	})
	t.Run("MilestoneEditNavigates", func(t *testing.T) {
		h.NavigateTo(tuicore.LocPMMilestoneDetail(f.MilestoneID))
		if h.CurrentPath() != "/pm/milestone" {
			t.Fatalf("expected /pm/milestone, got %q", h.CurrentPath())
		}
		h.SendKey("e")
		if h.CurrentPath() != "/pm/edit-milestone" {
			t.Errorf("pressing 'e' on milestone detail: path = %q, want /pm/edit-milestone", h.CurrentPath())
		}
	})
	t.Run("MilestoneCommentTriggersEditor", func(t *testing.T) {
		h.NavigateTo(tuicore.LocPMMilestoneDetail(f.MilestoneID))
		before := h.SkippedExecN
		h.SendKey("c")
		if h.SkippedExecN <= before {
			t.Error("pressing 'c' on milestone detail did not trigger editor")
		}
	})
	t.Run("SprintEditNavigates", func(t *testing.T) {
		h.NavigateTo(tuicore.LocPMSprintDetail(f.SprintID))
		if h.CurrentPath() != "/pm/sprint" {
			t.Fatalf("expected /pm/sprint, got %q", h.CurrentPath())
		}
		h.SendKey("e")
		if h.CurrentPath() != "/pm/edit-sprint" {
			t.Errorf("pressing 'e' on sprint detail: path = %q, want /pm/edit-sprint", h.CurrentPath())
		}
	})
	t.Run("SprintCommentTriggersEditor", func(t *testing.T) {
		h.NavigateTo(tuicore.LocPMSprintDetail(f.SprintID))
		before := h.SkippedExecN
		h.SendKey("c")
		if h.SkippedExecN <= before {
			t.Error("pressing 'c' on sprint detail did not trigger editor")
		}
	})
	t.Run("ReleaseEditNavigates", func(t *testing.T) {
		h.NavigateTo(tuicore.LocReleaseDetail(f.ReleaseID))
		if h.CurrentPath() != "/release/detail" {
			t.Fatalf("expected /release/detail, got %q", h.CurrentPath())
		}
		h.SendKey("e")
		if h.CurrentPath() != "/release/edit" {
			t.Errorf("pressing 'e' on release detail: path = %q, want /release/edit", h.CurrentPath())
		}
	})
	t.Run("ReleaseCommentTriggersEditor", func(t *testing.T) {
		h.NavigateTo(tuicore.LocReleaseDetail(f.ReleaseID))
		before := h.SkippedExecN
		h.SendKey("c")
		if h.SkippedExecN <= before {
			t.Error("pressing 'c' on release detail did not trigger editor")
		}
	})
	t.Run("PREditNavigates", func(t *testing.T) {
		h.NavigateTo(tuicore.LocReviewPRDetail(f.PRID))
		if h.CurrentPath() != "/review/pr" {
			t.Fatalf("expected /review/pr, got %q", h.CurrentPath())
		}
		h.SendKey("e")
		if h.CurrentPath() != "/review/edit-pr" {
			t.Errorf("pressing 'e' on PR detail: path = %q, want /review/edit-pr", h.CurrentPath())
		}
	})
	t.Run("PRCommentTriggersEditor", func(t *testing.T) {
		h.NavigateTo(tuicore.LocReviewPRDetail(f.PRID))
		before := h.SkippedExecN
		h.SendKey("c")
		if h.SkippedExecN <= before {
			t.Error("pressing 'c' on PR detail did not trigger editor")
		}
	})
	t.Run("MultipleViewRenders", func(t *testing.T) {
		views := []string{
			"/social/timeline", "/pm/board", "/pm/issues",
			"/pm/milestones", "/pm/sprints", "/review/prs",
			"/release/list", "/settings", "/cache", "/help",
		}
		for _, path := range views {
			t.Run(path, func(t *testing.T) {
				h.Navigate(path)
				assertNotEmpty(t, h.Rendered())
			})
		}
	})
}
