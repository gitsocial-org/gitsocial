// component_sectionlist_keys_test.go - SectionList cursor, offset and link focus under every key and mouse action
package tuicore

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// sectionListHeight is the viewport height every SectionList key test renders at.
const sectionListHeight = 9

// sectionLinks returns the two links item i carries.
func sectionLinks(i int) []CardLink {
	return []CardLink{
		{Label: "first", Location: Location{Path: fmt.Sprintf("/item/%d/first", i)}},
		{Label: "second", Location: Location{Path: fmt.Sprintf("/item/%d/second", i)}},
	}
}

// sectionSearchText returns the searchable text of item i.
func sectionSearchText(i int) string {
	switch i {
	case 1:
		return "beta and beta again"
	case 4:
		return "one beta here"
	}
	return "alpha only"
}

// keySectionItem builds one three-line item, linked on even indexes.
func keySectionItem(i int) SectionItem {
	item := SectionItem{
		SearchText: func() string { return sectionSearchText(i) },
		Render: func(_ int, _ bool, _ string, anchors *AnchorCollector) []string {
			links := sectionLinks(i)
			return []string{
				fmt.Sprintf(" item %d", i),
				" " + anchors.Mark(links[0].Label, links[0].Location) + " " + anchors.Mark(links[1].Label, links[1].Location),
				" " + sectionSearchText(i),
			}
		},
	}
	if i%2 == 0 {
		item.Links = func() []CardLink { return sectionLinks(i) }
	}
	if i != 3 {
		item.OnActivate = func() tea.Cmd {
			return func() tea.Msg {
				return NavigateMsg{Location: Location{Path: fmt.Sprintf("/activated/%d", i)}, Action: NavPush}
			}
		}
	}
	return item
}

// newKeySectionList builds a six-item section list of three-line items.
func newKeySectionList() *SectionList {
	sl := NewSectionList()
	items := make([]SectionItem, 6)
	for i := range items {
		items[i] = keySectionItem(i)
	}
	sl.SetSections([]Section{{Items: items}})
	sl.SetSize(80, sectionListHeight)
	sl.View()
	return sl
}

// wantSectionCursor fails when the cursor or offset differs, or a link is still focused.
func wantSectionCursor(t *testing.T, sl *SectionList, step string, sel, off int) {
	t.Helper()
	if sl.selected != sel {
		t.Fatalf("%s: selected = %d, want %d", step, sl.selected, sel)
	}
	if sl.scrollOffset != off {
		t.Fatalf("%s: offset = %d, want %d", step, sl.scrollOffset, off)
	}
	if sl.focusedLink != -1 {
		t.Fatalf("%s: focused link = %d, want -1", step, sl.focusedLink)
	}
}

// Every navigation key moves the cursor and the offset by its own rule.
func TestSectionListKeysMoveCursor(t *testing.T) {
	sl := newKeySectionList()
	for _, step := range []struct {
		key string
		sel int
		off int
	}{
		{"j", 1, 0},
		{"down", 2, 0},
		{"j", 3, 9},
		{"k", 2, 6},
		{"up", 1, 3},
		{"g", 0, 0},
		{"G", 5, 9},
		{"home", 0, 0},
		{"end", 5, 9},
		{"g", 0, 0},
		{"pgdown", 2, 4},
		{"pgup", 2, 0},
		{"ctrl+d", 2, 4},
		{"ctrl+u", 2, 0},
		{"pgup", 2, 0},
	} {
		consumed, cmd := sl.Update(keyMsg(step.key))
		if !consumed || cmd != nil {
			t.Fatalf("%s: consumed = %v, cmd = %v, want true and no command", step.key, consumed, cmd)
		}
		sl.View()
		wantSectionCursor(t, sl, step.key, step.sel, step.off)
	}
}

// j and k scroll inside an item taller than the viewport before moving on.
func TestSectionListKeysScrollInsideTallItem(t *testing.T) {
	tall := SectionItem{Render: func(_ int, _ bool, _ string, _ *AnchorCollector) []string {
		lines := make([]string, 12)
		for i := range lines {
			lines[i] = fmt.Sprintf(" tall %d", i)
		}
		return lines
	}}
	sl := NewSectionList()
	sl.SetSections([]Section{{Items: []SectionItem{tall, keySectionItem(1)}}})
	sl.SetSize(80, sectionListHeight)
	sl.View()
	for _, want := range []int{1, 2, 3} {
		sl.Update(keyMsg("j"))
		sl.View()
		wantSectionCursor(t, sl, "j inside the tall item", 0, want)
	}
	sl.Update(keyMsg("j"))
	sl.View()
	wantSectionCursor(t, sl, "j past the tall item", 1, 6)
	sl.Update(keyMsg("k"))
	sl.View()
	wantSectionCursor(t, sl, "k back into the tall item", 0, 0)
}

// The link keys cycle the focused link, esc unfocuses it, and enter opens it.
func TestSectionListLinkKeys(t *testing.T) {
	sl := newKeySectionList()
	if consumed, _ := sl.Update(keyMsg("esc")); consumed {
		t.Fatal("esc with no focused link: consumed = true, want false")
	}
	for _, want := range []int{0, 1, -1, 0} {
		if consumed, _ := sl.Update(keyMsg(";")); !consumed {
			t.Fatal(";: consumed = false, want true")
		}
		if sl.focusedLink != want {
			t.Fatalf(";: focused link = %d, want %d", sl.focusedLink, want)
		}
	}
	for _, want := range []int{-1, 1, 0} {
		if consumed, _ := sl.Update(keyMsg(",")); !consumed {
			t.Fatal(",: consumed = false, want true")
		}
		if sl.focusedLink != want {
			t.Fatalf(",: focused link = %d, want %d", sl.focusedLink, want)
		}
	}
	consumed, cmd := sl.Update(keyMsg("enter"))
	if !consumed || cmd == nil {
		t.Fatalf("enter on a focused link: consumed = %v, cmd = %v", consumed, cmd)
	}
	nav, ok := cmd().(NavigateMsg)
	if !ok || nav.Location.Path != "/item/0/first" {
		t.Fatalf("enter on a focused link: message = %#v, want the link location", cmd())
	}
	if consumed, _ := sl.Update(keyMsg("esc")); !consumed || sl.focusedLink != -1 {
		t.Fatalf("esc on a focused link: consumed = %v, focused link = %d", consumed, sl.focusedLink)
	}
	sl.Update(keyMsg(";"))
	sl.Update(keyMsg("j"))
	if sl.focusedLink != -1 {
		t.Fatalf("j after focusing a link: focused link = %d, want -1", sl.focusedLink)
	}
}

// An item without links leaves the link keys unconsumed.
func TestSectionListLinkKeysWithoutLinks(t *testing.T) {
	sl := newKeySectionList()
	sl.Update(keyMsg("j"))
	for _, key := range []string{";", ","} {
		if consumed, _ := sl.Update(keyMsg(key)); consumed {
			t.Fatalf("%s without links: consumed = true, want false", key)
		}
		if sl.focusedLink != -1 {
			t.Fatalf("%s without links: focused link = %d, want -1", key, sl.focusedLink)
		}
	}
}

// enter activates the selected item, and esc clears a navigation highlight.
func TestSectionListActivateAndHighlight(t *testing.T) {
	sl := newKeySectionList()
	consumed, cmd := sl.Update(keyMsg("enter"))
	if !consumed || cmd == nil {
		t.Fatalf("enter: consumed = %v, cmd = %v", consumed, cmd)
	}
	nav, ok := cmd().(NavigateMsg)
	if !ok || nav.Location.Path != "/activated/0" {
		t.Fatalf("enter: message = %#v, want the item's activation", cmd())
	}
	sl.SetSelected(3)
	if consumed, cmd := sl.Update(keyMsg("enter")); consumed || cmd != nil {
		t.Fatalf("enter on an item with no action: consumed = %v, cmd = %v", consumed, cmd)
	}
	sl.SetHighlightQuery("beta")
	if consumed, _ := sl.Update(keyMsg("esc")); !consumed || sl.highlightQuery != "" {
		t.Fatalf("esc on a highlight: consumed = %v, highlight = %q", consumed, sl.highlightQuery)
	}
}

// The wheel scrolls three lines at a time and stops at both ends.
func TestSectionListWheelScrolls(t *testing.T) {
	sl := newKeySectionList()
	for _, want := range []int{3, 6, 9, 9} {
		consumed, cmd := sl.Update(wheelMsg(false))
		if !consumed || cmd != nil {
			t.Fatalf("wheel down: consumed = %v, cmd = %v", consumed, cmd)
		}
		sl.View()
		wantSectionCursor(t, sl, "wheel down", 0, want)
	}
	for _, want := range []int{6, 3, 0, 0} {
		if consumed, _ := sl.Update(wheelMsg(true)); !consumed {
			t.Fatal("wheel up: consumed = false, want true")
		}
		sl.View()
		wantSectionCursor(t, sl, "wheel up", 0, want)
	}
}

// A click selects the item under the cursor, and a second click activates it.
func TestSectionListClickSelectsAndActivates(t *testing.T) {
	sl := newKeySectionList()
	z := zoneBounds(t, sl.View(), ZoneID(sl.zonePrefix, 2))
	consumed, cmd := sl.Update(clickMsg(z.StartX, z.StartY))
	if !consumed || cmd != nil {
		t.Fatalf("first click: consumed = %v, cmd = %v", consumed, cmd)
	}
	if sl.selected != 2 {
		t.Fatalf("first click: selected = %d, want 2", sl.selected)
	}
	consumed, cmd = sl.Update(clickMsg(z.StartX, z.StartY))
	if !consumed || cmd == nil {
		t.Fatalf("second click: consumed = %v, cmd = %v", consumed, cmd)
	}
	if nav, ok := cmd().(NavigateMsg); !ok || nav.Location.Path != "/activated/2" {
		t.Fatalf("second click: message = %#v, want the item's activation", cmd())
	}
	if consumed, _ := sl.Update(clickMsg(0, sectionListHeight+40)); consumed {
		t.Fatal("click outside every zone: consumed = true, want false")
	}
}

// A click on a link zone opens that link instead of moving the cursor.
func TestSectionListClickOpensLink(t *testing.T) {
	sl := newKeySectionList()
	view := sl.View()
	if len(sl.linkZones) == 0 {
		t.Fatal("the list published no link zones")
	}
	want := sl.linkZones[0]
	z := zoneBounds(t, view, want.ZoneID)
	consumed, cmd := sl.Update(clickMsg(z.StartX, z.StartY))
	if !consumed || cmd == nil {
		t.Fatalf("link click: consumed = %v, cmd = %v", consumed, cmd)
	}
	if nav, ok := cmd().(NavigateMsg); !ok || nav.Location.Path != want.Location.Path {
		t.Fatalf("link click: message = %#v, want %q", cmd(), want.Location.Path)
	}
	if sl.selected != 0 {
		t.Fatalf("link click: selected = %d, want it unmoved at 0", sl.selected)
	}
}

// Search opens on /, n and N walk the matches, and esc closes it.
func TestSectionListSearchKeys(t *testing.T) {
	sl := newKeySectionList()
	consumed, cmd := sl.Update(keyMsg("/"))
	if !consumed || cmd == nil {
		t.Fatalf("/: consumed = %v, cmd = %v", consumed, cmd)
	}
	if !sl.IsSearchActive() || !sl.IsInputActive() {
		t.Fatalf("/: search active = %v, input active = %v", sl.IsSearchActive(), sl.IsInputActive())
	}
	for _, r := range "beta" {
		sl.Update(keyMsg(string(r)))
	}
	if sl.searchQuery != "beta" {
		t.Fatalf("typing: query = %q, want %q", sl.searchQuery, "beta")
	}
	if sl.matchCount != 3 || sl.matchIndex != 1 || sl.selected != 1 {
		t.Fatalf("typing: matches = %d, index = %d, selected = %d, want 3, 1, 1", sl.matchCount, sl.matchIndex, sl.selected)
	}
	if consumed, _ := sl.Update(keyMsg("enter")); !consumed || sl.IsInputActive() || !sl.IsSearchActive() {
		t.Fatalf("enter: consumed = %v, input active = %v, search active = %v", consumed, sl.IsInputActive(), sl.IsSearchActive())
	}
	for _, step := range []struct {
		key   string
		index int
		sel   int
	}{{"n", 2, 1}, {"n", 3, 4}, {"n", 1, 1}, {"N", 3, 4}, {"N", 2, 1}} {
		if consumed, _ := sl.Update(keyMsg(step.key)); !consumed {
			t.Fatalf("%s: consumed = false, want true", step.key)
		}
		if sl.matchIndex != step.index || sl.selected != step.sel {
			t.Fatalf("%s: index = %d, selected = %d, want %d and %d", step.key, sl.matchIndex, sl.selected, step.index, step.sel)
		}
	}
	if consumed, cmd := sl.Update(keyMsg("/")); !consumed || cmd == nil || !sl.IsInputActive() {
		t.Fatalf("/ in search: consumed = %v, cmd = %v, input active = %v", consumed, cmd, sl.IsInputActive())
	}
	if consumed, _ := sl.Update(keyMsg("esc")); !consumed || sl.IsSearchActive() || sl.searchQuery != "" {
		t.Fatalf("esc in search: consumed = %v, active = %v, query = %q", consumed, sl.IsSearchActive(), sl.searchQuery)
	}
}

// esc that the list consumes returns a command, so the global esc binding does not navigate back as well.
func TestSectionListEscClaimsTheKey(t *testing.T) {
	sl := newKeySectionList()
	sl.Update(keyMsg(";"))
	if _, cmd := sl.Update(keyMsg("esc")); cmd == nil {
		t.Error("esc on a focused link: cmd = nil, so the global esc binding also navigates back")
	}
	sl.SetHighlightQuery("beta")
	if _, cmd := sl.Update(keyMsg("esc")); cmd == nil {
		t.Error("esc on a highlight: cmd = nil, so the global esc binding also navigates back")
	}
	sl.Update(keyMsg("/"))
	for _, r := range "beta" {
		sl.Update(keyMsg(string(r)))
	}
	sl.Update(keyMsg("enter"))
	if _, cmd := sl.Update(keyMsg("esc")); cmd == nil {
		t.Error("esc in search navigation: cmd = nil, so the global esc binding also navigates back")
	}
}

// The mouse is inert while the search input has focus.
func TestSectionListMouseIgnoredInSearchInput(t *testing.T) {
	sl := newKeySectionList()
	sl.Update(keyMsg("/"))
	if consumed, _ := sl.Update(wheelMsg(false)); consumed {
		t.Fatal("wheel in the search input: consumed = true, want false")
	}
	if sl.scrollOffset != 0 {
		t.Fatalf("wheel in the search input: offset = %d, want 0", sl.scrollOffset)
	}
}
