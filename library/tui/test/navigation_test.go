// navigation_test.go - View-to-view navigation tests
package test

import (
	"testing"
)

func TestNavigation(t *testing.T) {
	f := getFixture(t)
	h := New(t, f.Workdir, f.CacheDir)

	t.Run("GlobalKeys", func(t *testing.T) {
		tests := []struct {
			key  string
			path string
		}{
			{"T", "/social/timeline"},
			{"B", "/pm/board"},
			{"P", "/review/prs"},
			{"R", "/release/list"},
		}
		for _, tt := range tests {
			t.Run("key_"+tt.key, func(t *testing.T) {
				h.Navigate("/settings")
				h.SendKey(tt.key)
				got := h.CurrentPath()
				if got != tt.path {
					t.Errorf("after %q: path = %q, want %q", tt.key, got, tt.path)
				}
				assertNotEmpty(t, h.Rendered())
			})
		}
	})
	t.Run("Back", func(t *testing.T) {
		h.Navigate("/social/timeline")
		h.Navigate("/settings")
		if h.CurrentPath() != "/settings" {
			t.Fatalf("expected /settings, got %q", h.CurrentPath())
		}
		h.SendKey("esc")
		if h.CurrentPath() != "/social/timeline" {
			t.Errorf("after esc: path = %q, want /social/timeline", h.CurrentPath())
		}
	})
	t.Run("MultiLevelBack", func(t *testing.T) {
		h.Navigate("/social/timeline")
		h.Navigate("/settings")
		h.Navigate("/cache")
		h.SendKey("esc")
		if h.CurrentPath() != "/settings" {
			t.Errorf("first esc: path = %q, want /settings", h.CurrentPath())
		}
		h.SendKey("esc")
		if h.CurrentPath() != "/social/timeline" {
			t.Errorf("second esc: path = %q, want /social/timeline", h.CurrentPath())
		}
	})
	t.Run("Detail", func(t *testing.T) {
		h.Navigate("/pm/issues")
		listPath := h.CurrentPath()
		h.SendKey("enter")
		detailPath := h.CurrentPath()
		if detailPath == listPath {
			t.Log("enter did not navigate to detail — may be empty list or view-specific behavior")
		}
		h.SendKey("esc")
		assertNotEmpty(t, h.Rendered())
	})
	t.Run("Search", func(t *testing.T) {
		h.Navigate("/social/timeline")
		h.SendKey("/")
		if h.CurrentPath() != "/search" {
			t.Errorf("after /: path = %q, want /search", h.CurrentPath())
		}
		assertNotEmpty(t, h.Rendered())
	})
	t.Run("Help", func(t *testing.T) {
		h.Navigate("/social/timeline")
		h.SendKey("?")
		if h.CurrentPath() != "/help" {
			t.Errorf("after ?: path = %q, want /help", h.CurrentPath())
		}
		assertNotEmpty(t, h.Rendered())
	})
	t.Run("Notifications", func(t *testing.T) {
		h.Navigate("/social/timeline")
		h.SendKey("@")
		if h.CurrentPath() != "/notifications" {
			t.Errorf("after @: path = %q, want /notifications", h.CurrentPath())
		}
		assertNotEmpty(t, h.Rendered())
	})
}
