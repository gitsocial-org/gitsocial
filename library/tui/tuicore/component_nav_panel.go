// nav_panel.go - Left navigation sidebar panel with domain/extension menu
package tuicore

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"
)

// Special nav item IDs (not in registry)
const (
	NavIDSearch        = "_search"
	NavIDNotifications = "_notifications"
	NavIDAnalytics     = "_analytics"
	NavIDErrorLog      = "_errorlog"
)

// NavPanel is the left navigation sidebar
type NavPanel struct {
	width            int
	height           int
	focused          bool
	registry         *NavRegistry
	router           *Router
	cursorID         string // Visual cursor position (may differ from router selection when browsing)
	workdir          string
	cacheSize        string
	unreadCount      int
	unpushedCount    int
	unpushedLFSCount int
	errorLogCount    int
	zonePrefix       string
	// Cached flat items
	cachedItems      []NavItem
	cachedDomain     string
	cachedRegVersion int
	// View cache
	cachedView     string
	viewDirty      bool
	cachedFocused  bool
	cachedActiveID string
}

// NewNavPanel creates a new navigation panel.
func NewNavPanel(workdir string, registry *NavRegistry, r *Router) *NavPanel {
	return &NavPanel{
		registry:   registry,
		router:     r,
		cursorID:   r.NavItemID(),
		workdir:    workdir,
		zonePrefix: zone.NewPrefix(),
	}
}

// Init returns an initial command.
func (p *NavPanel) Init() tea.Cmd {
	return nil
}

// Update handles messages and returns updated model.
func (p *NavPanel) Update(msg tea.Msg) (*NavPanel, tea.Cmd) {
	if !p.focused {
		return p, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "down", "j":
			return p.moveCursor(1)
		case "up", "k":
			return p.moveCursor(-1)
		case "home":
			p.cursorID = NavIDSearch
			p.viewDirty = true
			return p, nil
		case "end":
			items := p.getFlatItems()
			if len(items) > 0 {
				p.cursorID = items[len(items)-1].ID
				p.viewDirty = true
			}
			return p, nil
		case "enter":
			return p.commitNavigation()
		case "esc", "tab":
			// Reset cursor to router selection and focus content
			p.cursorID = p.router.NavItemID()
			p.viewDirty = true
			return p, func() tea.Msg { return FocusMsg{Panel: 0} }
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			idx := int(msg.String()[0] - '1')
			topLevel := p.registry.GetTopLevel()
			if idx < len(topLevel) {
				item := topLevel[idx]
				children := p.registry.GetChildren(item.ID)
				if len(children) > 0 {
					p.cursorID = children[0].ID
				} else {
					p.cursorID = item.ID
				}
				p.viewDirty = true
			}
			return p, nil
		}
	}
	return p, nil
}

// getFlatItems returns all navigable items in order (cached per domain+registry version).
func (p *NavPanel) getFlatItems() []NavItem {
	domain := p.currentDomain()
	regVersion := p.registry.Version()
	if p.cachedItems != nil && p.cachedDomain == domain && p.cachedRegVersion == regVersion {
		return p.cachedItems
	}
	items := p.buildFlatItems()
	p.cachedItems = items
	p.cachedDomain = domain
	p.cachedRegVersion = regVersion
	return items
}

// buildFlatItems rebuilds the flat navigation item list.
func (p *NavPanel) buildFlatItems() []NavItem {
	var items []NavItem
	items = append(items, NavItem{ID: NavIDSearch, Label: "Search", Icon: "⌕", Enabled: true})
	items = append(items, NavItem{ID: NavIDNotifications, Label: "Notifications", Icon: "⚑", Enabled: true})
	items = append(items, NavItem{ID: NavIDAnalytics, Label: "Analytics", Icon: "◧", Enabled: true})

	currentDomain := p.currentDomain()

	// Separate into extension items, DM, and bottom items (config, cache, settings)
	var extensionItems, bottomItems []NavItem
	var dmItem *NavItem
	for _, top := range p.registry.GetTopLevel() {
		if top.ID == "dm" {
			t := top
			dmItem = &t
		} else if top.Order >= 9 {
			bottomItems = append(bottomItems, top)
		} else {
			extensionItems = append(extensionItems, top)
		}
	}

	// Add extension items (skip disabled/unimplemented)
	for _, top := range extensionItems {
		if !top.Enabled && top.ID != "social" {
			continue
		}
		items = append(items, top)
		if top.ID == currentDomain || top.ID == "social" {
			items = append(items, p.flattenChildren(top.ID)...)
		}
	}

	// Add DM (anchored above bottom section, skip if disabled)
	if dmItem != nil && dmItem.Enabled {
		items = append(items, *dmItem)
		if dmItem.ID == currentDomain {
			items = append(items, p.flattenChildren(dmItem.ID)...)
		}
	}

	// Add bottom items last (skip disabled)
	for _, top := range bottomItems {
		if !top.Enabled {
			continue
		}
		items = append(items, top)
		if top.ID == currentDomain {
			items = append(items, p.flattenChildren(top.ID)...)
		}
	}

	// Error log at the very bottom
	items = append(items, NavItem{ID: NavIDErrorLog, Label: "Error Log", Icon: "⚠", Enabled: true})

	return items
}

// flattenChildren recursively flattens child nav items.
func (p *NavPanel) flattenChildren(parentID string) []NavItem {
	children := p.registry.GetChildren(parentID)
	result := make([]NavItem, 0, len(children))
	for _, child := range children {
		result = append(result, child)
		result = append(result, p.flattenChildren(child.ID)...)
	}
	return result
}

// currentDomain returns the domain of the current cursor.
func (p *NavPanel) currentDomain() string {
	return p.domainOf(p.cursorID)
}

// domainOf returns the domain portion of a nav item ID.
func (p *NavPanel) domainOf(id string) string {
	if id == NavIDSearch || id == NavIDNotifications || id == NavIDAnalytics || id == NavIDErrorLog {
		return ""
	}
	parts := strings.Split(id, ".")
	if len(parts) > 0 {
		return parts[0]
	}
	return id
}

// moveCursor moves the cursor by delta items.
func (p *NavPanel) moveCursor(delta int) (*NavPanel, tea.Cmd) {
	items := p.getFlatItems()
	currentIdx := -1
	for i, item := range items {
		if item.ID == p.cursorID {
			currentIdx = i
			break
		}
	}
	if currentIdx < 0 {
		if len(items) > 0 {
			p.cursorID = items[0].ID
		}
		return p, nil
	}

	// Find next enabled item
	for i := 1; i <= len(items); i++ {
		nextIdx := (currentIdx + i*delta + len(items)) % len(items)
		if items[nextIdx].Enabled {
			p.cursorID = items[nextIdx].ID
			p.viewDirty = true
			break
		}
	}
	return p, nil
}

// commitNavigation commits the current cursor selection.
func (p *NavPanel) commitNavigation() (*NavPanel, tea.Cmd) {
	loc := p.cursorToLocation()
	return p, func() tea.Msg {
		return NavigateMsg{Location: loc, Action: NavPush}
	}
}

// cursorToLocation converts cursor ID to a location.
func (p *NavPanel) cursorToLocation() Location {
	switch p.cursorID {
	case NavIDSearch:
		return LocSearch
	case NavIDNotifications:
		return LocNotifications
	case NavIDAnalytics:
		return LocAnalytics
	case NavIDErrorLog:
		return LocErrorLog
	case "social.timeline":
		return LocTimeline
	case "social.myrepo":
		return LocMyRepo
	case "social.lists":
		return LocLists
	case "settings":
		return LocSettings
	case "config", "config.core":
		return LocConfig("core")
	case "config.forks":
		return LocForks
	case "config.social":
		return LocConfig("social")
	case "config.pm":
		return LocPMConfig
	case "cache":
		return LocCache
	case "pm", "pm.board":
		return LocPMBoard
	case "pm.issues":
		return LocPMIssues
	case "pm.milestones":
		return LocPMMilestones
	case "pm.sprints":
		return LocPMSprints
	case "release":
		return LocReleaseList
	case "review", "review.prs":
		return LocReviewPRs
	default:
		if strings.HasPrefix(p.cursorID, "social.lists.") {
			listID := strings.TrimPrefix(p.cursorID, "social.lists.")
			return LocList(listID)
		}
		return LocTimeline
	}
}

// SetSize sets the panel dimensions.
func (p *NavPanel) SetSize(width, height int) {
	if p.width != width || p.height != height {
		p.width = width
		p.height = height
		p.viewDirty = true
	}
}

// SetFocused sets the focus state of the panel.
func (p *NavPanel) SetFocused(focused bool) {
	p.focused = focused
	p.cursorID = p.router.NavItemID()
	p.viewDirty = true
}

// IsFocused returns true if the panel is focused.
func (p *NavPanel) IsFocused() bool {
	return p.focused
}

// SetCacheSize sets the displayed cache size.
func (p *NavPanel) SetCacheSize(size string) {
	p.cacheSize = size
	p.viewDirty = true
}

// SetUnreadCount sets the unread notification count.
func (p *NavPanel) SetUnreadCount(count int) {
	p.unreadCount = count
	p.viewDirty = true
}

// SetUnpushedCount sets the unpushed posts count.
func (p *NavPanel) SetUnpushedCount(count int) {
	p.unpushedCount = count
	p.viewDirty = true
}

// SetUnpushedLFSCount sets the unpushed LFS objects count for releases.
func (p *NavPanel) SetUnpushedLFSCount(count int) {
	p.unpushedLFSCount = count
	p.viewDirty = true
}

// SetErrorLogCount sets the error log entry count.
func (p *NavPanel) SetErrorLogCount(count int) {
	p.errorLogCount = count
	p.viewDirty = true
}

// Registry returns the navigation registry.
func (p *NavPanel) Registry() *NavRegistry {
	return p.registry
}

// UpdateMouse handles mouse events on the nav panel.
func (p *NavPanel) UpdateMouse(msg tea.MouseMsg) (*NavPanel, tea.Cmd) {
	switch msg.(type) {
	case tea.MouseWheelMsg:
		m := msg.Mouse()
		if m.Button == tea.MouseWheelUp {
			return p.moveCursor(-1)
		}
		return p.moveCursor(1)
	case tea.MouseClickMsg:
		for _, item := range p.getFlatItems() {
			if zone.Get(p.zonePrefix + item.ID).InBounds(msg) {
				p.cursorID = item.ID
				return p.commitNavigation()
			}
		}
		return p, nil
	}
	return p, nil
}

// View renders the navigation panel.
func (p *NavPanel) View() string {
	// Determine effective active ID for cache key
	activeID := p.cursorID
	if !p.focused {
		activeID = p.router.NavItemID()
	}
	if !p.viewDirty && p.cachedView != "" && p.cachedFocused == p.focused && p.cachedActiveID == activeID {
		return p.cachedView
	}

	borderColor := BorderUnfocused
	if p.focused {
		borderColor = BorderFocused
	}

	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(borderColor))
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(borderColor))
	if p.focused {
		titleStyle = titleStyle.Bold(true)
	}

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(TextPrimary)).
		Background(lipgloss.Color(BgSelected))
	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(TextNormal))

	innerWidth := p.width - 2
	padToWidth := func(s string) string {
		w := AnsiWidth(s)
		if w < innerWidth {
			return s + selectedStyle.Render(strings.Repeat(" ", innerWidth-w))
		}
		return s
	}

	currentDomain := p.domainOf(activeID)

	// Separate top-level items into extensions, DM, and bottom (config, cache, settings)
	topLevel := p.registry.GetTopLevel()
	var extensionItems, bottomItems []NavItem
	var dmItem *NavItem
	for _, item := range topLevel {
		if item.ID == "dm" {
			t := item
			dmItem = &t
		} else if item.Order >= 9 {
			bottomItems = append(bottomItems, item)
		} else {
			extensionItems = append(extensionItems, item)
		}
	}

	// markLine wraps a rendered line with a zone marker for mouse click detection.
	markLine := func(itemID, line string) string {
		if itemID == "" {
			return line
		}
		return zone.Mark(p.zonePrefix+itemID, line)
	}

	// Build top section (search, notifications, extensions)
	var topContent strings.Builder
	topContent.WriteString("\n")

	// Search /
	searchText := "  " + SafeIcon("⌕") + "  Search"
	searchSelected := activeID == NavIDSearch
	if searchSelected {
		topContent.WriteString(markLine(NavIDSearch, formatNavItem(searchText, "/", innerWidth, selectedStyle, true)))
	} else {
		topContent.WriteString(markLine(NavIDSearch, formatNavItem(searchText, "/", innerWidth, normalStyle, false)))
	}
	topContent.WriteString("\n")

	// Notifications
	notifIcon := SafeIcon("⚑")
	notifText := "  " + notifIcon + "  Notifications"
	if p.unreadCount > 0 {
		notifText = fmt.Sprintf("  "+notifIcon+"  Notifications (%d)", p.unreadCount)
	}
	notifSelected := activeID == NavIDNotifications
	if notifSelected {
		topContent.WriteString(markLine(NavIDNotifications, formatNavItem(notifText, "@", innerWidth, selectedStyle, true)))
	} else {
		topContent.WriteString(markLine(NavIDNotifications, formatNavItem(notifText, "@", innerWidth, normalStyle, false)))
	}
	topContent.WriteString("\n")

	// Analytics
	analyticsText := "  " + SafeIcon("◧") + "  Analytics"
	analyticsSelected := activeID == NavIDAnalytics
	if analyticsSelected {
		topContent.WriteString(markLine(NavIDAnalytics, formatNavItem(analyticsText, "%", innerWidth, selectedStyle, true)))
	} else {
		topContent.WriteString(markLine(NavIDAnalytics, formatNavItem(analyticsText, "%", innerWidth, normalStyle, false)))
	}
	topContent.WriteString("\n")
	topContent.WriteString(Dim.Render(" " + strings.Repeat("─", innerWidth-2) + " "))
	topContent.WriteString("\n")

	// Render extension domains (skip disabled/unimplemented)
	for _, item := range extensionItems {
		if !item.Enabled && item.ID != "social" {
			continue
		}
		isSelected := activeID == item.ID
		label := "  "
		if icon := SafeIcon(item.Icon); icon != "" {
			label += icon + "  "
		}
		label += item.Label
		if item.ID == "release" && p.unpushedLFSCount > 0 {
			label = fmt.Sprintf("%s (%d)", label, p.unpushedLFSCount)
		}
		key := getDomainKey(item.ID)

		if isSelected {
			topContent.WriteString(markLine(item.ID, formatNavItem(label, key, innerWidth, selectedStyle, true)))
		} else {
			topContent.WriteString(markLine(item.ID, formatNavItem(label, key, innerWidth, normalStyle, false)))
		}
		topContent.WriteString("\n")

		if currentDomain == item.ID || isSelected || item.ID == "social" {
			p.renderChildren(&topContent, item.ID, activeID, padToWidth, selectedStyle, normalStyle)
		}
	}

	// Build DM section (anchored above bottom divider, skip if disabled)
	var dmContent strings.Builder
	if dmItem != nil && dmItem.Enabled {
		isSelected := activeID == dmItem.ID
		label := "  "
		if icon := SafeIcon(dmItem.Icon); icon != "" {
			label += icon + "  "
		}
		label += dmItem.Label
		key := getDomainKey(dmItem.ID)

		if isSelected {
			dmContent.WriteString(markLine(dmItem.ID, formatNavItem(label, key, innerWidth, selectedStyle, true)))
		} else {
			dmContent.WriteString(markLine(dmItem.ID, formatNavItem(label, key, innerWidth, normalStyle, false)))
		}
		dmContent.WriteString("\n")

		if currentDomain == dmItem.ID || isSelected {
			p.renderChildren(&dmContent, dmItem.ID, activeID, padToWidth, selectedStyle, normalStyle)
		}
	}

	// Build bottom section (config, cache, settings)
	var bottomContent strings.Builder
	bottomContent.WriteString(Dim.Render(" " + strings.Repeat("─", innerWidth-2) + " "))
	bottomContent.WriteString("\n")

	for _, item := range bottomItems {
		if !item.Enabled {
			continue
		}
		isSelected := activeID == item.ID
		label := "  "
		if icon := SafeIcon(item.Icon); icon != "" {
			label += icon + "  "
		}
		label += item.Label

		// Cache item has special right-aligned size display
		if item.ID == "cache" && p.cacheSize != "" {
			suffix := " " + p.cacheSize + "  "
			padding := innerWidth - AnsiWidth(label) - len(suffix) - 2 // -2 for spaces before dots
			if padding > 0 {
				dots := "  " + strings.Repeat("·", padding)
				var rendered string
				if isSelected {
					dimSelected := lipgloss.NewStyle().
						Foreground(lipgloss.Color(TextSecondary)).
						Background(lipgloss.Color(BgSelected))
					rendered = selectedStyle.Render(label) + dimSelected.Render(dots+suffix)
				} else {
					rendered = normalStyle.Render(label) + Dim.Render(dots+suffix)
				}
				bottomContent.WriteString(markLine(item.ID, rendered))
				bottomContent.WriteString("\n")
				continue
			}
		}

		var rendered string
		if isSelected {
			rendered = padToWidth(selectedStyle.Render(label))
		} else {
			rendered = normalStyle.Render(label)
		}
		bottomContent.WriteString(markLine(item.ID, rendered))
		bottomContent.WriteString("\n")

		if currentDomain == item.ID || isSelected {
			p.renderChildren(&bottomContent, item.ID, activeID, padToWidth, selectedStyle, normalStyle)
		}
	}

	// Error Log (at the very bottom)
	errorLogIcon := SafeIcon("⚠")
	errorLogText := "  " + errorLogIcon + "  Error Log"
	if p.errorLogCount > 0 {
		errorLogText = fmt.Sprintf("  "+errorLogIcon+"  Error Log (%d)", p.errorLogCount)
	}
	errorLogSelected := activeID == NavIDErrorLog
	if errorLogSelected {
		bottomContent.WriteString(markLine(NavIDErrorLog, formatNavItem(errorLogText, "!", innerWidth, selectedStyle, true)))
	} else {
		bottomContent.WriteString(markLine(NavIDErrorLog, formatNavItem(errorLogText, "!", innerWidth, normalStyle, false)))
	}
	bottomContent.WriteString("\n")

	// Calculate heights and combine: top + padding + dm + bottom + footer
	topLines := strings.Split(strings.TrimSuffix(topContent.String(), "\n"), "\n")
	var dmLines []string
	if dmContent.Len() > 0 {
		dmLines = strings.Split(strings.TrimSuffix(dmContent.String(), "\n"), "\n")
	}
	bottomLines := strings.Split(strings.TrimSuffix(bottomContent.String(), "\n"), "\n")
	innerHeight := p.height - 2
	footerHeight := 2 // separator + directory path line

	topCount := len(topLines)
	dmCount := len(dmLines)
	bottomCount := len(bottomLines)
	paddingNeeded := innerHeight - topCount - dmCount - bottomCount - footerHeight
	if paddingNeeded < 0 {
		paddingNeeded = 0
	}

	var contentLines []string
	contentLines = append(contentLines, topLines...)
	for i := 0; i < paddingNeeded; i++ {
		contentLines = append(contentLines, "")
	}
	contentLines = append(contentLines, dmLines...)
	contentLines = append(contentLines, bottomLines...)

	// Separator before directory path
	contentLines = append(contentLines, Dim.Render(" "+strings.Repeat("─", innerWidth-2)+" "))

	// Directory path with background (similar to footer)
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(TextSecondary)).
		Background(lipgloss.Color(BgFooter))
	dirPath := p.truncatePath(innerWidth)
	dirLine := footerStyle.Width(innerWidth).Render(dirPath)
	contentLines = append(contentLines, dirLine)

	// Build frame
	title := SafeIcon("※") + "  GitSocial"
	titleRendered := titleStyle.Render(title)
	titleLen := AnsiWidth(title)
	rightPadLen := innerWidth - titleLen - 3
	if rightPadLen < 0 {
		rightPadLen = 0
	}
	topLine := borderStyle.Render("╭─ ") + titleRendered + borderStyle.Render(" "+strings.Repeat("─", rightPadLen)+"╮")

	borderV := borderStyle.Render("│")

	var lines []string
	lines = append(lines, topLine)
	for i := 0; i < innerHeight && i < len(contentLines); i++ {
		line := contentLines[i]
		lineWidth := AnsiWidth(line)
		padding := innerWidth - lineWidth
		if padding < 0 {
			padding = 0
		}
		lines = append(lines, borderV+line+strings.Repeat(" ", padding)+borderV)
	}
	bottomLine := borderStyle.Render("╰" + strings.Repeat("─", innerWidth) + "╯")
	lines = append(lines, bottomLine)

	p.cachedView = strings.Join(lines, "\n")
	p.cachedFocused = p.focused
	p.cachedActiveID = activeID
	p.viewDirty = false
	return p.cachedView
}

// renderChildren renders child navigation items.
func (p *NavPanel) renderChildren(content *strings.Builder, parentID, activeID string, padToWidth func(string) string, selectedStyle, normalStyle lipgloss.Style) {
	children := p.registry.GetChildren(parentID)

	for _, child := range children {
		if !child.Enabled {
			continue
		}
		indent := "   "
		if strings.Count(child.ID, ".") > 1 {
			indent = "     "
		}

		icon := SafeIcon(child.Icon)
		if icon != "" {
			icon = icon + "  "
		}

		prefix := "  "
		if strings.HasPrefix(child.ID, "social.lists.") {
			prefix = "   • "
		}
		label := child.Label
		if child.ID == "social.myrepo" && p.unpushedCount > 0 {
			label = fmt.Sprintf("%s (%d)", child.Label, p.unpushedCount)
		}
		isSelected := activeID == child.ID
		line := indent + prefix + icon + label
		var rendered string
		if isSelected {
			rendered = padToWidth(selectedStyle.Render(line))
		} else {
			rendered = normalStyle.Render(line)
		}
		content.WriteString(zone.Mark(p.zonePrefix+child.ID, rendered))
		content.WriteString("\n")

		if p.registry.HasChildren(child.ID) {
			p.renderChildren(content, child.ID, activeID, padToWidth, selectedStyle, normalStyle)
		}
	}
}

// formatNavItem formats a nav item with right-aligned key and middot fill.
// Format: "label  ·······  key  " where middots fill to reach targetWidth.
// If label is long enough, dots are omitted.
func formatNavItem(label, key string, targetWidth int, labelStyle lipgloss.Style, selected bool) string {
	if key == "" {
		return labelStyle.Render(label)
	}
	keyPart := "  " + key + "  "
	labelWidth := AnsiWidth(label)
	keyWidth := len(keyPart)
	dotsNeeded := targetWidth - labelWidth - keyWidth - 2 // -2 for spaces before dots
	var fill string
	if dotsNeeded > 0 {
		fill = "  " + strings.Repeat("·", dotsNeeded)
	} else {
		fill = "  " // just spacing, no dots
	}
	if selected {
		// Apply selected background to entire line, but keep fill/key dimmed
		dimSelected := lipgloss.NewStyle().
			Foreground(lipgloss.Color(TextSecondary)).
			Background(lipgloss.Color(BgSelected))
		return labelStyle.Render(label) + dimSelected.Render(fill+keyPart)
	}
	return labelStyle.Render(label) + Dim.Render(fill+keyPart)
}

// getDomainKey returns the extension key for a domain, or empty string.
func getDomainKey(itemID string) string {
	if ek := GetExtensionKey(itemID); ek != nil {
		return ek.Key
	}
	return ""
}

// truncatePath truncates the workdir path to fit width.
func (p *NavPanel) truncatePath(maxWidth int) string {
	path := p.workdir
	if len(path) <= maxWidth {
		return path
	}
	base := filepath.Base(path)
	if len(base)+4 <= maxWidth {
		return ".../" + base
	}
	if maxWidth > 3 {
		return "..." + base[len(base)-(maxWidth-3):]
	}
	return base[:maxWidth]
}
