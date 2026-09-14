// component_list_actions.go - The key and mouse map CardList and SectionList share
package tuicore

import tea "charm.land/bubbletea/v2"

// listAction is one navigation action a list resolves a key or mouse message to.
type listAction int

// The actions both list components resolve; each list applies its own scroll model.
const (
	listActionNone listAction = iota
	listActionMoveDown
	listActionMoveUp
	listActionTop
	listActionBottom
	listActionPageDown
	listActionPageUp
	listActionNextLink
	listActionPrevLink
	listActionUnfocus
	listActionActivate
	listActionSearch
	listActionNextMatch
	listActionPrevMatch
	listActionWheel
	listActionClick
)

// resolveListAction maps a key press or mouse message to the action it triggers.
func resolveListAction(msg tea.Msg) listAction {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		switch msg.(type) {
		case tea.MouseWheelMsg:
			return listActionWheel
		case tea.MouseClickMsg:
			return listActionClick
		}
		return listActionNone
	}
	switch key.String() {
	case "down", "j":
		return listActionMoveDown
	case "up", "k":
		return listActionMoveUp
	case "home", "g":
		return listActionTop
	case "end", "G":
		return listActionBottom
	case "pgdown", "ctrl+d":
		return listActionPageDown
	case "pgup", "ctrl+u":
		return listActionPageUp
	case ";":
		return listActionNextLink
	case ",":
		return listActionPrevLink
	case "esc":
		return listActionUnfocus
	case "enter":
		return listActionActivate
	case "/":
		return listActionSearch
	case "n":
		return listActionNextMatch
	case "N":
		return listActionPrevMatch
	}
	return listActionNone
}

// cycleLinkIndex moves the focused link by step, wrapping through -1 for none.
func cycleLinkIndex(focused, count, step int) (int, bool) {
	if count == 0 {
		return -1, false
	}
	next := focused + step
	if next >= count {
		next = -1
	}
	if next < -1 {
		next = count - 1
	}
	return next, true
}

// focusedLinkLocation returns the location of the focused link, or nil when there is none.
func focusedLinkLocation(links []CardLink, focused int) *Location {
	if focused < 0 || focused >= len(links) {
		return nil
	}
	loc := links[focused].Location
	return &loc
}

// listClickTarget resolves a click to a link, or to an item index, or to -1 for a miss.
func listClickTarget(msg tea.Msg, linkZones []CardLinkZone, count int, prefix string) (*Location, int) {
	click, ok := msg.(tea.MouseClickMsg)
	if !ok {
		return nil, -1
	}
	if loc := LinkZoneClicked(click, linkZones); loc != nil {
		return loc, -1
	}
	return nil, ZoneClicked(click, count, prefix)
}

// navigateTo returns a command that pushes loc onto the router.
func navigateTo(loc Location) tea.Cmd {
	return func() tea.Msg {
		return NavigateMsg{Location: loc, Action: NavPush}
	}
}

// wheelScrollsUp reports whether a wheel message scrolls up.
func wheelScrollsUp(msg tea.Msg) bool {
	wheel, ok := msg.(tea.MouseWheelMsg)
	return ok && wheel.Button == tea.MouseWheelUp
}
