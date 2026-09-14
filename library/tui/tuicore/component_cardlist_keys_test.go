// component_cardlist_keys_test.go - CardList cursor, offset and link focus under every key and mouse action
package tuicore

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
)

// linkedCardItem is a DisplayItem whose card carries a title link and a subtitle link.
type linkedCardItem struct {
	id string
}

// ItemID returns the item's identifier.
func (i linkedCardItem) ItemID() string { return i.id }

// ItemType returns a test extension and type.
func (i linkedCardItem) ItemType() ItemType { return ItemType{Extension: "test", Type: "test"} }

// ToCard renders the item as a card with two links.
func (i linkedCardItem) ToCard(ItemResolver) Card {
	return Card{
		Header: CardHeader{
			Title:     i.id,
			TitleLink: &Location{Path: "/title/" + i.id},
			Subtitle:  []HeaderPart{{Text: "by " + i.id, Link: &Location{Path: "/author/" + i.id}}},
		},
		Content: CardContent{Text: "body of " + i.id},
	}
}

// Timestamp returns a fixed time.
func (i linkedCardItem) Timestamp() time.Time { return time.Unix(0, 0) }

// IsDimmed reports that the item is never dimmed.
func (i linkedCardItem) IsDimmed() bool { return false }

// linkedCards builds count items whose cards each carry two links.
func linkedCards(count int) []DisplayItem {
	out := make([]DisplayItem, count)
	for i := range out {
		out[i] = linkedCardItem{id: fmt.Sprintf("card-%02d", i)}
	}
	return out
}

// cardListHeight is the viewport height every CardList key test renders at.
const cardListHeight = 10

// newKeyCardList builds a 12-item card list of uniform card height.
func newKeyCardList(t *testing.T) *CardList {
	t.Helper()
	l := NewCardList(linkedCards(12))
	l.SetCardOptions(CardOptions{MaxLines: 1, ShowStats: false, Separator: false})
	l.SetSize(80, cardListHeight)
	h := l.itemHeight(0)
	for i := range l.items {
		if l.itemHeight(i) != h {
			t.Fatalf("card %d height = %d, want the uniform %d", i, l.itemHeight(i), h)
		}
	}
	return l
}

// keyMsg builds a KeyPressMsg for a key name the list handles.
func keyMsg(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
}

// wheelMsg builds a wheel message in the given direction.
func wheelMsg(up bool) tea.MouseWheelMsg {
	if up {
		return tea.MouseWheelMsg{Button: tea.MouseWheelUp}
	}
	return tea.MouseWheelMsg{Button: tea.MouseWheelDown}
}

// clickMsg builds a click message at the given cell.
func clickMsg(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// zoneBounds scans a rendered view and waits for the zone manager to publish the zone.
func zoneBounds(t *testing.T, view, id string) *zone.ZoneInfo {
	t.Helper()
	zone.Scan(view)
	for range 200 {
		if z := zone.Get(id); !z.IsZero() {
			return z
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("zone %q has no bounds after the scan", id)
	return nil
}

// cardScrollFor returns the offset that keeps item sel of height h inside the viewport.
func cardScrollFor(prev, sel, h int) int {
	top, bottom := sel*h, sel*h+h
	if top < prev {
		return top
	}
	if bottom > prev+cardListHeight {
		return bottom - cardListHeight
	}
	return prev
}

// wantCardCursor fails when the cursor or offset differs, or a link is still focused.
func wantCardCursor(t *testing.T, l *CardList, step string, sel, off int) {
	t.Helper()
	if l.selected != sel {
		t.Fatalf("%s: selected = %d, want %d", step, l.selected, sel)
	}
	if l.scrollOffset != off {
		t.Fatalf("%s: offset = %d, want %d", step, l.scrollOffset, off)
	}
	if l.focusedLink != -1 {
		t.Fatalf("%s: focused link = %d, want -1", step, l.focusedLink)
	}
}

// Every navigation key moves the cursor and the offset by its own rule.
func TestCardListKeysMoveCursor(t *testing.T) {
	l := newKeyCardList(t)
	h := l.itemHeight(0)
	perScreen := 0
	for lines := 0; lines < cardListHeight; lines += h {
		perScreen++
	}
	half := perScreen / 2
	if half < 1 {
		t.Fatalf("half page = %d cards, want at least 1", half)
	}
	off := 0
	for _, step := range []struct {
		key      string
		consumed bool
		sel      int
	}{
		{"j", true, 1},
		{"down", true, 2},
		{"k", true, 1},
		{"up", true, 0},
		{"k", false, 0},
		{"pgdown", true, half},
		{"ctrl+d", true, 2 * half},
		{"pgup", true, half},
		{"ctrl+u", true, 0},
		{"pgup", false, 0},
		{"G", true, 11},
		{"j", false, 11},
		{"end", false, 11},
		{"g", true, 0},
		{"home", false, 0},
	} {
		consumed, activate, link := l.Update(keyMsg(step.key))
		if consumed != step.consumed {
			t.Fatalf("%s: consumed = %v, want %v", step.key, consumed, step.consumed)
		}
		if activate || link != nil {
			t.Fatalf("%s: activate = %v, link = %v, want neither", step.key, activate, link)
		}
		switch step.key {
		case "g", "home":
			off = 0
		default:
			off = cardScrollFor(off, step.sel, h)
		}
		wantCardCursor(t, l, step.key, step.sel, off)
	}
}

// The link keys cycle the focused link, esc unfocuses it, and enter opens it.
func TestCardListLinkKeys(t *testing.T) {
	l := newKeyCardList(t)
	if consumed, _, _ := l.Update(keyMsg("esc")); consumed {
		t.Fatal("esc with no focused link: consumed = true, want false")
	}
	for _, want := range []int{0, 1, -1, 0} {
		if consumed, _, _ := l.Update(keyMsg(";")); !consumed {
			t.Fatal(";: consumed = false, want true")
		}
		if l.focusedLink != want {
			t.Fatalf(";: focused link = %d, want %d", l.focusedLink, want)
		}
	}
	for _, want := range []int{-1, 1, 0} {
		if consumed, _, _ := l.Update(keyMsg(",")); !consumed {
			t.Fatal(",: consumed = false, want true")
		}
		if l.focusedLink != want {
			t.Fatalf(",: focused link = %d, want %d", l.focusedLink, want)
		}
	}
	consumed, activate, link := l.Update(keyMsg("enter"))
	if !consumed || activate || link == nil || link.Path != "/title/card-00" {
		t.Fatalf("enter on a focused link: consumed=%v activate=%v link=%v", consumed, activate, link)
	}
	if consumed, _, _ := l.Update(keyMsg("esc")); !consumed || l.focusedLink != -1 {
		t.Fatalf("esc on a focused link: consumed=%v focused link=%d", consumed, l.focusedLink)
	}
	consumed, activate, link = l.Update(keyMsg("enter"))
	if !consumed || !activate || link != nil {
		t.Fatalf("enter with no focused link: consumed=%v activate=%v link=%v", consumed, activate, link)
	}
	l.Update(keyMsg(";"))
	if consumed, _, _ := l.Update(keyMsg("j")); !consumed || l.focusedLink != -1 {
		t.Fatalf("j after focusing a link: consumed=%v focused link=%d", consumed, l.focusedLink)
	}
}

// A card list with no links leaves the link keys unconsumed.
func TestCardListLinkKeysWithoutLinks(t *testing.T) {
	l := NewCardList(items("a", "b"))
	l.SetSize(80, cardListHeight)
	for _, key := range []string{";", ","} {
		if consumed, _, _ := l.Update(keyMsg(key)); consumed {
			t.Fatalf("%s without links: consumed = true, want false", key)
		}
		if l.focusedLink != -1 {
			t.Fatalf("%s without links: focused link = %d, want -1", key, l.focusedLink)
		}
	}
}

// The wheel moves the cursor one card and stops at both ends.
func TestCardListWheelMovesCursor(t *testing.T) {
	l := newKeyCardList(t)
	h := l.itemHeight(0)
	off := 0
	for _, step := range []struct {
		up  bool
		sel int
	}{{false, 1}, {false, 2}, {true, 1}, {true, 0}, {true, 0}} {
		consumed, activate, link := l.Update(wheelMsg(step.up))
		if !consumed || activate || link != nil {
			t.Fatalf("wheel: consumed=%v activate=%v link=%v", consumed, activate, link)
		}
		off = cardScrollFor(off, step.sel, h)
		wantCardCursor(t, l, "wheel", step.sel, off)
	}
}

// A click selects the card under the cursor, and a second click activates it.
func TestCardListClickSelectsAndActivates(t *testing.T) {
	l := newKeyCardList(t)
	z := zoneBounds(t, l.View(), ZoneID(l.zonePrefix, 2))
	consumed, activate, link := l.Update(clickMsg(z.StartX, z.StartY))
	if !consumed || activate || link != nil {
		t.Fatalf("first click: consumed=%v activate=%v link=%v", consumed, activate, link)
	}
	if l.selected != 2 {
		t.Fatalf("first click: selected = %d, want 2", l.selected)
	}
	consumed, activate, link = l.Update(clickMsg(z.StartX, z.StartY))
	if !consumed || !activate || link != nil {
		t.Fatalf("second click: consumed=%v activate=%v link=%v", consumed, activate, link)
	}
	if consumed, _, _ := l.Update(clickMsg(0, cardListHeight+40)); consumed {
		t.Fatal("click outside every zone: consumed = true, want false")
	}
}

// A click on a link zone opens that link instead of moving the cursor.
func TestCardListClickOpensLink(t *testing.T) {
	l := NewCardList(linkedCards(3))
	l.SetCardOptions(CardOptions{MaxLines: 1, ShowStats: false, Separator: false})
	l.SetSize(80, cardListHeight)
	view := l.View()
	if len(l.linkZones) == 0 {
		t.Fatal("the selected card published no link zones")
	}
	want := l.linkZones[0]
	z := zoneBounds(t, view, want.ZoneID)
	consumed, activate, link := l.Update(clickMsg(z.StartX, z.StartY))
	if !consumed || activate || link == nil {
		t.Fatalf("link click: consumed=%v activate=%v link=%v", consumed, activate, link)
	}
	if link.Path != want.Location.Path {
		t.Fatalf("link click: path = %q, want %q", link.Path, want.Location.Path)
	}
	if l.selected != 0 {
		t.Fatalf("link click: selected = %d, want it unmoved at 0", l.selected)
	}
}
