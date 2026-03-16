// nav.go - Release extension navigation registration
package release

import "github.com/gitsocial-org/gitsocial/tui/tuicore"

// RegisterNavItems registers release extension navigation items.
func RegisterNavItems(r *tuicore.NavRegistry) {
	r.Register(tuicore.NavItem{
		ID:      "release",
		Label:   "Release",
		Icon:    "⏏",
		Order:   3,
		Enabled: true,
	})
}
