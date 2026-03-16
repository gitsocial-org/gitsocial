// nav_registry.go - Navigation target registry for cross-extension routing
package tuicore

import "sync"

// NavTarget returns a Location for navigating to an item by ID
type NavTarget func(itemID string) Location

// navTargetRegistry maps item types to navigation targets
type navTargetRegistry struct {
	mu      sync.RWMutex
	targets map[ItemType]NavTarget
}

var globalNavTargets = &navTargetRegistry{targets: make(map[ItemType]NavTarget)}

// RegisterNavTarget registers a navigation target for an item type.
// Use Type: "*" as a wildcard fallback for an extension.
func RegisterNavTarget(itemType ItemType, target NavTarget) {
	globalNavTargets.mu.Lock()
	defer globalNavTargets.mu.Unlock()
	globalNavTargets.targets[itemType] = target
}

// GetNavTarget returns the navigation location for an item.
// Lookup order: exact match, extension wildcard, fallback to social detail.
// For cross-extension comments, the comment's ID is preserved as a "focusID"
// param so the detail view can auto-scroll to it.
func GetNavTarget(item DisplayItem) Location {
	globalNavTargets.mu.RLock()
	defer globalNavTargets.mu.RUnlock()

	itemType := item.ItemType()

	// For cross-extension items (e.g., social comment on a PR), use the
	// original item's ID so the detail view receives the correct reference.
	navID := item.ItemID()
	commentID := ""
	if ui, ok := item.(Item); ok && ui.OriginalID != "" {
		commentID = navID
		navID = ui.OriginalID
	}

	var loc Location
	if target, ok := globalNavTargets.targets[itemType]; ok {
		loc = target(navID)
	} else if target, ok := globalNavTargets.targets[ItemType{Extension: itemType.Extension, Type: "*"}]; ok {
		loc = target(navID)
	} else {
		loc = LocDetail(navID)
	}

	// Preserve the comment ID so the detail view can focus on it
	if commentID != "" {
		if loc.Params == nil {
			loc.Params = make(map[string]string)
		}
		loc.Params["focusID"] = commentID
	}
	return loc
}
