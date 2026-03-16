// nav_registry_test.go - Tests for cross-extension navigation routing
package tuicore

import (
	"testing"
	"time"
)

// mockDisplayItem implements DisplayItem for testing
type mockDisplayItem struct {
	id       string
	itemType ItemType
}

func (m mockDisplayItem) ItemID() string                    { return m.id }
func (m mockDisplayItem) ItemType() ItemType                { return m.itemType }
func (m mockDisplayItem) ToCard(resolver ItemResolver) Card { return Card{} }
func (m mockDisplayItem) Timestamp() time.Time              { return time.Time{} }
func (m mockDisplayItem) IsDimmed() bool                    { return false }

func TestGetNavTarget_ExactMatch(t *testing.T) {
	// Register a specific target
	RegisterNavTarget(
		ItemType{Extension: "test", Type: "specific"},
		func(id string) Location { return Location{Path: "/test/specific", Params: map[string]string{"id": id}} },
	)

	item := mockDisplayItem{id: "item123", itemType: ItemType{Extension: "test", Type: "specific"}}
	loc := GetNavTarget(item)

	if loc.Path != "/test/specific" {
		t.Errorf("Expected path /test/specific, got %s", loc.Path)
	}
	if loc.Params["id"] != "item123" {
		t.Errorf("Expected id param item123, got %s", loc.Params["id"])
	}
}

func TestGetNavTarget_WildcardFallback(t *testing.T) {
	// Register a wildcard target
	RegisterNavTarget(
		ItemType{Extension: "wildcard", Type: "*"},
		func(id string) Location {
			return Location{Path: "/wildcard/detail", Params: map[string]string{"id": id}}
		},
	)

	// Test with a type that has no exact match but extension matches
	item := mockDisplayItem{id: "item456", itemType: ItemType{Extension: "wildcard", Type: "unknown"}}
	loc := GetNavTarget(item)

	if loc.Path != "/wildcard/detail" {
		t.Errorf("Expected wildcard fallback path /wildcard/detail, got %s", loc.Path)
	}
}

func TestGetNavTarget_UltimateFallback(t *testing.T) {
	// Test with completely unknown extension
	item := mockDisplayItem{id: "item789", itemType: ItemType{Extension: "unknown", Type: "thing"}}
	loc := GetNavTarget(item)

	// Should fall back to social detail
	if loc.Path != "/social/detail" {
		t.Errorf("Expected ultimate fallback /social/detail, got %s", loc.Path)
	}
}

func TestItemType_Equality(t *testing.T) {
	t1 := ItemType{Extension: "pm", Type: "issue"}
	t2 := ItemType{Extension: "pm", Type: "issue"}
	t3 := ItemType{Extension: "pm", Type: "milestone"}

	if t1 != t2 {
		t.Error("Identical ItemTypes should be equal")
	}
	if t1 == t3 {
		t.Error("Different ItemTypes should not be equal")
	}
}

func TestUniversalItem_ItemType(t *testing.T) {
	item := Item{
		ID:   "test123",
		Ext:  "pm",
		Type: "issue",
	}

	itemType := item.ItemType()
	if itemType.Extension != "pm" || itemType.Type != "issue" {
		t.Errorf("Expected {pm, issue}, got {%s, %s}", itemType.Extension, itemType.Type)
	}
}

func TestUniversalItem_CrossExtension(t *testing.T) {
	// A social comment on a PM issue should return the original's type
	item := Item{
		ID:           "comment123",
		Ext:          "social",
		Type:         "comment",
		OriginalExt:  "pm",
		OriginalType: "issue",
	}

	itemType := item.ItemType()
	if itemType.Extension != "pm" || itemType.Type != "issue" {
		t.Errorf("Cross-ext comment should return original type {pm, issue}, got {%s, %s}",
			itemType.Extension, itemType.Type)
	}
}

func TestGetItemToCardFunc_Fallback(t *testing.T) {
	// Unknown extension should fall back to social/* or default
	renderer := GetItemToCardFunc(ItemType{Extension: "unknown", Type: "thing"})
	if renderer == nil {
		t.Error("Should return a renderer even for unknown types")
	}

	// Should produce a card (either social fallback or default)
	card := renderer(nil, nil)
	if card.Header.Title == "" {
		t.Error("Fallback renderer should produce a card with a title")
	}
}

func TestGetDimmedCheckFunc_Default(t *testing.T) {
	// Unknown extension should return default (not dimmed)
	checker := GetDimmedCheckFunc(ItemType{Extension: "unknown", Type: "thing"})
	if checker == nil {
		t.Error("Should return a checker even for unknown types")
	}

	// Default should return false
	if checker(nil) {
		t.Error("Default dimmed checker should return false")
	}
}
