// util_sectionlist.go - Scrollable sectioned list for detail views with vim navigation, search, and mouse
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

// FocusedLinkLocation returns the Location of the focused link, if any.
func (sl *SectionList) FocusedLinkLocation() *Location {
	if sl.focusedLink < 0 {
		return nil
	}
	item := sl.getItem(sl.selected)
	if item == nil || item.Links == nil {
		return nil
	}
	links := item.Links()
	if sl.focusedLink >= len(links) {
		return nil
	}
	loc := links[sl.focusedLink].Location
	return &loc
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

// Update handles keyboard and mouse input. Returns (consumed, cmd).
func (sl *SectionList) Update(msg tea.Msg) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return sl.updateKey(msg)
	case tea.MouseMsg:
		return sl.updateMouse(msg)
	}
	return false, nil
}

// updateKey handles keyboard input.
func (sl *SectionList) updateKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if sl.searchInputMode {
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
	if sl.searchActive {
		switch msg.String() {
		case "n":
			sl.nextMatch()
			return true, nil
		case "N":
			sl.prevMatch()
			return true, nil
		case "/":
			sl.searchInputMode = true
			return true, sl.searchInput.Focus()
		case "esc":
			sl.exitSearch()
			return true, nil
		}
		return false, nil
	}
	switch msg.String() {
	case "esc":
		if sl.focusedLink >= 0 {
			sl.focusedLink = -1
			return true, nil
		}
		if sl.highlightQuery != "" {
			sl.highlightQuery = ""
			return true, nil
		}
	case "down", "j":
		sl.moveDown()
		return true, nil
	case "up", "k":
		sl.moveUp()
		return true, nil
	case "home", "g":
		sl.selected = 0
		sl.scrollOffset = 0
		sl.focusedLink = -1
		return true, nil
	case "end", "G":
		if sl.totalItems > 0 {
			sl.selected = sl.totalItems - 1
			sl.focusedLink = -1
		}
		return true, nil
	case "pgup", "ctrl+u":
		sl.pageUp()
		return true, nil
	case "pgdown", "ctrl+d":
		sl.pageDown()
		return true, nil
	case ";":
		return sl.cycleLinkForward(), nil
	case ",":
		return sl.cycleLinkBackward(), nil
	case "enter":
		if sl.focusedLink >= 0 {
			if loc := sl.FocusedLinkLocation(); loc != nil {
				navLoc := *loc
				return true, func() tea.Msg {
					return NavigateMsg{Location: navLoc, Action: NavPush}
				}
			}
		}
		item := sl.getItem(sl.selected)
		if item != nil && item.OnActivate != nil {
			return true, item.OnActivate()
		}
		return false, nil
	case "/":
		sl.searchActive = true
		sl.searchInputMode = true
		sl.searchInput.SetValue("")
		return true, sl.searchInput.Focus()
	}
	return false, nil
}

// updateMouse handles mouse events.
func (sl *SectionList) updateMouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	if sl.searchInputMode {
		return false, nil
	}
	switch msg.(type) {
	case tea.MouseWheelMsg:
		m := msg.Mouse()
		if m.Button == tea.MouseWheelUp {
			sl.scrollOffset -= 3
			if sl.scrollOffset < 0 {
				sl.scrollOffset = 0
			}
		} else {
			sl.scrollOffset += 3
		}
		return true, nil
	case tea.MouseClickMsg:
		if loc := LinkZoneClicked(msg, sl.linkZones); loc != nil {
			navLoc := *loc
			return true, func() tea.Msg {
				return NavigateMsg{Location: navLoc, Action: NavPush}
			}
		}
		idx := ZoneClicked(msg, sl.totalItems, sl.zonePrefix)
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
			if strings.Contains(line, FocusedLinkMarker) {
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
	maxScroll := len(allLines) - sl.height
	if maxScroll < 0 {
		maxScroll = 0
	}
	if sl.scrollOffset > maxScroll {
		sl.scrollOffset = maxScroll
	}
	if sl.scrollOffset < 0 {
		sl.scrollOffset = 0
	}
	// Extract visible lines and mark zones
	endLine := sl.scrollOffset + sl.height
	if endLine > len(allLines) {
		endLine = len(allLines)
	}
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

// SearchFooter renders the search UI footer.
func (sl *SectionList) SearchFooter(width int) string {
	sl.searchInput.SetWidth(width - 5)
	return sl.searchInput.View() + "\n" + RenderSearchFooter(width, sl.matchIndex, sl.matchCount, sl.searchInputMode, sl.searchQuery != "")
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
	scroll := sl.height / 2
	if scroll < 1 {
		scroll = 1
	}
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
	scroll := sl.height / 2
	if scroll < 1 {
		scroll = 1
	}
	sl.scrollOffset -= scroll
	if sl.scrollOffset < 0 {
		sl.scrollOffset = 0
	}
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

func (sl *SectionList) cycleLinkForward() bool {
	item := sl.getItem(sl.selected)
	if item == nil || item.Links == nil {
		return false
	}
	links := item.Links()
	if len(links) == 0 {
		return false
	}
	sl.focusedLink++
	if sl.focusedLink >= len(links) {
		sl.focusedLink = -1
	}
	return true
}

func (sl *SectionList) cycleLinkBackward() bool {
	item := sl.getItem(sl.selected)
	if item == nil || item.Links == nil {
		return false
	}
	links := item.Links()
	if len(links) == 0 {
		return false
	}
	sl.focusedLink--
	if sl.focusedLink < -1 {
		sl.focusedLink = len(links) - 1
	}
	return true
}

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

func (sl *SectionList) navigateToMatch(idx int) {
	sl.selected = sl.matches[idx].flatIndex
	sl.focusedLink = -1
}

func (sl *SectionList) exitSearch() {
	sl.searchActive = false
	sl.searchInputMode = false
	sl.searchQuery = ""
	sl.searchInput.Blur()
	sl.matches = nil
	sl.matchIndex = 0
	sl.matchCount = 0
}
