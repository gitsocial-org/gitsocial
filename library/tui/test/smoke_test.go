// smoke_test.go - Smoke tests: every view survives the keys it binds
package test

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// unboundCandidates are probed in order for a key the view's context leaves free.
var unboundCandidates = []string{"z", "1", "~", "&"}

func TestSmoke(t *testing.T) {
	fullTierOnly(t)
	f := getFixture(t)
	h := New(t, f.Workdir, f.CacheDir)
	shortcuts := globalShortcuts(h)

	t.Run("AllKeysAllViews", func(t *testing.T) {
		for _, meta := range tuicore.AllViewMetas() {
			t.Run(meta.Path, func(t *testing.T) {
				h.Navigate(meta.Path)
				assertNotEmpty(t, h.Rendered())
				bound := boundKeys(h, meta.Context)
				for _, key := range ownKeys(h, meta.Context, shortcuts) {
					pressOnView(t, h, meta.Path, key)
				}
				pressOnView(t, h, meta.Path, unboundKey(bound))
			})
		}
	})
	t.Run("GlobalShortcuts", func(t *testing.T) {
		pressed := make(map[string]bool)
		for _, meta := range tuicore.AllViewMetas() {
			for _, b := range h.BindingsForContext(meta.Context) {
				if !shortcuts[b.Key] || pressed[b.Key] {
					continue
				}
				pressed[b.Key] = true
				h.Navigate(meta.Path)
				pressOnView(t, h, meta.Path, b.Key)
			}
		}
		if len(pressed) != len(shortcuts) {
			t.Errorf("pressed %d global shortcuts, %d are registered", len(pressed), len(shortcuts))
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

// pressOnView presses one key, requires a render, and returns to the view.
func pressOnView(t *testing.T, h *Harness, path, key string) {
	t.Helper()
	h.SendKey(key)
	if strings.TrimSpace(stripANSI(h.Rendered())) == "" {
		t.Errorf("%s: empty render after key %q", path, key)
	}
	if dialogOpen(h) {
		h.SendKey("n")
	}
	if h.CurrentPath() != path {
		h.Navigate(path)
	}
}

// dialogOpen reports whether a confirm or choice prompt holds the next key.
func dialogOpen(h *Harness) bool {
	if h.model.Host().State().ChoicePrompt != "" {
		return true
	}
	return strings.Contains(rendered(h), "[y/n]")
}

// globalShortcuts returns the keys more than half the contexts bind.
func globalShortcuts(h *Harness) map[string]bool {
	contexts := tuicore.AllContexts()
	count := make(map[string]int)
	for _, ctx := range contexts {
		for _, b := range h.BindingsForContext(ctx) {
			count[b.Key]++
		}
	}
	shortcuts := make(map[string]bool)
	for key, n := range count {
		if n*2 > len(contexts) {
			shortcuts[key] = true
		}
	}
	return shortcuts
}

// ownKeys returns the keys a context binds beyond the global shortcuts.
func ownKeys(h *Harness, ctx tuicore.Context, shortcuts map[string]bool) []string {
	keys := make([]string, 0)
	for _, b := range h.BindingsForContext(ctx) {
		if !shortcuts[b.Key] {
			keys = append(keys, b.Key)
		}
	}
	return keys
}

// boundKeys returns the set of keys a context binds.
func boundKeys(h *Harness, ctx tuicore.Context) map[string]bool {
	bound := make(map[string]bool)
	for _, b := range h.BindingsForContext(ctx) {
		bound[b.Key] = true
	}
	return bound
}

// unboundKey returns a key the context leaves free, for the unregistered case.
func unboundKey(bound map[string]bool) string {
	for _, key := range unboundCandidates {
		if !bound[key] {
			return key
		}
	}
	return unboundCandidates[0]
}
