// nav.go - Navigation registry items and view metadata for PM extension
package pm

import "github.com/gitsocial-org/gitsocial/library/tui/tuinav"

// RegisterNavItems registers PM extension navigation items.
func RegisterNavItems(r *tuinav.NavRegistry, workdir string) {
	// PM domain (order 1 - after social)
	r.Register(tuinav.NavItem{
		ID:      "pm",
		Label:   "PM",
		Icon:    "▢",
		Order:   1,
		Enabled: true,
	})

	// Boards sub-item (always visible)
	r.Register(tuinav.NavItem{
		ID:      "pm.board",
		Label:   "Boards",
		Icon:    "▦",
		Parent:  "pm",
		Order:   0,
		Enabled: true,
	})

	// Issues sub-item (always visible)
	r.Register(tuinav.NavItem{
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
func UpdatePMNavItems(r *tuinav.NavRegistry, workdir string) {
	hasMilestones, hasSprints := FrameworkFeatures(workdir)
	var items []tuinav.NavItem
	if hasMilestones {
		items = append(items, tuinav.NavItem{
			ID:      "pm.milestones",
			Label:   "Milestones",
			Icon:    "◇",
			Parent:  "pm",
			Order:   2,
			Enabled: true,
		})
	}
	if hasSprints {
		items = append(items, tuinav.NavItem{
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
