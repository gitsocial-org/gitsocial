// registry_nav.go - Navigation items and the registry the TUI and extensions register into
package tuinav

import "sort"

// NavItem represents a navigation entry in the sidebar
type NavItem struct {
	ID      string // Unique identifier (e.g., "social", "social.timeline")
	Label   string // Display label
	Icon    string // Icon prefix (e.g., "")
	Parent  string // Parent ID ("" for top-level)
	Order   int    // Sort order within parent
	Enabled bool   // Whether item is implemented
}

// IsTopLevel returns true if this item has no parent.
func (n NavItem) IsTopLevel() bool {
	return n.Parent == ""
}

// NavRegistry manages navigation items with tree structure
type NavRegistry struct {
	items   []NavItem
	dynamic map[string][]NavItem // parentID -> dynamic children
	hidden  map[string]bool      // domain IDs hidden by user settings
	version int                  // incremented on mutations for cache invalidation
}

// NewNavRegistry creates a new navigation registry.
func NewNavRegistry() *NavRegistry {
	return &NavRegistry{
		items:   make([]NavItem, 0),
		dynamic: make(map[string][]NavItem),
		hidden:  make(map[string]bool),
	}
}

// SetHidden controls whether a top-level domain is hidden from navigation.
func (r *NavRegistry) SetHidden(domain string, hide bool) {
	if hide {
		r.hidden[domain] = true
	} else {
		delete(r.hidden, domain)
	}
	r.version++
}

// IsHidden returns true if a domain is hidden.
func (r *NavRegistry) IsHidden(domain string) bool {
	return r.hidden[domain]
}

// Register adds a static navigation item.
func (r *NavRegistry) Register(item NavItem) {
	r.items = append(r.items, item)
	r.version++
}

// RegisterDynamic replaces dynamic children under a parent for runtime items.
func (r *NavRegistry) RegisterDynamic(parentID string, items []NavItem) {
	r.dynamic[parentID] = items
	r.version++
}

// Version returns the current registry version for cache invalidation.
func (r *NavRegistry) Version() int {
	return r.version
}

// Get returns a navigation item by ID.
func (r *NavRegistry) Get(id string) *NavItem {
	for i := range r.items {
		if r.items[i].ID == id {
			return &r.items[i]
		}
	}
	for _, children := range r.dynamic {
		for i := range children {
			if children[i].ID == id {
				return &children[i]
			}
		}
	}
	return nil
}

// GetTopLevel returns all top-level items sorted by Order, excluding hidden domains.
func (r *NavRegistry) GetTopLevel() []NavItem {
	var result []NavItem
	for _, item := range r.items {
		if item.Parent == "" && !r.hidden[item.ID] {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Order < result[j].Order
	})
	return result
}

// GetChildren returns direct children of a parent, sorted by Order. Returns nil for hidden parents.
func (r *NavRegistry) GetChildren(parentID string) []NavItem {
	if r.hidden[parentID] {
		return nil
	}
	var result []NavItem
	// Static children
	for _, item := range r.items {
		if item.Parent == parentID {
			result = append(result, item)
		}
	}
	// Dynamic children
	if dyn, ok := r.dynamic[parentID]; ok {
		result = append(result, dyn...)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Order < result[j].Order
	})
	return result
}

// HasChildren returns true if the item has any children.
func (r *NavRegistry) HasChildren(id string) bool {
	if r.hidden[id] {
		return false
	}
	for _, item := range r.items {
		if item.Parent == id {
			return true
		}
	}
	if dyn, ok := r.dynamic[id]; ok && len(dyn) > 0 {
		return true
	}
	return false
}

// RegisterCoreNavItems registers the core navigation structure.
func RegisterCoreNavItems(r *NavRegistry) {
	// Unimplemented extensions (placeholders)
	// PM is registered by extensions/pm/nav.go
	// Release is registered by extensions/release/nav.go
	// Review is registered by extensions/review/nav.go
	r.Register(NavItem{ID: "cicd", Label: "CI/CD", Icon: "⚒", Order: 4, Enabled: false})
	r.Register(NavItem{ID: "infra", Label: "Infrastructure", Icon: "⛫", Order: 5, Enabled: false})
	r.Register(NavItem{ID: "ops", Label: "Operations", Icon: "⎈", Order: 6, Enabled: false})
	r.Register(NavItem{ID: "security", Label: "Security", Icon: "⛨", Order: 7, Enabled: false})
	r.Register(NavItem{ID: "portfolio", Label: "Portfolio", Icon: "⧉", Order: 8, Enabled: false})
	r.Register(NavItem{ID: "dm", Label: "DM", Icon: "✉", Order: 9, Enabled: false})

	// Config domain with sub-items (project/extension config only)
	r.Register(NavItem{ID: "config", Label: "Configuration", Icon: "⚙", Order: 10, Enabled: true})
	r.Register(NavItem{ID: "config.core", Label: "Core", Icon: "※", Parent: "config", Order: 0, Enabled: false})
	r.Register(NavItem{ID: "config.forks", Label: "Forks", Icon: "⑂", Parent: "config", Order: 1, Enabled: true})
	r.Register(NavItem{ID: "config.site", Label: "Site", Icon: "◱", Parent: "config", Order: 7, Enabled: true})
	r.Register(NavItem{ID: "config.social", Label: "Social", Icon: "⌘", Parent: "config", Order: 2, Enabled: true})
	r.Register(NavItem{ID: "config.pm", Label: "PM", Icon: "▢", Parent: "config", Order: 3, Enabled: true})
	r.Register(NavItem{ID: "config.release", Label: "Release", Icon: "⏏", Parent: "config", Order: 4, Enabled: true})
	r.Register(NavItem{ID: "config.review", Label: "Review", Icon: "⑂", Parent: "config", Order: 5, Enabled: true})
	r.Register(NavItem{ID: "config.memo", Label: "Memo", Icon: "☞", Parent: "config", Order: 6, Enabled: true})

	// Order 11 is intentionally reserved for the planned DM extension so its
	// nav slot doesn't shuffle Identity/Cache/Settings when added.
	// Top-level user concerns (Identity, Cache, Settings)
	r.Register(NavItem{ID: "identity", Label: "Identity", Icon: "⚿", Order: 12, Enabled: true})
	r.Register(NavItem{ID: "cache", Label: "Cache", Icon: "⛁", Order: 13, Enabled: true})
	r.Register(NavItem{ID: "settings", Label: "Settings", Icon: "⌨", Order: 14, Enabled: true})
}
