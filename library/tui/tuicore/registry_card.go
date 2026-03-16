// card_registry.go - Card renderer registry for extension-agnostic item display
package tuicore

import (
	"sync"

	"github.com/gitsocial-org/gitsocial/core/notifications"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// itemToCardRegistry maps ext/type to card renderers
type itemToCardRegistry struct {
	mu             sync.RWMutex
	renderers      map[ItemType]ItemToCardFunc
	dimmedCheckers map[ItemType]DimmedCheckFunc
}

var globalItemRenderers = &itemToCardRegistry{
	renderers:      make(map[ItemType]ItemToCardFunc),
	dimmedCheckers: make(map[ItemType]DimmedCheckFunc),
}

// RegisterCardRenderer registers a card renderer for an item type.
// Use Type: "*" as a wildcard fallback for an extension.
func RegisterCardRenderer(itemType ItemType, renderer ItemToCardFunc) {
	globalItemRenderers.mu.Lock()
	defer globalItemRenderers.mu.Unlock()
	globalItemRenderers.renderers[itemType] = renderer
}

// RegisterDimmedChecker registers a dimmed checker for an item type.
func RegisterDimmedChecker(itemType ItemType, checker DimmedCheckFunc) {
	globalItemRenderers.mu.Lock()
	defer globalItemRenderers.mu.Unlock()
	globalItemRenderers.dimmedCheckers[itemType] = checker
}

// GetItemToCardFunc returns a card renderer for the given item type.
// Lookup order: exact match, extension wildcard, basic fallback.
func GetItemToCardFunc(itemType ItemType) ItemToCardFunc {
	globalItemRenderers.mu.RLock()
	defer globalItemRenderers.mu.RUnlock()

	// Exact match
	if r, ok := globalItemRenderers.renderers[itemType]; ok {
		return r
	}

	// Extension wildcard (e.g., social/* for any social type)
	wildcard := ItemType{Extension: itemType.Extension, Type: "*"}
	if r, ok := globalItemRenderers.renderers[wildcard]; ok {
		return r
	}

	// Basic fallback: render minimal card
	return defaultItemToCard
}

// GetDimmedCheckFunc returns a dimmed checker for the given item type.
func GetDimmedCheckFunc(itemType ItemType) DimmedCheckFunc {
	globalItemRenderers.mu.RLock()
	defer globalItemRenderers.mu.RUnlock()

	// Exact match
	if c, ok := globalItemRenderers.dimmedCheckers[itemType]; ok {
		return c
	}

	// Extension wildcard
	wildcard := ItemType{Extension: itemType.Extension, Type: "*"}
	if c, ok := globalItemRenderers.dimmedCheckers[wildcard]; ok {
		return c
	}

	// Default: not dimmed
	return func(any) bool { return false }
}

func init() {
	RegisterCardRenderer(
		ItemType{Extension: "core", Type: "mention"},
		mentionNotificationToCard,
	)
	RegisterDimmedChecker(
		ItemType{Extension: "core", Type: "mention"},
		mentionIsDimmed,
	)
}

// mentionNotificationToCard renders a core mention notification to a Card.
func mentionNotificationToCard(data any, _ ItemResolver) Card {
	n, ok := data.(notifications.Notification)
	if !ok {
		return Card{Header: CardHeader{Title: "Invalid notification"}}
	}
	var subtitleParts []HeaderPart
	subtitleParts = append(subtitleParts, HeaderPart{Text: FormatTime(n.Timestamp)})
	if n.RepoURL != "" {
		subtitleParts = append(subtitleParts, HeaderPart{Text: protocol.GetFullDisplayName(n.RepoURL)})
	}
	return Card{
		Header: CardHeader{
			Icon:     "@",
			Title:    n.Actor.Name,
			Subtitle: subtitleParts,
			Badge:    "mentioned you",
		},
	}
}

// mentionIsDimmed checks if a mention notification should be dimmed.
func mentionIsDimmed(data any) bool {
	n, ok := data.(notifications.Notification)
	if !ok {
		return false
	}
	return n.IsRead
}

// defaultItemToCard provides basic rendering for unknown types
func defaultItemToCard(data any, resolver ItemResolver) Card {
	return Card{
		Header: CardHeader{
			Title:    "Unknown Item",
			Subtitle: []HeaderPart{{Text: "unrecognized type"}},
		},
		Content: CardContent{
			Text: "(no renderer registered)",
		},
	}
}
