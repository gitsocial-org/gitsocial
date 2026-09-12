// nav.go - Release extension navigation registration
package release

import "github.com/gitsocial-org/gitsocial/library/tui/tuinav"

// RegisterNavItems registers release extension navigation items.
func RegisterNavItems(r *tuinav.NavRegistry) {
	r.Register(tuinav.NavItem{
		ID:      "release",
		Label:   "Release",
		Icon:    "⏏",
		Order:   3,
		Enabled: true,
	})
}
