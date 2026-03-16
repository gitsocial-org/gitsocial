// util_router_test.go - Tests for navigation router and location utilities
package tuicore

import (
	"testing"
)

func TestLocation_Param(t *testing.T) {
	loc := Location{Path: "/test", Params: map[string]string{"key": "value"}}
	if got := loc.Param("key"); got != "value" {
		t.Errorf("Param(key) = %q, want %q", got, "value")
	}
	if got := loc.Param("missing"); got != "" {
		t.Errorf("Param(missing) = %q, want empty", got)
	}
}

func TestLocation_Param_nilParams(t *testing.T) {
	loc := Location{Path: "/test"}
	if got := loc.Param("key"); got != "" {
		t.Errorf("Param(nil map) = %q, want empty", got)
	}
}

func TestLocation_Is(t *testing.T) {
	loc := Location{Path: "/social/timeline"}
	if !loc.Is("/social/timeline") {
		t.Error("Is() should match exact path")
	}
	if loc.Is("/social/detail") {
		t.Error("Is() should not match different path")
	}
}

func TestLocation_HasPrefix(t *testing.T) {
	loc := Location{Path: "/social/timeline"}
	if !loc.HasPrefix("/social") {
		t.Error("HasPrefix(/social) should match")
	}
	if loc.HasPrefix("/pm") {
		t.Error("HasPrefix(/pm) should not match")
	}
}

func TestRouter_Push(t *testing.T) {
	r := NewRouter(Location{Path: "/start"})
	if r.Location().Path != "/start" {
		t.Errorf("initial path = %q", r.Location().Path)
	}

	r.Push(Location{Path: "/second"})
	if r.Location().Path != "/second" {
		t.Errorf("after push path = %q", r.Location().Path)
	}
}

func TestRouter_Back(t *testing.T) {
	r := NewRouter(Location{Path: "/start"})
	r.Push(Location{Path: "/second"})
	r.Push(Location{Path: "/third"})

	if !r.Back() {
		t.Error("Back() should return true")
	}
	if r.Location().Path != "/second" {
		t.Errorf("after first back = %q, want /second", r.Location().Path)
	}

	if !r.Back() {
		t.Error("Back() should return true")
	}
	if r.Location().Path != "/start" {
		t.Errorf("after second back = %q, want /start", r.Location().Path)
	}

	if r.Back() {
		t.Error("Back() should return false when no history")
	}
}

func TestRouter_Replace(t *testing.T) {
	r := NewRouter(Location{Path: "/start"})
	r.Push(Location{Path: "/second"})
	r.Replace(Location{Path: "/replaced"})

	if r.Location().Path != "/replaced" {
		t.Errorf("after replace = %q", r.Location().Path)
	}

	// Back should go to /start, not /second
	r.Back()
	if r.Location().Path != "/start" {
		t.Errorf("after back = %q, want /start", r.Location().Path)
	}
}

func TestRouter_Back_empty(t *testing.T) {
	r := NewRouter(Location{Path: "/start"})
	if r.Back() {
		t.Error("Back() should return false with no history")
	}
	if r.Location().Path != "/start" {
		t.Errorf("path should not change, got %q", r.Location().Path)
	}
}

func TestLocDetail(t *testing.T) {
	loc := LocDetail("abc123")
	if loc.Path != "/social/detail" {
		t.Errorf("Path = %q", loc.Path)
	}
	if loc.Param("postID") != "abc123" {
		t.Errorf("postID = %q", loc.Param("postID"))
	}
}

func TestLocRepository(t *testing.T) {
	loc := LocRepository("https://github.com/user/repo", "main")
	if loc.Path != "/social/repository" {
		t.Errorf("Path = %q", loc.Path)
	}
	if loc.Param("url") != "https://github.com/user/repo" {
		t.Errorf("url = %q", loc.Param("url"))
	}
	if loc.Param("branch") != "main" {
		t.Errorf("branch = %q", loc.Param("branch"))
	}
}

func TestLocRepository_noBranch(t *testing.T) {
	loc := LocRepository("https://github.com/user/repo", "")
	if loc.Param("branch") != "" {
		t.Errorf("branch = %q, want empty", loc.Param("branch"))
	}
}

func TestLocSearchQuery(t *testing.T) {
	loc := LocSearchQuery("hello world")
	if loc.Path != "/search" {
		t.Errorf("Path = %q", loc.Path)
	}
	if loc.Param("q") != "hello world" {
		t.Errorf("q = %q", loc.Param("q"))
	}
}

func TestLocPMIssueDetail(t *testing.T) {
	loc := LocPMIssueDetail("issue123")
	if loc.Path != "/pm/issue" {
		t.Errorf("Path = %q", loc.Path)
	}
	if loc.Param("issueID") != "issue123" {
		t.Errorf("issueID = %q", loc.Param("issueID"))
	}
}

func TestLocReviewPRDetail(t *testing.T) {
	loc := LocReviewPRDetail("pr456")
	if loc.Path != "/review/pr" {
		t.Errorf("Path = %q", loc.Path)
	}
	if loc.Param("prID") != "pr456" {
		t.Errorf("prID = %q", loc.Param("prID"))
	}
}

func TestLocReviewFeedbackInline(t *testing.T) {
	loc := LocReviewFeedbackInline("pr1", "main.go", 10, 15, "abc")
	if loc.Path != "/review/feedback" {
		t.Errorf("Path = %q", loc.Path)
	}
	if loc.Param("file") != "main.go" {
		t.Errorf("file = %q", loc.Param("file"))
	}
	if loc.Param("oldLine") != "10" {
		t.Errorf("oldLine = %q", loc.Param("oldLine"))
	}
	if loc.Param("newLine") != "15" {
		t.Errorf("newLine = %q", loc.Param("newLine"))
	}
}

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
