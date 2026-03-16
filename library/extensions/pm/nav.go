// nav.go - Navigation registry items and view metadata for PM extension
package pm

import "github.com/gitsocial-org/gitsocial/tui/tuicore"

// RegisterNavItems registers PM extension navigation items.
func RegisterNavItems(r *tuicore.NavRegistry, workdir string) {
	// PM domain (order 1 - after social)
	r.Register(tuicore.NavItem{
		ID:      "pm",
		Label:   "PM",
		Icon:    "▢",
		Order:   1,
		Enabled: true,
	})

	// Boards sub-item (always visible)
	r.Register(tuicore.NavItem{
		ID:      "pm.board",
		Label:   "Boards",
		Icon:    "▦",
		Parent:  "pm",
		Order:   0,
		Enabled: true,
	})

	// Issues sub-item (always visible)
	r.Register(tuicore.NavItem{
		ID:      "pm.issues",
		Label:   "Issues",
		Icon:    "○",
		Parent:  "pm",
		Order:   1,
		Enabled: true,
	})

	// Milestones and sprints are dynamic (framework-dependent)
	UpdatePMNavItems(r, workdir)
}

// UpdatePMNavItems refreshes framework-dependent nav items (milestones/sprints).
func UpdatePMNavItems(r *tuicore.NavRegistry, workdir string) {
	hasMilestones, hasSprints := FrameworkFeatures(workdir)
	var items []tuicore.NavItem
	if hasMilestones {
		items = append(items, tuicore.NavItem{
			ID:      "pm.milestones",
			Label:   "Milestones",
			Icon:    "◇",
			Parent:  "pm",
			Order:   2,
			Enabled: true,
		})
	}
	if hasSprints {
		items = append(items, tuicore.NavItem{
			ID:      "pm.sprints",
			Label:   "Sprints",
			Icon:    "◷",
			Parent:  "pm",
			Order:   3,
			Enabled: true,
		})
	}
	r.RegisterDynamic("pm", items)
}
