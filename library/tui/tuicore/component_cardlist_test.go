// component_cardlist_test.go - Cursor-preservation behavior for CardList reloads
package tuicore

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	zone "github.com/lrstanley/bubblezone/v2"
)

// TestMain initializes the bubblezone global manager that NewCardList requires.
func TestMain(m *testing.M) {
	zone.NewGlobal()
	os.Exit(m.Run())
}

// longCardItem is a DisplayItem whose card body is a single long line.
type longCardItem struct {
	id   string
	body string
}

// ItemID returns the item's identifier.
func (i longCardItem) ItemID() string { return i.id }

// ItemType returns a test extension and type.
func (i longCardItem) ItemType() ItemType { return ItemType{Extension: "test", Type: "test"} }

// ToCard renders the item as a card whose title is its ID.
func (i longCardItem) ToCard(ItemResolver) Card {
	return Card{Header: CardHeader{Title: i.id}, Content: CardContent{Text: i.body}}
}

// Timestamp returns a fixed time.
func (i longCardItem) Timestamp() time.Time { return time.Unix(0, 0) }

// IsDimmed reports that the item is never dimmed.
func (i longCardItem) IsDimmed() bool { return false }

// longCards builds twelve items whose card body is one 360-character line.
func longCards() []DisplayItem {
	body := strings.Repeat("x", 360)
	out := make([]DisplayItem, 12)
	for i := range out {
		out[i] = longCardItem{id: fmt.Sprintf("card-%02d", i), body: body}
	}
	return out
}

// The selected card is inside the frame at every index, for each body limit.
func TestSelectedCardStaysInFrame(t *testing.T) {
	for _, maxLines := range []int{0, -1, 1} {
		t.Run(fmt.Sprintf("maxlines_%d", maxLines), func(t *testing.T) {
			cards := longCards()
			l := NewCardList(cards)
			l.SetCardOptions(CardOptions{MaxLines: maxLines, ShowStats: true, Separator: true})
			l.SetSize(80, 24)
			for i := range cards {
				l.SetSelected(i)
				title := cards[i].ItemID()
				if !strings.Contains(l.View(), title) {
					t.Fatalf("MaxLines %d, index %d: %q missing from the view", maxLines, i, title)
				}
			}
		})
	}
}

// A cursor change is visible on the next View, through SetSelected and SelectByID.
func TestCursorChangeShowsOnNextView(t *testing.T) {
	l := NewCardList(longCards())
	l.SetSize(80, 24)
	first := l.View()
	l.SetSelected(2)
	if l.View() == first {
		t.Fatal("SetSelected: the view is unchanged")
	}
	l.SetSelected(0)
	if l.View() != first {
		t.Fatal("SetSelected back to the top: the view differs")
	}
	l.SelectByID("card-05")
	if l.View() == first {
		t.Fatal("SelectByID: the view is unchanged")
	}
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
