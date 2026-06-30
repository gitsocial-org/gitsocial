// cursor_test.go - List cursor must survive fetch and back-navigation
package test

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/library/tui/tuisocial"
)

// selectionReader is implemented by list views that expose their focused item.
type selectionReader interface {
	SelectedDisplayItem() (tuicore.DisplayItem, bool)
}

func timelineSelection(t *testing.T, h *Harness) tuicore.DisplayItem {
	t.Helper()
	sr, ok := h.CurrentView().(selectionReader)
	if !ok {
		t.Fatalf("current view %T does not expose SelectedDisplayItem", h.CurrentView())
	}
	item, ok := sr.SelectedDisplayItem()
	if !ok {
		t.Fatal("no timeline selection (fixture needs >= 2 timeline items)")
	}
	return item
}

// A completed fetch must not move the timeline cursor. Regression: the timeline
// load message is applied twice (SetDisplayItems + the view's own handleLoaded),
// which used to reset the cursor to the top on every fetch.
func TestTimelineCursorSurvivesFetch(t *testing.T) {
	f := getFixture(t)
	h := New(t, f.Workdir, f.CacheDir)
	h.Navigate("/social/timeline")
	h.SendKey("down") // move off the top

	before := timelineSelection(t, h)
	h.sendMsg(tuisocial.FetchCompletedMsg{}) // exact path the `f` key completes through
	after := timelineSelection(t, h)

	if after.ItemID() != before.ItemID() {
		t.Fatalf("fetch reset the cursor: before=%q after=%q", before.ItemID(), after.ItemID())
	}
}

// Opening an item and pressing esc must return to the same row.
func TestTimelineCursorSurvivesBackNav(t *testing.T) {
	f := getFixture(t)
	h := New(t, f.Workdir, f.CacheDir)
	h.Navigate("/social/timeline")
	h.SendKey("down")

	before := timelineSelection(t, h)
	listPath := h.CurrentPath()

	h.SendKey("enter")
	if h.CurrentPath() == listPath {
		t.Skip("enter did not open a detail view for this fixture item")
	}
	h.SendKey("esc")
	if h.CurrentPath() != listPath {
		t.Fatalf("esc did not return to %q (got %q)", listPath, h.CurrentPath())
	}

	after := timelineSelection(t, h)
	if after.ItemID() != before.ItemID() {
		t.Fatalf("back-nav reset the cursor: before=%q after=%q", before.ItemID(), after.ItemID())
	}
}
