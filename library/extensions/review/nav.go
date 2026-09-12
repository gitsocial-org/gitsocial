// nav.go - Navigation registry items and view metadata for review extension
package review

import "github.com/gitsocial-org/gitsocial/library/tui/tuinav"

// RegisterNavItems registers review extension navigation items.
func RegisterNavItems(r *tuinav.NavRegistry) {
	r.Register(tuinav.NavItem{
		ID:      "review",
		Label:   "Review",
		Icon:    "⑂",
		Order:   2,
		Enabled: true,
	})
	r.Register(tuinav.NavItem{
		ID:      "review.prs",
		Label:   "Pull Requests",
		Icon:    "⑂",
		Parent:  "review",
		Order:   0,
		Enabled: true,
	})
}
