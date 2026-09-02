// display_test.go - Content rendering verification for each TUI view
package test

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

func TestDisplay(t *testing.T) {
	f := getFixture(t)
	h := New(t, f.Workdir, f.CacheDir)

	t.Run("Timeline", func(t *testing.T) {
		h.Navigate("/social/timeline")
		out := h.Rendered()
		assertNotEmpty(t, out)
		assertContains(t, out, "Alice")
		assertContains(t, out, "Timeline")
	})
	t.Run("Search", func(t *testing.T) {
		h.Navigate("/search")
		assertNotEmpty(t, h.Rendered())
	})
	t.Run("MyRepository", func(t *testing.T) {
		assertRendersItem(t, h, tuicore.Location{Path: "/social/repository"}, f.MemoSubject, f.EditedMemoSubj)
	})
	t.Run("Board", func(t *testing.T) {
		h.Navigate("/pm/board")
		out := h.Rendered()
		assertNotEmpty(t, out)
		assertContains(t, out, "Board")
	})
	t.Run("IssuesList", func(t *testing.T) {
		h.Navigate("/pm/issues")
		out := h.Rendered()
		assertNotEmpty(t, out)
		assertContains(t, out, f.IssueSubject)
	})
	t.Run("Milestones", func(t *testing.T) {
		h.Navigate("/pm/milestones")
		out := h.Rendered()
		assertNotEmpty(t, out)
		assertContains(t, out, f.MilestoneTitle)
	})
	t.Run("Sprints", func(t *testing.T) {
		h.Navigate("/pm/sprints")
		out := h.Rendered()
		assertNotEmpty(t, out)
		assertContains(t, out, f.SprintTitle)
	})
	t.Run("PRList", func(t *testing.T) {
		h.Navigate("/review/prs")
		out := h.Rendered()
		assertNotEmpty(t, out)
		assertContains(t, out, f.PRSubject)
	})
	t.Run("ReleasesList", func(t *testing.T) {
		h.Navigate("/release/list")
		out := h.Rendered()
		assertNotEmpty(t, out)
		assertContains(t, out, f.ReleaseSubject)
	})
	t.Run("Notifications", func(t *testing.T) {
		// The fork's cross-repo edit of the workspace issue is the seeded event.
		assertRendersItem(t, h, tuicore.Location{Path: "/notifications"}, "Bob edited your item", "bob/repo")
	})
	t.Run("Memos", func(t *testing.T) {
		assertRendersItem(t, h, tuicore.Location{Path: "/memo/list"},
			f.MemoSubject, f.MemoBody, f.MemoLabel, f.EditedMemoSubj, "[project]")
	})
	t.Run("ProjectMemos", func(t *testing.T) {
		assertRendersItem(t, h, tuicore.Location{Path: "/memo/project"}, f.MemoSubject, f.EditedMemoSubj)
	})
	t.Run("MemoDetail", func(t *testing.T) {
		assertRendersItem(t, h, tuicore.LocMemoDetail(f.MemoID), f.MemoSubject, f.MemoBody, f.MemoLabel)
	})
	t.Run("MemoHistory", func(t *testing.T) {
		assertRendersItem(t, h, tuicore.LocMemoHistory(f.EditedMemoID), f.EditedMemoSubj, "2 version(s)")
	})
	t.Run("MemoInherits", func(t *testing.T) {
		assertRendersItem(t, h, tuicore.Location{Path: "/memo/inherits"}, f.InheritURL)
	})
	t.Run("Forks", func(t *testing.T) {
		// The view renders fork URLs without the scheme.
		assertRendersItem(t, h, tuicore.LocForks, "Forks (1)", strings.TrimPrefix(f.ForkURL, "https://"))
	})
	t.Run("Settings", func(t *testing.T) {
		h.Navigate("/settings")
		out := h.Rendered()
		assertNotEmpty(t, out)
		assertContains(t, out, "Settings")
	})
	t.Run("Site", func(t *testing.T) {
		h.Navigate("/config/site")
		out := h.Rendered()
		assertNotEmpty(t, out)
		assertContains(t, out, "Site")
		assertContains(t, out, "title")
		assertContains(t, out, "accent")
		assertContains(t, out, "favicon")
	})
	t.Run("Cache", func(t *testing.T) {
		h.Navigate("/cache")
		out := h.Rendered()
		assertNotEmpty(t, out)
		assertContains(t, out, "Cache")
	})
	t.Run("Help", func(t *testing.T) {
		h.Navigate("/help")
		out := h.Rendered()
		assertNotEmpty(t, out)
		assertContains(t, out, "Help")
	})
}
