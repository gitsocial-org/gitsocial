// util_cardlist.go - Scrollable list component for displaying cards with vim navigation
package tuicore

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
)

// ConsumedCmd is a no-op command views return when CardList consumed a key,
// preventing the registry from also handling it (e.g. esc unfocusing a link
// should not also trigger back navigation).
var ConsumedCmd tea.Cmd = func() tea.Msg { return nil }

// cardCacheEntry stores a cached per-card render to avoid re-rendering unchanged cards.
type cardCacheEntry struct {
	rendered string
	selected bool
	dimmed   bool
}

type CardList struct {
	items        []DisplayItem
	selected     int
	scrollOffset int // line offset for scrolling
	width        int
	height       int
	active       bool
	cardOptions  CardOptions
	zonePrefix   string
	// Line count of each item's rendered card; 0 = not measured yet
	itemHeights []int
	// Item resolver for nested items
	itemIndex        map[string]int // maps item ID to index in items
	itemResolver     ItemResolver
	externalResolver ItemResolver // fallback resolver for items not in list
	// Dimmed state overrides (index -> dimmed)
	dimmedOverrides map[int]bool
	// Link zone tracking for clickable card fields
	linkZones   []CardLinkZone // rebuilt on each View()
	focusedLink int            // -1 = none, 0..n = index in selected card's links
	// View cache - avoids re-rendering all visible cards every frame
	cachedView string
	viewDirty  bool
	// Per-card render cache — on cursor move, only old+new selection re-render
	cardCache map[int]cardCacheEntry
}

// NewCardList creates a new card list with the given items.
func NewCardList(items []DisplayItem) *CardList {
	l := &CardList{
		items:       items,
		active:      true,
		zonePrefix:  zone.NewPrefix(),
		focusedLink: -1,
		cardOptions: CardOptions{
			MaxLines:  5,
			ShowStats: true,
			Separator: true,
		},
	}
	l.buildItemIndex()
	return l
}

// SetActive sets whether the list is active.
func (l *CardList) SetActive(active bool) {
	if l.active != active {
		l.active = active
		l.viewDirty = true
	}
}

// SetCardOptions sets the card rendering options.
func (l *CardList) SetCardOptions(opts CardOptions) {
	l.cardOptions = opts
	l.invalidateHeightCache()
	l.adjustScroll()
}

// SetItems sets the items and resets selection.
func (l *CardList) SetItems(items []DisplayItem) {
	l.items = items
	l.selected = 0
	l.scrollOffset = 0
	l.invalidateHeightCache()
	l.buildItemIndex()
}

// ReloadItems replaces items while keeping the cursor anchored to the currently
// selected item by ID (falling back to the top when it is gone). Unlike SetItems
// it preserves selection, and unlike a one-shot restore it is idempotent — safe
// to apply repeatedly for the same reload (e.g. a load message handled by both a
// SetDisplayItems hook and the view's own Update).
func (l *CardList) ReloadItems(items []DisplayItem) {
	prevID := ""
	if l.selected >= 0 && l.selected < len(l.items) {
		prevID = l.items[l.selected].ItemID()
	}
	l.items = items
	l.selected = 0
	l.scrollOffset = 0
	l.invalidateHeightCache()
	l.buildItemIndex()
	if prevID != "" {
		if idx, ok := l.itemIndex[prevID]; ok && idx < len(items) {
			l.selected = idx
			l.adjustScroll()
		}
	}
}

// buildItemIndex builds the item ID to index map.
func (l *CardList) buildItemIndex() {
	l.itemIndex = make(map[string]int)
	for i, item := range l.items {
		l.itemIndex[item.ItemID()] = i
	}
	l.itemResolver = func(itemID string) (DisplayItem, bool) {
		if idx, ok := l.itemIndex[itemID]; ok && idx < len(l.items) {
			return l.items[idx], true
		}
		if l.externalResolver != nil {
			return l.externalResolver(itemID)
		}
		return nil, false
	}
}

// SetItemResolver sets an external resolver for nested items not in the list.
func (l *CardList) SetItemResolver(resolver ItemResolver) {
	l.externalResolver = resolver
	l.buildItemIndex()
}

// SetSize sets the list dimensions.
func (l *CardList) SetSize(width, height int) {
	if l.width != width {
		l.invalidateHeightCache()
	}
	l.width = width
	l.height = height
	l.adjustScroll()
}

// Items returns all items.
func (l *CardList) Items() []DisplayItem {
	return l.items
}

// SetDimmed sets a dimmed override for a specific item index.
func (l *CardList) SetDimmed(idx int, dimmed bool) {
	if l.dimmedOverrides == nil {
		l.dimmedOverrides = make(map[int]bool)
	}
	l.dimmedOverrides[idx] = dimmed
	delete(l.cardCache, idx)
	if idx < len(l.itemHeights) {
		l.itemHeights[idx] = 0
	}
	l.viewDirty = true
}

// Selected returns the selected index.
func (l *CardList) Selected() int {
	return l.selected
}

// SetSelected sets the selected index. Every cursor move dirties the view cache.
func (l *CardList) SetSelected(idx int) {
	if idx >= 0 && idx < len(l.items) {
		l.selected = idx
		l.focusedLink = -1
		l.adjustScroll()
		l.viewDirty = true
	}
}

// SelectedItem returns the selected item.
func (l *CardList) SelectedItem() (DisplayItem, bool) {
	if l.selected >= 0 && l.selected < len(l.items) {
		return l.items[l.selected], true
	}
	return nil, false
}

// SelectedID returns the ID of the selected item.
func (l *CardList) SelectedID() (string, bool) {
	if l.selected >= 0 && l.selected < len(l.items) {
		return l.items[l.selected].ItemID(), true
	}
	return "", false
}

// SelectByID selects the item with the given ID, preserving cursor across a
// reload even when items were reordered or inserted. Returns false if absent.
func (l *CardList) SelectByID(id string) bool {
	if idx, ok := l.itemIndex[id]; ok && idx < len(l.items) {
		l.SetSelected(idx)
		return true
	}
	return false
}

// FocusedLinkLocation returns the Location of the currently focused link, if any.
func (l *CardList) FocusedLinkLocation() *Location {
	if l.focusedLink < 0 || l.selected < 0 || l.selected >= len(l.items) {
		return nil
	}
	card := l.items[l.selected].ToCard(l.itemResolver)
	links := card.AllLinks()
	if l.focusedLink >= len(links) {
		return nil
	}
	loc := links[l.focusedLink].Location
	return &loc
}

// Update handles keyboard and mouse input. Returns (consumed, activate, link):
// consumed = input was handled by the list
// activate = selected item should be opened (enter or click same item)
// link = a link was activated (click or enter on focused link)
func (l *CardList) Update(msg tea.Msg) (consumed, activate bool, link *Location) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return l.updateKey(msg)
	case tea.MouseMsg:
		return l.updateMouse(msg)
	}
	return false, false, nil
}

// updateKey handles keyboard navigation and activation.
func (l *CardList) updateKey(msg tea.KeyPressMsg) (consumed, activate bool, link *Location) {
	switch msg.String() {
	case "esc":
		if l.focusedLink >= 0 {
			l.focusedLink = -1
			l.viewDirty = true
			return true, false, nil
		}
	case "up", "k":
		if l.selected > 0 {
			l.selected--
			l.focusedLink = -1
			l.adjustScroll()
			l.viewDirty = true
			return true, false, nil
		}
	case "down", "j":
		if l.selected < len(l.items)-1 {
			l.selected++
			l.focusedLink = -1
			l.adjustScroll()
			l.viewDirty = true
			return true, false, nil
		}
	case "enter":
		if loc := l.FocusedLinkLocation(); loc != nil {
			return true, false, loc
		}
		if l.selected >= 0 && l.selected < len(l.items) {
			return true, true, nil
		}
	case ";":
		if l.selected >= 0 && l.selected < len(l.items) {
			card := l.items[l.selected].ToCard(l.itemResolver)
			links := card.AllLinks()
			if len(links) == 0 {
				return false, false, nil
			}
			l.focusedLink++
			if l.focusedLink >= len(links) {
				l.focusedLink = -1
			}
			l.viewDirty = true
			return true, false, nil
		}
	case ",":
		if l.selected >= 0 && l.selected < len(l.items) {
			card := l.items[l.selected].ToCard(l.itemResolver)
			links := card.AllLinks()
			if len(links) == 0 {
				return false, false, nil
			}
			l.focusedLink--
			if l.focusedLink < -1 {
				l.focusedLink = len(links) - 1
			}
			l.viewDirty = true
			return true, false, nil
		}
	case "pgup", "ctrl+u":
		if l.selected > 0 {
			target := l.selected - l.visibleItemCount()/2
			if target < 0 {
				target = 0
			}
			l.selected = target
			l.focusedLink = -1
			l.adjustScroll()
			l.viewDirty = true
			return true, false, nil
		}
	case "pgdown", "ctrl+d":
		if l.selected < len(l.items)-1 {
			target := l.selected + l.visibleItemCount()/2
			if target >= len(l.items) {
				target = len(l.items) - 1
			}
			l.selected = target
			l.focusedLink = -1
			l.adjustScroll()
			l.viewDirty = true
			return true, false, nil
		}
	case "home", "g":
		if l.selected != 0 {
			l.selected = 0
			l.focusedLink = -1
			l.scrollOffset = 0
			l.viewDirty = true
			return true, false, nil
		}
	case "end", "G":
		if l.selected != len(l.items)-1 {
			l.selected = len(l.items) - 1
			l.focusedLink = -1
			l.adjustScroll()
			l.viewDirty = true
			return true, false, nil
		}
	}
	return false, false, nil
}

// invalidateHeightCache clears the measured item heights, per-card cache, and view cache.
func (l *CardList) invalidateHeightCache() {
	l.itemHeights = nil
	l.cardCache = nil
	l.viewDirty = true
}

// itemHeight returns the line count of item idx's rendered card, measuring it once per width.
// The height the list scrolls by is the height RenderCard draws, so the selected card stays in frame.
// Measuring uses the unselected render, which differs from the selected one in escapes alone.
func (l *CardList) itemHeight(idx int) int {
	if len(l.itemHeights) != len(l.items) {
		l.itemHeights = make([]int, len(l.items))
	}
	if l.itemHeights[idx] > 0 {
		return l.itemHeights[idx]
	}
	rendered, _ := l.renderItem(idx, l.items[idx], false)
	l.itemHeights[idx] = strings.Count(rendered, "\n") + 1
	return l.itemHeights[idx]
}

// visibleItemCount returns how many items fit the viewport from the selection down.
func (l *CardList) visibleItemCount() int {
	if l.height <= 0 || len(l.items) == 0 {
		return 1
	}
	lines, count := 0, 0
	for i := l.selected; i < len(l.items) && lines < l.height; i++ {
		lines += l.itemHeight(i)
		count++
	}
	if count < 1 {
		count = 1
	}
	return count
}

// adjustScroll adjusts scroll to keep selected item visible.
func (l *CardList) adjustScroll() {
	if len(l.items) == 0 || l.height <= 0 || l.selected < 0 || l.selected >= len(l.items) {
		return
	}
	linePos := 0
	for i := 0; i < l.selected; i++ {
		linePos += l.itemHeight(i)
	}
	selectedHeight := l.itemHeight(l.selected)
	if linePos < l.scrollOffset {
		l.scrollOffset = linePos
	}
	if linePos+selectedHeight > l.scrollOffset+l.height {
		l.scrollOffset = linePos + selectedHeight - l.height
	}
}

// updateMouse handles mouse events.
func (l *CardList) updateMouse(msg tea.MouseMsg) (handled, clicked bool, link *Location) {
	if len(l.items) == 0 {
		return false, false, nil
	}
	switch msg.(type) {
	case tea.MouseWheelMsg:
		m := msg.Mouse()
		if m.Button == tea.MouseWheelUp {
			if l.selected > 0 {
				l.selected--
				l.focusedLink = -1
				l.adjustScroll()
				l.viewDirty = true
			}
		} else {
			if l.selected < len(l.items)-1 {
				l.selected++
				l.focusedLink = -1
				l.adjustScroll()
				l.viewDirty = true
			}
		}
		return true, false, nil
	case tea.MouseClickMsg:
		// Check link zones first (more specific)
		if loc := LinkZoneClicked(msg, l.linkZones); loc != nil {
			return true, false, loc
		}
		idx := ZoneClicked(msg, len(l.items), l.zonePrefix)
		if idx < 0 {
			return false, false, nil
		}
		if idx == l.selected {
			return true, true, nil
		}
		l.selected = idx
		l.focusedLink = -1
		l.adjustScroll()
		l.viewDirty = true
		return true, false, nil
	}
	return false, false, nil
}

// NearBottom returns true when the selected item is within one screen of the end.
func (l *CardList) NearBottom() bool {
	if len(l.items) == 0 {
		return false
	}
	return l.selected >= len(l.items)-l.visibleItemCount()
}

// AppendItems appends items to the list without resetting selection or scroll.
func (l *CardList) AppendItems(items []DisplayItem) {
	l.items = append(l.items, items...)
	l.invalidateHeightCache()
	l.buildItemIndex()
}

// View renders the card list.
func (l *CardList) View() string {
	if len(l.items) == 0 {
		return Dim.Render("No items")
	}
	if !l.viewDirty && l.cachedView != "" {
		return l.cachedView
	}
	l.linkZones = l.linkZones[:0] // reset link zones
	var lines []string
	currentLine := 0
	for i, item := range l.items {
		h := l.itemHeight(i)
		itemEndLine := currentLine + h
		if itemEndLine <= l.scrollOffset {
			currentLine = itemEndLine
			continue
		}
		if currentLine >= l.scrollOffset+l.height {
			break
		}
		isSelected := i == l.selected && l.active
		rendered, anchors := l.renderItem(i, item, isSelected)
		if anchors != nil {
			l.linkZones = append(l.linkZones, anchors.Zones()...)
		}
		// Wrap card in a zone for click detection. Trim trailing newlines first so the
		// zone end marker lands on the separator line (full width) rather than an empty
		// line where EndX < StartX would cause bubblezone InBounds to always fail.
		trimmed := strings.TrimRight(rendered, "\n")
		trailing := len(rendered) - len(trimmed)
		rendered = MarkZone(ZoneID(l.zonePrefix, i), trimmed)
		if trailing > 0 {
			rendered += strings.Repeat("\n", trailing)
		}
		itemLines := strings.Split(rendered, "\n")
		for j, line := range itemLines {
			lineNum := currentLine + j
			if lineNum >= l.scrollOffset && lineNum < l.scrollOffset+l.height {
				lines = append(lines, line)
			}
		}
		currentLine = itemEndLine
	}
	if len(lines) > l.height {
		lines = lines[:l.height]
	}
	// Remove trailing separator lines (lines that are only ─ characters)
	for len(lines) > 0 {
		last := strings.TrimSpace(lines[len(lines)-1])
		if last == "" || strings.Trim(last, "─") == "" {
			lines = lines[:len(lines)-1]
		} else {
			break
		}
	}
	l.cachedView = strings.Join(lines, "\n")
	l.viewDirty = false
	return l.cachedView
}

// renderItem renders a single item. Returns the rendered string and the AnchorCollector (non-nil for selected).
// Uses per-card cache: non-selected cards with unchanged dimmed state return cached renders.
// Selected cards always re-render to collect fresh anchor zones for link navigation.
func (l *CardList) renderItem(idx int, item DisplayItem, selected bool) (string, *AnchorCollector) {
	dimmed := item.IsDimmed()
	if d, ok := l.dimmedOverrides[idx]; ok {
		dimmed = d
	}
	// Non-selected cards: use per-card cache if state matches
	if !selected {
		if entry, ok := l.cardCache[idx]; ok && !entry.selected && entry.dimmed == dimmed {
			return entry.rendered, nil
		}
	}
	card := item.ToCard(l.itemResolver)
	opts := l.cardOptions
	opts.Selected = selected
	opts.Width = l.width
	opts.WrapWidth = l.width - 1
	opts.Dimmed = dimmed
	var anchors *AnchorCollector
	if selected {
		anchors = NewAnchorCollector(l.zonePrefix+fmt.Sprintf("_%d", idx), l.focusedLink)
		opts.Anchors = anchors
	}
	rendered := RenderCard(card, opts)
	if l.cardCache == nil {
		l.cardCache = make(map[int]cardCacheEntry)
	}
	l.cardCache[idx] = cardCacheEntry{rendered: rendered, selected: selected, dimmed: dimmed}
	return rendered, anchors
}
