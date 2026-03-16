// display_test.go - Content rendering verification for each TUI view
package test

import (
	"testing"
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
		h.Navigate("/social/repository")
		assertNotEmpty(t, h.Rendered())
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
		h.Navigate("/notifications")
		assertNotEmpty(t, h.Rendered())
	})
	t.Run("Settings", func(t *testing.T) {
		h.Navigate("/settings")
		out := h.Rendered()
		assertNotEmpty(t, out)
		assertContains(t, out, "Settings")
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
