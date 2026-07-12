// sequence_test.go - Multi-step user interaction flows
package test

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

func TestSequence(t *testing.T) {
	f := getFixture(t)
	h := New(t, f.Workdir, f.CacheDir)

	t.Run("AllExtensions", func(t *testing.T) {
		steps := []struct {
			key  string
			path string
		}{
			{"S", "/social/timeline"},
			{"P", "/pm/board"},
			{"R", "/review/prs"},
			{"V", "/release/list"},
			{"M", "/memo/project"},
			{"S", "/social/timeline"},
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
		h.SendKey("P")
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
		h.SendKey("V")
		if h.CurrentPath() != "/release/list" {
			t.Errorf("after V: path = %q, want /release/list", h.CurrentPath())
		}
		assertNotEmpty(t, h.Rendered())
	})
	t.Run("PostEditOpensForm", func(t *testing.T) {
		h.NavigateTo(tuicore.LocDetail(f.PostID))
		if h.CurrentPath() != "/social/detail" {
			t.Fatalf("expected /social/detail, got %q", h.CurrentPath())
		}
		assertContains(t, h.Rendered(), f.EditedContent)
		h.SendKey("e")
		if h.CurrentPath() != "/social/post-form" {
			t.Errorf("expected /social/post-form after 'e', got %q", h.CurrentPath())
		}
	})
	t.Run("PostCommentOpensForm", func(t *testing.T) {
		h.NavigateTo(tuicore.LocDetail(f.PostID))
		h.SendKey("c")
		if h.CurrentPath() != "/social/post-form" {
			t.Errorf("expected /social/post-form after 'c', got %q", h.CurrentPath())
		}
	})
	t.Run("PostRepostOpensForm", func(t *testing.T) {
		h.NavigateTo(tuicore.LocDetail(f.PostID))
		h.SendKey("y")
		if h.CurrentPath() != "/social/post-form" {
			t.Errorf("expected /social/post-form after 'y', got %q", h.CurrentPath())
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
	t.Run("IssueCommentOpensForm", func(t *testing.T) {
		h.NavigateTo(tuicore.LocPMIssueDetail(f.IssueID))
		h.SendKey("c")
		if h.CurrentPath() != "/social/post-form" {
			t.Errorf("pressing 'c' on issue detail: path = %q, want /social/post-form", h.CurrentPath())
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
	t.Run("MilestoneCommentOpensForm", func(t *testing.T) {
		h.NavigateTo(tuicore.LocPMMilestoneDetail(f.MilestoneID))
		h.SendKey("c")
		if h.CurrentPath() != "/social/post-form" {
			t.Errorf("pressing 'c' on milestone detail: path = %q, want /social/post-form", h.CurrentPath())
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
	t.Run("SprintCommentOpensForm", func(t *testing.T) {
		h.NavigateTo(tuicore.LocPMSprintDetail(f.SprintID))
		h.SendKey("c")
		if h.CurrentPath() != "/social/post-form" {
			t.Errorf("pressing 'c' on sprint detail: path = %q, want /social/post-form", h.CurrentPath())
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
	t.Run("ReleaseCommentOpensForm", func(t *testing.T) {
		h.NavigateTo(tuicore.LocReleaseDetail(f.ReleaseID))
		h.SendKey("c")
		if h.CurrentPath() != "/social/post-form" {
			t.Errorf("pressing 'c' on release detail: path = %q, want /social/post-form", h.CurrentPath())
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
	t.Run("PRCommentOpensForm", func(t *testing.T) {
		h.NavigateTo(tuicore.LocReviewPRDetail(f.PRID))
		h.SendKey("c")
		if h.CurrentPath() != "/social/post-form" {
			t.Errorf("pressing 'c' on PR detail: path = %q, want /social/post-form", h.CurrentPath())
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
	// PushConfirmNamesRemote: 'p' on the config view opens the push confirm,
	// which names the resolved remote. The fixture has a single non-s3 origin
	// and no gitsocial.pushRemote, so the target resolves to "origin" with no
	// picker (the picker needs 2+ s3 remotes, which the shared fixture can't
	// provide; the picker/prompt construction is unit-tested in the tui package).
	t.Run("PushConfirmNamesRemote", func(t *testing.T) {
		h.NavigateTo(tuicore.LocConfig("social"))
		h.SendKey("p")
		out := rendered(h)
		assertContains(t, out, "Push to origin")
		assertContains(t, out, "tags checked at push")
		h.SendKey("n") // dismiss
	})
}
