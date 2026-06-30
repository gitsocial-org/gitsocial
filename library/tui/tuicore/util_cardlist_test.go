// util_cardlist_test.go - Cursor-preservation behavior for CardList reloads
package tuicore

import (
	"os"
	"testing"
	"time"

	zone "github.com/lrstanley/bubblezone/v2"
)

// TestMain initializes the bubblezone global manager that NewCardList requires.
func TestMain(m *testing.M) {
	zone.NewGlobal()
	os.Exit(m.Run())
}

// items builds a CardList item slice with the given IDs.
func items(ids ...string) []DisplayItem {
	out := make([]DisplayItem, len(ids))
	for i, id := range ids {
		out[i] = NewItem(id, "test", "test", time.Unix(int64(i), 0), nil)
	}
	return out
}

// ReloadItems must keep the cursor on the same item (by ID) across a reload that
// reorders and inserts rows, must be idempotent (safe to apply twice for one
// load), and must fall back to the top when the selected item is gone.
func TestReloadItemsPreservesSelectionByID(t *testing.T) {
	l := NewCardList(items("a", "b", "c"))
	l.SetSelected(1) // "b"

	// Reorder + prepend: "b" moves from index 1 to index 2.
	l.ReloadItems(items("x", "a", "b", "c"))
	if got, _ := l.SelectedID(); got != "b" {
		t.Fatalf("after reload: selected = %q, want %q", got, "b")
	}
	if l.Selected() != 2 {
		t.Fatalf("after reload: index = %d, want 2", l.Selected())
	}

	// Idempotent: a second apply of the same load must not move the cursor.
	// (The timeline's load message is applied by both SetDisplayItems and the
	// view's own Update, so ReloadItems runs twice per load.)
	l.ReloadItems(items("x", "a", "b", "c"))
	if got, _ := l.SelectedID(); got != "b" {
		t.Fatalf("after second reload: selected = %q, want %q (not idempotent)", got, "b")
	}

	// Selected item removed: fall back to the top.
	l.ReloadItems(items("x", "a", "c"))
	if l.Selected() != 0 {
		t.Fatalf("after removal: index = %d, want 0", l.Selected())
	}
}

// SelectByID selects the matching item and reports whether it was found.
func TestSelectByID(t *testing.T) {
	l := NewCardList(items("a", "b", "c"))
	if !l.SelectByID("c") || l.Selected() != 2 {
		t.Fatalf("SelectByID(c): found=%v index=%d, want true/2", l.SelectByID("c"), l.Selected())
	}
	if l.SelectByID("missing") {
		t.Fatal("SelectByID(missing) = true, want false")
	}
	if l.Selected() != 2 {
		t.Fatalf("SelectByID(missing) moved cursor to %d, want it unchanged at 2", l.Selected())
	}
}

// Plain SetItems still resets selection to the top (callers that want to keep
// the cursor must use ReloadItems).
func TestSetItemsResetsSelection(t *testing.T) {
	l := NewCardList(items("a", "b", "c"))
	l.SetSelected(2)
	l.SetItems(items("a", "b", "c"))
	if l.Selected() != 0 {
		t.Fatalf("SetItems: index = %d, want 0", l.Selected())
	}
}
