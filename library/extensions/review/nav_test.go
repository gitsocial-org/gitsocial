// nav_test.go - Tests for review navigation registration
package review

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

func TestRegisterNavItems(t *testing.T) {
	r := tuicore.NewNavRegistry()
	RegisterNavItems(r)

	item := r.Get("review")
	if item == nil {
		t.Fatal("review nav item not found")
	}
	if item.Label != "Review" {
		t.Errorf("Label = %q, want Review", item.Label)
	}
	if item.Order != 2 {
		t.Errorf("Order = %d, want 2", item.Order)
	}
	if !item.Enabled {
		t.Error("Enabled should be true")
	}

	child := r.Get("review.prs")
	if child == nil {
		t.Fatal("review.prs nav item not found")
	}
	if child.Label != "Pull Requests" {
		t.Errorf("Label = %q, want Pull Requests", child.Label)
	}
	if child.Parent != "review" {
		t.Errorf("Parent = %q, want review", child.Parent)
	}
}
