// smoke_test.go - Smoke tests: every key on every view should not panic
package test

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

func TestSmoke(t *testing.T) {
	f := getFixture(t)
	h := New(t, f.Workdir, f.CacheDir)

	t.Run("AllKeysAllViews", func(t *testing.T) {
		for _, meta := range tuicore.AllViewMetas() {
			t.Run(meta.Path, func(t *testing.T) {
				h.Navigate(meta.Path)

				out := h.Rendered()
				assertNotEmpty(t, out)

				for _, b := range h.BindingsForContext(meta.Context) {
					t.Run("key_"+b.Key, func(t *testing.T) {
						// Reset to clean state for this view
						h.Navigate(meta.Path)

						// Send key — no panic = pass
						h.SendKey(b.Key)

						out := h.Rendered()
						assertNotEmpty(t, out)
					})
				}
			})
		}
	})
	t.Run("UnregisteredKeysIgnored", func(t *testing.T) {
		h.Navigate("/social/timeline")
		before := rendered(h)

		unregistered := []string{"z", "x", "1", "2", "!", "#", "&", "*"}
		for _, key := range unregistered {
			t.Run("key_"+key, func(t *testing.T) {
				h.SendKey(key)
				out := h.Rendered()
				assertNotEmpty(t, out)
			})
		}

		after := rendered(h)
		if before != after {
			t.Log("view changed after unregistered keys, which may indicate unexpected binding")
		}
	})
}
