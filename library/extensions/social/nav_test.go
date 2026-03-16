// nav_test.go - Tests for TUI navigation registration
package social

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

func TestRegisterNavItems(t *testing.T) {
	r := tuicore.NewNavRegistry()
	RegisterNavItems(r)

	social := r.Get("social")
	if social == nil {
		t.Fatal("social nav item should be registered")
	}
	if social.Label != "Social" {
		t.Errorf("social.Label = %q, want Social", social.Label)
	}
	if social.Enabled {
		t.Error("social should not be enabled (it's a section header)")
	}

	timeline := r.Get("social.timeline")
	if timeline == nil {
		t.Fatal("social.timeline should be registered")
	}
	if timeline.Parent != "social" {
		t.Errorf("social.timeline.Parent = %q, want social", timeline.Parent)
	}
	if !timeline.Enabled {
		t.Error("social.timeline should be enabled")
	}

	myrepo := r.Get("social.myrepo")
	if myrepo == nil {
		t.Fatal("social.myrepo should be registered")
	}

	lists := r.Get("social.lists")
	if lists == nil {
		t.Fatal("social.lists should be registered")
	}
}

func TestUpdateListItems(t *testing.T) {
	r := tuicore.NewNavRegistry()
	RegisterNavItems(r)

	lists := []List{
		{ID: "friends", Name: "Friends"},
		{ID: "devs", Name: ""},
	}
	UpdateListItems(r, lists)

	friendsItem := r.Get("social.lists.friends")
	if friendsItem == nil {
		t.Fatal("social.lists.friends should be registered")
	}
	if friendsItem.Label != "Friends" {
		t.Errorf("friends label = %q, want Friends", friendsItem.Label)
	}

	devsItem := r.Get("social.lists.devs")
	if devsItem == nil {
		t.Fatal("social.lists.devs should be registered")
	}
	if devsItem.Label != "devs" {
		t.Error("empty name should fall back to ID")
	}
}

func TestUpdateListItems_empty(t *testing.T) {
	r := tuicore.NewNavRegistry()
	RegisterNavItems(r)
	UpdateListItems(r, nil)
	// Should not panic; no dynamic items registered
}
