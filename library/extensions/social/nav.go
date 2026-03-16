// nav.go - Navigation registry items and view metadata for social extension
package social

import "github.com/gitsocial-org/gitsocial/tui/tuicore"

// RegisterNavItems registers social extension navigation items
func RegisterNavItems(r *tuicore.NavRegistry) {
	// Social domain (order 0 - first in list, not selectable)
	r.Register(tuicore.NavItem{
		ID:      "social",
		Label:   "Social",
		Icon:    "⌘",
		Order:   0,
		Enabled: false,
	})

	// Timeline sub-item
	r.Register(tuicore.NavItem{
		ID:      "social.timeline",
		Label:   "Timeline",
		Icon:    "⏱",
		Parent:  "social",
		Order:   0,
		Enabled: true,
	})

	// My Repository sub-item
	r.Register(tuicore.NavItem{
		ID:      "social.myrepo",
		Label:   "My Repository",
		Icon:    "♥",
		Parent:  "social",
		Order:   1,
		Enabled: true,
	})

	// My Lists sub-item
	r.Register(tuicore.NavItem{
		ID:      "social.lists",
		Label:   "My Lists",
		Icon:    "☷",
		Parent:  "social",
		Order:   2,
		Enabled: true,
	})
}

// UpdateListItems updates the dynamic list items under social.lists in the nav.
func UpdateListItems(r *tuicore.NavRegistry, lists []List) {
	items := make([]tuicore.NavItem, len(lists))
	for i, list := range lists {
		label := list.Name
		if label == "" {
			label = list.ID
		}
		items[i] = tuicore.NavItem{
			ID:      "social.lists." + list.ID,
			Label:   label,
			Icon:    "",
			Parent:  "social.lists",
			Order:   i,
			Enabled: true,
		}
	}
	r.RegisterDynamic("social.lists", items)
}
