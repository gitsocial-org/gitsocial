// view_branches_test.go - The branches view reports a failed load, labels the rows and opens the picked branch
package tuisocial

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// ansiCodes matches the escape sequences a render carries.
var ansiCodes = regexp.MustCompile(`\x1b\[[0-9;]*m|\x1b\]8;;[^\x1b]*\x1b\\`)

// loadedBranchesView returns a view over three branches of a repository, with main as the default.
func loadedBranchesView() *branchesView {
	view := newBranchesView()
	view.url = "https://github.com/user/repo"
	view.name = "user/repo"
	view.Update(branchesLoadedMsg{Default: "main", Branches: []cache.BranchSummary{
		{Name: "feature/dark", Commits: 3, LastTime: time.Now().Add(-time.Hour)},
		{Name: "main", Commits: 12, LastTime: time.Now().Add(-24 * time.Hour)},
		{Name: "gitmsg/social", Commits: 1, LastTime: time.Now().Add(-48 * time.Hour)},
	}}, &tuicore.State{})
	return view
}

// TestBranchesLoadFailureSurfaces checks that a failed branch load reaches the status bar.
func TestBranchesLoadFailureSurfaces(t *testing.T) {
	view := newBranchesView()
	view.loading = true
	state := &tuicore.State{}
	view.Update(branchesLoadedMsg{Err: errors.New("query branches: cache closed")}, state)
	if state.Message != "query branches: cache closed" {
		t.Errorf("Message = %q, want the error text", state.Message)
	}
	if state.MessageType != tuicore.MessageTypeError {
		t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
	}
	if view.loading {
		t.Error("a failed load left the view loading; the panel would keep saying Loading")
	}
}

// TestBranchesRender checks the all-branches row, the ahead label of a feature branch and the default mark.
func TestBranchesRender(t *testing.T) {
	view := loadedBranchesView()
	out := ansiCodes.ReplaceAllString(view.Render(&tuicore.State{Width: 120, Height: 40, Registry: tuicore.NewRegistry()}), "")
	for _, want := range []string{"All branches · 16 commits", "feature/dark · 3 ahead", "main · 12 commits", "default", "gitmsg/social · 1 commit"} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "gitmsg/social · 1 ahead") || strings.Contains(out, "main · 12 ahead") {
		t.Errorf("a content or default branch is labeled ahead:\n%s", out)
	}
}

// TestBranchesEnterReplacesLocation checks that Enter replaces the location with the Repository view on the picked branch, and on every branch from the first row.
func TestBranchesEnterReplacesLocation(t *testing.T) {
	view := loadedBranchesView()
	open := func(cursor int) tuicore.NavigateMsg {
		t.Helper()
		view.cursor = cursor
		cmd := view.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, &tuicore.State{})
		if cmd == nil {
			t.Fatalf("enter on row %d returned no command", cursor)
		}
		nav, ok := cmd().(tuicore.NavigateMsg)
		if !ok || nav.Action != tuicore.NavReplace {
			t.Fatalf("enter on row %d = %+v, want a NavReplace", cursor, nav)
		}
		return nav
	}
	if loc := open(2).Location; loc.Path != "/social/repository" || loc.Param("url") != view.url || loc.Param("branch") != "main" {
		t.Errorf("row 2 opens %+v, want the repository on main", loc)
	}
	if loc := open(0).Location; loc.Param("branch") != "" || loc.Param("url") != view.url {
		t.Errorf("row 0 opens %+v, want the repository on every branch", loc)
	}
}
