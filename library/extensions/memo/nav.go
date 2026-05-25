// nav.go - Memo extension navigation registry items
package memo

import "github.com/gitsocial-org/gitsocial/library/tui/tuicore"

// RegisterNavItems registers memo extension navigation items.
func RegisterNavItems(r *tuicore.NavRegistry) {
	r.Register(tuicore.NavItem{
		ID:      "memo",
		Label:   "Memos",
		Icon:    "☞",
		Order:   4,
		Enabled: true,
	})
	r.Register(tuicore.NavItem{
		ID:      "memo.project",
		Label:   "Project",
		Icon:    "·",
		Parent:  "memo",
		Order:   0,
		Enabled: true,
	})
	r.Register(tuicore.NavItem{
		ID:      "memo.inherited",
		Label:   "Inherited",
		Icon:    "·",
		Parent:  "memo",
		Order:   1,
		Enabled: true,
	})
	r.Register(tuicore.NavItem{
		ID:      "memo.personal",
		Label:   "Personal",
		Icon:    "·",
		Parent:  "memo",
		Order:   2,
		Enabled: true,
	})
	r.Register(tuicore.NavItem{
		ID:      "memo.session",
		Label:   "Session",
		Icon:    "·",
		Parent:  "memo",
		Order:   3,
		Enabled: true,
	})
	r.Register(tuicore.NavItem{
		ID:      "memo.list",
		Label:   "All",
		Icon:    "·",
		Parent:  "memo",
		Order:   4,
		Enabled: true,
	})
}
