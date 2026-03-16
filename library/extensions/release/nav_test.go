// nav_test.go - Tests for release navigation registration
package release

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

func TestRegisterNavItems(t *testing.T) {
	r := tuicore.NewNavRegistry()
	RegisterNavItems(r)

	item := r.Get("release")
	if item == nil {
		t.Fatal("release nav item not found")
	}
	if item.Label != "Release" {
		t.Errorf("Label = %q, want Release", item.Label)
	}
	if item.Order != 3 {
		t.Errorf("Order = %d, want 3", item.Order)
	}
	if !item.Enabled {
		t.Error("Enabled should be true")
	}
}
