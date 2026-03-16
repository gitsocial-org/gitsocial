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

// CardRenderer renders a Card to a string and calculates its height.
// This allows the rendering logic to live in components while CardList lives in tuicore.
type CardRenderer interface {
	RenderCard(card Card, opts CardOptions) string
	CardHeight(card Card, opts CardOptions) int
}

// DefaultCardRenderer is set by components package on init
var DefaultCardRenderer CardRenderer

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
	renderer     CardRenderer
	zonePrefix   string
	// Cached calculations
	itemHeights        []int
	cachedVisibleCount int
	heightCacheValid   bool
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
		renderer:    DefaultCardRenderer,
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

// SetRenderer sets the card renderer.
func (l *CardList) SetRenderer(r CardRenderer) {
	l.renderer = r
	l.invalidateHeightCache()
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

// UpdateItems updates items while preserving selection.
func (l *CardList) UpdateItems(items []DisplayItem) {
	l.items = items
	if l.selected >= len(items) {
		l.selected = max(0, len(items)-1)
	}
	l.invalidateHeightCache()
	l.buildItemIndex()
	l.adjustScroll()
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
	l.viewDirty = true
}

// Selected returns the selected index.
func (l *CardList) Selected() int {
	return l.selected
}

// SetSelected sets the selected index.
func (l *CardList) SetSelected(idx int) {
	if idx >= 0 && idx < len(l.items) {
		l.selected = idx
		l.adjustScroll()
	}
}

// SelectedItem returns the selected item.
func (l *CardList) SelectedItem() (DisplayItem, bool) {
	if l.selected >= 0 && l.selected < len(l.items) {
		return l.items[l.selected], true
	}
	return nil, false
}

// FocusedLink returns the index of the currently focused link (-1 = none).
func (l *CardList) FocusedLink() int {
	return l.focusedLink
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
	case "tab":
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
	case "shift+tab":
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

// invalidateHeightCache clears the cached item heights, per-card cache, and view cache.
func (l *CardList) invalidateHeightCache() {
	l.heightCacheValid = false
	l.itemHeights = nil
	l.cachedVisibleCount = 0
	l.cardCache = nil
	l.viewDirty = true
}

// ensureHeightCache calculates and caches item heights.
func (l *CardList) ensureHeightCache() {
	if l.heightCacheValid && len(l.itemHeights) == len(l.items) {
		return
	}
	l.itemHeights = make([]int, len(l.items))
	totalHeight := 0
	for i, item := range l.items {
		h := l.calculateItemHeight(item)
		l.itemHeights[i] = h
		totalHeight += h
	}
	if len(l.items) > 0 {
		avgHeight := totalHeight / len(l.items)
		if avgHeight < 1 {
			avgHeight = 1
		}
		l.cachedVisibleCount = l.height / avgHeight
	} else {
		l.cachedVisibleCount = 1
	}
	l.heightCacheValid = true
}

// visibleItemCount returns approximate number of visible items.
func (l *CardList) visibleItemCount() int {
	if l.height <= 0 || len(l.items) == 0 {
		return 1
	}
	l.ensureHeightCache()
	return l.cachedVisibleCount
}

// adjustScroll adjusts scroll to keep selected item visible.
func (l *CardList) adjustScroll() {
	if len(l.items) == 0 || l.height <= 0 {
		return
	}
	l.ensureHeightCache()
	linePos := 0
	for i := 0; i < l.selected; i++ {
		linePos += l.itemHeights[i]
	}
	selectedHeight := l.itemHeights[l.selected]
	if linePos < l.scrollOffset {
		l.scrollOffset = linePos
	}
	if linePos+selectedHeight > l.scrollOffset+l.height {
		l.scrollOffset = linePos + selectedHeight - l.height
	}
}

// calculateItemHeight calculates the height of an item.
func (l *CardList) calculateItemHeight(item DisplayItem) int {
	if l.renderer == nil {
		return 3 // fallback
	}
	card := item.ToCard(l.itemResolver)
	opts := l.cardOptions
	opts.Width = l.width
	return l.renderer.CardHeight(card, opts)
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
	l.ensureHeightCache()
	l.linkZones = l.linkZones[:0] // reset link zones
	var lines []string
	currentLine := 0
	for i, item := range l.items {
		h := l.itemHeights[i]
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
	if l.renderer == nil {
		return item.ItemID(), nil // fallback
	}
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
	rendered := l.renderer.RenderCard(card, opts)
	if l.cardCache == nil {
		l.cardCache = make(map[int]cardCacheEntry)
	}
	l.cardCache[idx] = cardCacheEntry{rendered: rendered, selected: selected, dimmed: dimmed}
	return rendered, anchors
}
