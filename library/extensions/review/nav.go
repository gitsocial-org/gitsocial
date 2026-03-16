// nav.go - Navigation registry items and view metadata for review extension
package review

import "github.com/gitsocial-org/gitsocial/tui/tuicore"

// RegisterNavItems registers review extension navigation items.
func RegisterNavItems(r *tuicore.NavRegistry) {
	r.Register(tuicore.NavItem{
		ID:      "review",
		Label:   "Review",
		Icon:    "⑂",
		Order:   2,
		Enabled: true,
	})
	r.Register(tuicore.NavItem{
		ID:      "review.prs",
		Label:   "Pull Requests",
		Icon:    "⑂",
		Parent:  "review",
		Order:   0,
		Enabled: true,
	})
}
