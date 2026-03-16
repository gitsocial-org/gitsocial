// view_registry.go - Centralized view metadata registry for context, title, and nav mapping
package tuicore

import "sync"

// ViewMeta describes a view's metadata for routing, titles, and navigation.
type ViewMeta struct {
	Path      string  // Route path (e.g., "/social/timeline")
	Context   Context // Context for keybindings
	Title     string  // Default title text
	Icon      string  // Icon prefix for title
	NavItemID string  // Nav panel selection ID
	ShowFetch bool    // Whether to show fetch info in title
	Component string  // Shared component type ("CardList", "SectionList", "VersionPicker", "")
}

var (
	viewRegistry   = make(map[string]ViewMeta)
	viewMetaOrder  []ViewMeta // preserves registration order
	viewRegistryMu sync.RWMutex
)

// RegisterViewMeta registers view metadata for a path.
// Extensions call this to declare their views.
func RegisterViewMeta(meta ViewMeta) {
	viewRegistryMu.Lock()
	defer viewRegistryMu.Unlock()
	viewRegistry[meta.Path] = meta
	viewMetaOrder = append(viewMetaOrder, meta)
}

// AllViewMetas returns all registered ViewMeta in registration order.
func AllViewMetas() []ViewMeta {
	viewRegistryMu.RLock()
	defer viewRegistryMu.RUnlock()
	result := make([]ViewMeta, len(viewMetaOrder))
	copy(result, viewMetaOrder)
	return result
}

// GetViewMeta returns metadata for a path.
func GetViewMeta(path string) (ViewMeta, bool) {
	viewRegistryMu.RLock()
	defer viewRegistryMu.RUnlock()
	meta, ok := viewRegistry[path]
	return meta, ok
}

// GetContextForPath returns the context for a given path.
func GetContextForPath(path string) Context {
	if meta, ok := GetViewMeta(path); ok {
		return meta.Context
	}
	return Timeline // default
}

// GetNavItemIDForPath returns the nav item ID for a given path.
func GetNavItemIDForPath(path string) string {
	if meta, ok := GetViewMeta(path); ok && meta.NavItemID != "" {
		return meta.NavItemID
	}
	return "social.timeline" // default
}

// registerCoreViews registers core view metadata.
func init() {
	RegisterViewMeta(ViewMeta{Path: "/analytics", Context: Analytics, Title: "Analytics", Icon: "◧", NavItemID: "_analytics"})
	RegisterViewMeta(ViewMeta{Path: "/settings", Context: Settings, Title: "Settings", Icon: "⌨", NavItemID: "settings"})
	RegisterViewMeta(ViewMeta{Path: "/config", Context: Config, Title: "Config", Icon: "※", NavItemID: "config.core"})
	RegisterViewMeta(ViewMeta{Path: "/cache", Context: Cache, Title: "Cache", Icon: "⛁", NavItemID: "cache"})
	RegisterViewMeta(ViewMeta{Path: "/help", Context: Help, Title: "Help", Icon: "?", NavItemID: "_help"})
	RegisterViewMeta(ViewMeta{Path: "/errorlog", Context: ErrorLog, Title: "Error Log", Icon: "⚠", NavItemID: "_errorlog"})
}
