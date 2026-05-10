// util_history_diff.go - Shared navigation helper for history-diff routes.
package tuicore

import (
	tea "charm.land/bubbletea/v2"
)

// OpenHistoryDiff returns a command that navigates to a history-diff route
// comparing the picker's current item to its older neighbor.
//
// olderOffset is the offset (in items[]) to the older neighbor: +1 for
// DESC-ordered pickers (latest first) and -1 for ASC-ordered ones. If the
// neighbor is out of range, the route's "from" parameter is left empty —
// the diff view falls back to picking a pair on its own.
//
// itemID maps a picker item to its diff-route version ID. Pass nil to use
// item.GetID(); pass a custom function when the picker's IDs differ from
// what the diff loader produces (e.g., PR versions whose loader emits
// synthetic "v<N>" IDs while the picker carries commit hashes).
func OpenHistoryDiff(
	picker *VersionPicker,
	state *State,
	idParam string,
	loc func(id, fromID, toID string) Location,
	olderOffset int,
	itemID func(item VersionItem, idx int) string,
) tea.Cmd {
	items := picker.Items()
	if len(items) < 2 {
		return nil
	}
	if itemID == nil {
		itemID = func(item VersionItem, _ int) string { return item.GetID() }
	}
	cursor := picker.Cursor()
	toID := itemID(items[cursor], cursor)
	var fromID string
	if olderIdx := cursor + olderOffset; olderIdx >= 0 && olderIdx < len(items) {
		fromID = itemID(items[olderIdx], olderIdx)
	}
	id := state.Router.Location().Param(idParam)
	return func() tea.Msg {
		return NavigateMsg{
			Location: loc(id, fromID, toID),
			Action:   NavPush,
		}
	}
}
