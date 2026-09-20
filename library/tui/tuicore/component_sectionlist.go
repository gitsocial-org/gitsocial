// component_sectionlist.go - Scrollable sectioned list for detail views with vim navigation, search, and mouse
package tuicore

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
)

// Section is a group of items with an optional header.
type Section struct {
	Label string // "" = no header (hero section)
	Items []SectionItem
}

// SectionItem is one selectable element in a section.
type SectionItem struct {
	Render     func(width int, selected bool, searchQuery string, anchors *AnchorCollector) []string
	SearchText func() string     // nil = not searchable
	Links      func() []CardLink // nil = no links
	OnActivate func() tea.Cmd    // nil = no action
}

// SectionList manages selection, scroll, search, zones, and mouse for sectioned detail views.
type SectionList struct {
	sections       []Section
	totalItems     int
	selected       int
	scrollOffset   int
	prevSelected   int
	width          int
	height         int
	itemStartLines []int
	itemEndLines   []int
	zonePrefix     string
	linkZones      []CardLinkZone
	focusedLink    int
	// Search state
	searchActive    bool
	searchInputMode bool
	searchInput     textinput.Model
	searchQuery     string
	highlightQuery  string // from navigation source (e.g. global search)
	matches         []sectionMatchLocation
	matchIndex      int
	matchCount      int
}

type sectionMatchLocation struct {
	flatIndex int
	matchNum  int
}

// NewSectionList creates a new section list.
func NewSectionList() *SectionList {
	input := textinput.New()
	input.Placeholder = "Search..."
	input.CharLimit = 100
	input.Prompt = "> "
	StyleTextInput(&input, Title, Title, Dim)
	return &SectionList{
		prevSelected: -1,
		focusedLink:  -1,
		zonePrefix:   zone.NewPrefix(),
		searchInput:  input,
	}
}

// SetSections sets the sections and resets selection.
func (sl *SectionList) SetSections(sections []Section) {
	sl.sections = sections
	sl.totalItems = 0
	for _, s := range sections {
		sl.totalItems += len(s.Items)
	}
	sl.selected = 0
	sl.scrollOffset = 0
	sl.prevSelected = -1
	sl.focusedLink = -1
}

// SetSize sets the viewport dimensions.
func (sl *SectionList) SetSize(width, height int) {
	sl.width = width
	sl.height = height
}

// Selected returns the flat selected index across all sections.
func (sl *SectionList) Selected() int {
	return sl.selected
}

// SetSelected sets the selection to a flat index.
func (sl *SectionList) SetSelected(idx int) {
	if idx >= 0 && idx < sl.totalItems {
		sl.selected = idx
		sl.focusedLink = -1
	}
}

// SectionAndIndex returns the section index and item index within that section.
func (sl *SectionList) SectionAndIndex() (section, index int) {
	flat := 0
	for si, s := range sl.sections {
		if sl.selected < flat+len(s.Items) {
			return si, sl.selected - flat
		}
		flat += len(s.Items)
	}
	return 0, 0
}

// selectedLinks returns the links of the selected item.
func (sl *SectionList) selectedLinks() []CardLink {
	item := sl.getItem(sl.selected)
	if item == nil || item.Links == nil {
		return nil
	}
	return item.Links()
}

// FocusedLinkLocation returns the Location of the focused link, if any.
func (sl *SectionList) FocusedLinkLocation() *Location {
	return focusedLinkLocation(sl.selectedLinks(), sl.focusedLink)
}

// IsSearchActive returns true when search mode is active (input or navigation).
func (sl *SectionList) IsSearchActive() bool {
	return sl.searchActive
}

// IsInputActive returns true when the search text input is focused.
func (sl *SectionList) IsInputActive() bool {
	return sl.searchInputMode
}

// SetHighlightQuery sets a highlight query from navigation source (e.g. global search).
// This is used for rendering when no local search is active.
func (sl *SectionList) SetHighlightQuery(query string) {
	sl.highlightQuery = query
}

// SearchQuery returns the current search query for highlight pass-through.
func (sl *SectionList) SearchQuery() string {
	return sl.searchQuery
}

// effectiveHighlight returns the query to use for rendering highlights.
// Local search takes priority over navigation highlight.
func (sl *SectionList) effectiveHighlight() string {
	if sl.searchQuery != "" {
		return sl.searchQuery
	}
	return sl.highlightQuery
}

// updateSearchInput handles a key while the search input has focus.
func (sl *SectionList) updateSearchInput(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "esc":
		sl.exitSearch()
		return true, nil
	case "enter":
		sl.searchInputMode = false
		sl.searchInput.Blur()
		if sl.searchQuery == "" {
			sl.searchActive = false
		}
		return true, nil
	}
	var cmd tea.Cmd
	sl.searchInput, cmd = sl.searchInput.Update(msg)
	sl.updateLiveSearch()
	return true, cmd
}

// updateSearchKey handles a key while search is active but the input is blurred.
func (sl *SectionList) updateSearchKey(action listAction) (bool, tea.Cmd) {
	switch action {
	case listActionNextMatch:
		sl.nextMatch()
		return true, nil
	case listActionPrevMatch:
		sl.prevMatch()
		return true, nil
	case listActionSearch:
		sl.searchInputMode = true
		return true, sl.searchInput.Focus()
	case listActionUnfocus:
		sl.exitSearch()
		// ConsumedCmd keeps the global esc binding from firing on the key we just used.
		return true, ConsumedCmd
	}
	return false, nil
}

// Update handles keyboard and mouse input. Returns (consumed, cmd).
func (sl *SectionList) Update(msg tea.Msg) (bool, tea.Cmd) {
	key, isKey := msg.(tea.KeyPressMsg)
	if sl.searchInputMode {
		if !isKey {
			return false, nil
		}
		return sl.updateSearchInput(key)
	}
	action := resolveListAction(msg)
	if sl.searchActive && isKey {
		return sl.updateSearchKey(action)
	}
	switch action {
	case listActionUnfocus:
		// ConsumedCmd keeps the global esc binding from firing on the key we just used.
		if sl.focusedLink >= 0 {
			sl.focusedLink = -1
			return true, ConsumedCmd
		}
		if sl.highlightQuery != "" {
			sl.highlightQuery = ""
			return true, ConsumedCmd
		}
	case listActionMoveDown:
		sl.moveDown()
		return true, nil
	case listActionMoveUp:
		sl.moveUp()
		return true, nil
	case listActionTop:
		sl.selected = 0
		sl.scrollOffset = 0
		sl.focusedLink = -1
		return true, nil
	case listActionBottom:
		if sl.totalItems > 0 {
			sl.selected = sl.totalItems - 1
			sl.focusedLink = -1
		}
		return true, nil
	case listActionPageUp:
		sl.pageUp()
		return true, nil
	case listActionPageDown:
		sl.pageDown()
		return true, nil
	case listActionNextLink:
		return sl.cycleLink(1), nil
	case listActionPrevLink:
		return sl.cycleLink(-1), nil
	case listActionActivate:
		if loc := sl.FocusedLinkLocation(); loc != nil {
			return true, navigateTo(*loc)
		}
		item := sl.getItem(sl.selected)
		if item != nil && item.OnActivate != nil {
			return true, item.OnActivate()
		}
		return false, nil
	case listActionSearch:
		sl.searchActive = true
		sl.searchInputMode = true
		sl.searchInput.SetValue("")
		return true, sl.searchInput.Focus()
	case listActionWheel:
		if wheelScrollsUp(msg) {
			sl.scrollOffset = max(sl.scrollOffset-3, 0)
		} else {
			sl.scrollOffset += 3
		}
		return true, nil
	case listActionClick:
		loc, idx := listClickTarget(msg, sl.linkZones, sl.totalItems, sl.zonePrefix)
		if loc != nil {
			return true, navigateTo(*loc)
		}
		if idx < 0 {
			return false, nil
		}
		if idx == sl.selected {
			item := sl.getItem(sl.selected)
			if item != nil && item.OnActivate != nil {
				return true, item.OnActivate()
			}
			return true, nil
		}
		sl.selected = idx
		sl.focusedLink = -1
		return true, nil
	}
	return false, nil
}

// View renders the section list.
func (sl *SectionList) View() string {
	if sl.totalItems == 0 {
		return Dim.Render("No items")
	}
	sl.linkZones = sl.linkZones[:0]
	sl.itemStartLines = sl.itemStartLines[:0]
	sl.itemEndLines = sl.itemEndLines[:0]
	var allLines []string
	var itemAnchors []*AnchorCollector
	flatIdx := 0
	for _, s := range sl.sections {
		if s.Label != "" {
			allLines = append(allLines, "", RenderSectionSeparator(sl.width), "")
			allLines = append(allLines, " "+s.Label)
			allLines = append(allLines, " "+Dim.Render(strings.Repeat("─", sl.width-3)))
		}
		for _, item := range s.Items {
			sl.itemStartLines = append(sl.itemStartLines, len(allLines))
			isSelected := flatIdx == sl.selected
			var anchors *AnchorCollector
			if item.Links != nil {
				focused := -1
				if isSelected {
					focused = sl.focusedLink
				}
				anchors = NewAnchorCollector(fmt.Sprintf("%s_link_%d", sl.zonePrefix, flatIdx), focused)
			}
			lines := item.Render(sl.width, isSelected, sl.effectiveHighlight(), anchors)
			itemAnchors = append(itemAnchors, anchors)
			// Wrap entire item in a zone so clicking anywhere on it selects it
			joined := strings.Join(lines, "\n")
			joined = MarkZone(ZoneID(sl.zonePrefix, flatIdx), joined)
			allLines = append(allLines, strings.Split(joined, "\n")...)
			sl.itemEndLines = append(sl.itemEndLines, len(allLines))
			flatIdx++
		}
	}
	// Auto-scroll on selection change
	if sl.selected != sl.prevSelected && sl.selected < len(sl.itemStartLines) {
		sl.prevSelected = sl.selected
		selStart := sl.itemStartLines[sl.selected]
		selEnd := sl.itemEndLines[sl.selected]
		itemHeight := selEnd - selStart
		if selStart < sl.scrollOffset {
			sl.scrollOffset = selStart
		} else if selStart >= sl.scrollOffset+sl.height {
			sl.scrollOffset = selStart
		} else if itemHeight <= sl.height && selEnd > sl.scrollOffset+sl.height {
			sl.scrollOffset = selEnd - sl.height
		}
	}
	// Scroll to focused link if it's outside the viewport
	if sl.focusedLink >= 0 {
		for i, line := range allLines {
			if strings.Contains(line, currentTheme.focusedLinkMarker) {
				if i < sl.scrollOffset {
					sl.scrollOffset = i
				} else if i >= sl.scrollOffset+sl.height {
					sl.scrollOffset = i - sl.height + 1
				}
				break
			}
		}
	}
	// Clamp scroll
	sl.scrollOffset = min(max(sl.scrollOffset, 0), max(len(allLines)-sl.height, 0))
	// Extract visible lines and mark zones
	endLine := min(sl.scrollOffset+sl.height, len(allLines))
	visibleLines := allLines[sl.scrollOffset:endLine]
	// Collect link zones from all visible items
	for i := range sl.itemStartLines {
		if i >= len(sl.itemEndLines) || i >= len(itemAnchors) {
			break
		}
		itemStart := sl.itemStartLines[i]
		itemEnd := sl.itemEndLines[i]
		if itemEnd <= sl.scrollOffset || itemStart >= endLine {
			continue
		}
		if itemAnchors[i] != nil {
			sl.linkZones = append(sl.linkZones, itemAnchors[i].Zones()...)
		}
	}
	return strings.Join(visibleLines, "\n")
}

// SearchFooter renders the search UI footer (input line above, nav hints
// below). viewWrapper.Render applies the bgFooter bar around both lines.
func (sl *SectionList) SearchFooter(width int) string {
	sl.searchInput.SetWidth(width - 5)
	return sl.searchInput.View() + "\n" + RenderSearchFooter(sl.matchIndex, sl.matchCount, sl.searchInputMode, sl.searchQuery != "")
}

// UpdateSearchInput forwards a non-key message to the search input (e.g., blink).
func (sl *SectionList) UpdateSearchInput(msg tea.Msg) tea.Cmd {
	if !sl.searchInputMode {
		return nil
	}
	var cmd tea.Cmd
	sl.searchInput, cmd = sl.searchInput.Update(msg)
	return cmd
}

// moveDown scrolls through a selected item taller than the viewport before moving on.
func (sl *SectionList) moveDown() {
	// If current item extends below viewport, scroll within it first
	if sl.selected < len(sl.itemEndLines) {
		itemEnd := sl.itemEndLines[sl.selected]
		if itemEnd > sl.scrollOffset+sl.height {
			sl.scrollOffset++
			sl.prevSelected = sl.selected // prevent auto-scroll from overriding
			return
		}
	}
	if sl.selected < sl.totalItems-1 {
		sl.selected++
		sl.focusedLink = -1
	}
}

// moveUp scrolls back through a selected item taller than the viewport before moving on.
func (sl *SectionList) moveUp() {
	// If scrolled past the start of current item, scroll within it first
	if sl.selected < len(sl.itemStartLines) {
		itemStart := sl.itemStartLines[sl.selected]
		if sl.scrollOffset > itemStart {
			sl.scrollOffset--
			sl.prevSelected = sl.selected // prevent auto-scroll from overriding
			return
		}
	}
	if sl.selected > 0 {
		sl.selected--
		sl.focusedLink = -1
	}
}

// pageDown scrolls the viewport half a page down, advancing selection if it falls above.
func (sl *SectionList) pageDown() {
	scroll := max(sl.height/2, 1)
	sl.scrollOffset += scroll
	// If selected item scrolled above viewport, advance to first visible item
	if sl.selected < len(sl.itemEndLines) && sl.itemEndLines[sl.selected] <= sl.scrollOffset {
		for i := sl.selected + 1; i < sl.totalItems && i < len(sl.itemStartLines); i++ {
			if sl.itemStartLines[i] >= sl.scrollOffset {
				sl.selected = i
				sl.focusedLink = -1
				break
			}
		}
	}
	sl.prevSelected = sl.selected
}

// pageUp scrolls the viewport half a page up, retreating selection if it falls below.
func (sl *SectionList) pageUp() {
	scroll := max(sl.height/2, 1)
	sl.scrollOffset = max(sl.scrollOffset-scroll, 0)
	// If selected item scrolled below viewport, retreat to last visible item
	if sl.selected < len(sl.itemStartLines) && sl.itemStartLines[sl.selected] >= sl.scrollOffset+sl.height {
		for i := sl.selected - 1; i >= 0; i-- {
			if i < len(sl.itemStartLines) && sl.itemStartLines[i] < sl.scrollOffset+sl.height {
				sl.selected = i
				sl.focusedLink = -1
				break
			}
		}
	}
	sl.prevSelected = sl.selected
}

// cycleLink moves the focused link by step and reports whether the key was consumed.
func (sl *SectionList) cycleLink(step int) bool {
	next, ok := cycleLinkIndex(sl.focusedLink, len(sl.selectedLinks()), step)
	if !ok {
		return false
	}
	sl.focusedLink = next
	return true
}

// getItem returns the item at a flat index across all sections.
func (sl *SectionList) getItem(flatIdx int) *SectionItem {
	cur := 0
	for si := range sl.sections {
		sLen := len(sl.sections[si].Items)
		if flatIdx < cur+sLen {
			return &sl.sections[si].Items[flatIdx-cur]
		}
		cur += sLen
	}
	return nil
}

// updateLiveSearch takes the query from the input and jumps to its first match.
func (sl *SectionList) updateLiveSearch() {
	sl.searchQuery = sl.searchInput.Value()
	if sl.searchQuery == "" {
		sl.matches = nil
		sl.matchIndex = 0
		sl.matchCount = 0
		return
	}
	sl.buildMatchLocations()
	sl.matchCount = len(sl.matches)
	if sl.matchCount == 0 {
		sl.matchIndex = 0
	} else {
		sl.matchIndex = 1
		sl.navigateToMatch(0)
	}
}

// buildMatchLocations records every query match across all sections.
func (sl *SectionList) buildMatchLocations() {
	sl.matches = nil
	if sl.searchQuery == "" {
		return
	}
	pattern := CompileSearchPattern(sl.searchQuery)
	flatIdx := 0
	for _, s := range sl.sections {
		for _, item := range s.Items {
			if item.SearchText != nil && pattern != nil {
				text := item.SearchText()
				for i := range pattern.FindAllStringIndex(text, -1) {
					sl.matches = append(sl.matches, sectionMatchLocation{flatIndex: flatIdx, matchNum: i})
				}
			}
			flatIdx++
		}
	}
}

// nextMatch selects the following match, wrapping to the first.
func (sl *SectionList) nextMatch() {
	if sl.matchCount == 0 {
		return
	}
	sl.matchIndex++
	if sl.matchIndex > sl.matchCount {
		sl.matchIndex = 1
	}
	sl.navigateToMatch(sl.matchIndex - 1)
}

// prevMatch selects the preceding match, wrapping to the last.
func (sl *SectionList) prevMatch() {
	if sl.matchCount == 0 {
		return
	}
	sl.matchIndex--
	if sl.matchIndex < 1 {
		sl.matchIndex = sl.matchCount
	}
	sl.navigateToMatch(sl.matchIndex - 1)
}

// navigateToMatch selects the item holding the match at idx.
func (sl *SectionList) navigateToMatch(idx int) {
	sl.selected = sl.matches[idx].flatIndex
	sl.focusedLink = -1
}

// exitSearch clears the query, the matches and the input focus.
func (sl *SectionList) exitSearch() {
	sl.searchActive = false
	sl.searchInputMode = false
	sl.searchQuery = ""
	sl.searchInput.Blur()
	sl.matches = nil
	sl.matchIndex = 0
	sl.matchCount = 0
}
