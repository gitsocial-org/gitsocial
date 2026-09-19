// registry_nav_test.go - Tests for navigation items and the navigation registry
package tuinav

import (
	"testing"
)

func TestNavRegistry_basic(t *testing.T) {
	r := NewNavRegistry()
	r.Register(NavItem{ID: "social", Label: "Social", Order: 1})
	r.Register(NavItem{ID: "pm", Label: "PM", Order: 2})

	items := r.GetTopLevel()
	if len(items) != 2 {
		t.Fatalf("len(GetTopLevel()) = %d, want 2", len(items))
	}
	if items[0].ID != "social" {
		t.Errorf("first item = %q, want social", items[0].ID)
	}
}

func TestNavRegistry_children(t *testing.T) {
	r := NewNavRegistry()
	r.Register(NavItem{ID: "social", Label: "Social", Order: 1})
	r.Register(NavItem{ID: "social.timeline", Label: "Timeline", Parent: "social", Order: 1})
	r.Register(NavItem{ID: "social.lists", Label: "Lists", Parent: "social", Order: 2})

	children := r.GetChildren("social")
	if len(children) != 2 {
		t.Fatalf("len(GetChildren) = %d, want 2", len(children))
	}
	if children[0].ID != "social.timeline" {
		t.Errorf("first child = %q", children[0].ID)
	}
}

func TestNavRegistry_hidden(t *testing.T) {
	r := NewNavRegistry()
	r.Register(NavItem{ID: "social", Label: "Social", Order: 1})
	r.Register(NavItem{ID: "pm", Label: "PM", Order: 2})

	r.SetHidden("pm", true)
	if !r.IsHidden("pm") {
		t.Error("pm should be hidden")
	}

	items := r.GetTopLevel()
	if len(items) != 1 {
		t.Fatalf("len(GetTopLevel) = %d, want 1", len(items))
	}
	if items[0].ID != "social" {
		t.Errorf("visible item = %q", items[0].ID)
	}

	r.SetHidden("pm", false)
	if r.IsHidden("pm") {
		t.Error("pm should not be hidden after unhide")
	}
}

func TestNavRegistry_Get(t *testing.T) {
	r := NewNavRegistry()
	r.Register(NavItem{ID: "social", Label: "Social"})

	got := r.Get("social")
	if got == nil {
		t.Fatal("Get(social) returned nil")
	}
	if got.Label != "Social" {
		t.Errorf("Label = %q", got.Label)
	}

	if r.Get("nonexistent") != nil {
		t.Error("Get(nonexistent) should return nil")
	}
}

func TestNavRegistry_HasChildren(t *testing.T) {
	r := NewNavRegistry()
	r.Register(NavItem{ID: "social", Label: "Social"})
	r.Register(NavItem{ID: "social.timeline", Label: "Timeline", Parent: "social"})

	if !r.HasChildren("social") {
		t.Error("HasChildren(social) should be true")
	}
	if r.HasChildren("social.timeline") {
		t.Error("HasChildren(social.timeline) should be false")
	}
}

func TestNavRegistry_dynamic(t *testing.T) {
	r := NewNavRegistry()
	r.Register(NavItem{ID: "social", Label: "Social"})
	r.RegisterDynamic("social", []NavItem{
		{ID: "social.list.1", Label: "My List", Parent: "social", Order: 10},
	})

	children := r.GetChildren("social")
	if len(children) != 1 {
		t.Fatalf("len(GetChildren) = %d, want 1", len(children))
	}
	if children[0].ID != "social.list.1" {
		t.Errorf("dynamic child = %q", children[0].ID)
	}

	r.ClearDynamic("social")
	children = r.GetChildren("social")
	if len(children) != 0 {
		t.Errorf("after clear, len(GetChildren) = %d, want 0", len(children))
	}
}

func TestNavItem_IsTopLevel(t *testing.T) {
	top := NavItem{ID: "social", Label: "Social"}
	if !top.IsTopLevel() {
		t.Error("item without parent should be top-level")
	}

	child := NavItem{ID: "social.timeline", Parent: "social"}
	if child.IsTopLevel() {
		t.Error("item with parent should not be top-level")
	}
}
