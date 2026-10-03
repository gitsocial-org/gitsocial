// view_tags_test.go - The tags view reports a failed load, labels the rows and opens a release or the tagged commit
package tuisocial

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// loadedTagsView returns a view over two tags, the newer one named by a release.
func loadedTagsView() *tagsView {
	view := newTagsView()
	view.url, view.name = "https://github.com/user/repo", "user/repo"
	now := time.Now().Unix()
	view.Update(tagsLoadedMsg{
		Tags: []tagRow{
			{Tag: git.Tag{Name: "v1.0.0", Commit: "aaa", Author: "Alice", Time: now - 3600}, Prev: "v0.9.0", Count: 4},
			{Tag: git.Tag{Name: "v0.9.0", Commit: "bbb", Author: "Bob", Time: now - 86400}, Count: 12},
		},
		Releases: map[string]string{"1.0.0": "cafe12345678"},
	}, &tuicore.State{})
	return view
}

// TestTagsLoadFailureSurfaces checks that a failed tag read reaches the status bar.
func TestTagsLoadFailureSurfaces(t *testing.T) {
	view := newTagsView()
	view.loading = true
	state := &tuicore.State{}
	view.Update(tagsLoadedMsg{Err: errors.New("list tags: no clone")}, state)
	if state.Message != "list tags: no clone" || state.MessageType != tuicore.MessageTypeError || view.loading {
		t.Errorf("message = %q (%v), loading = %v", state.Message, state.MessageType, view.loading)
	}
}

// TestTagsRender checks the count since the previous tag, the history count of the oldest tag, the author and the release mark.
func TestTagsRender(t *testing.T) {
	out := ansiCodes.ReplaceAllString(loadedTagsView().Render(renderState()), "")
	for _, want := range []string{"▸ v1.0.0 · 4 commits since v0.9.0", "Alice · release", "v0.9.0 · 12 commits", "Bob"} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Bob · release") {
		t.Errorf("a tag with no release carries the mark:\n%s", out)
	}
}

// TestTagsEnterOpensReleaseOrCommit checks that Enter opens the release of a tag, the diff of a tag without one, and c the code at the tag.
func TestTagsEnterOpensReleaseOrCommit(t *testing.T) {
	view := loadedTagsView()
	press := func(cursor int, key tea.KeyPressMsg) tuicore.NavigateMsg {
		t.Helper()
		view.cursor = cursor
		cmd := view.Update(key, &tuicore.State{})
		if cmd == nil {
			t.Fatalf("row %d returned no command", cursor)
		}
		nav, ok := cmd().(tuicore.NavigateMsg)
		if !ok {
			t.Fatalf("row %d = %T, want a NavigateMsg", cursor, cmd())
		}
		return nav
	}
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	if loc := press(0, enter).Location; loc.Path != "/release/detail" || loc.Param("releaseID") != "cafe12345678" {
		t.Errorf("v1.0.0 opens %+v, want its release", loc)
	}
	if loc := press(1, enter).Location; loc.Path != "/diff" || loc.Param("commit") != "refs/tags/v0.9.0" {
		t.Errorf("v0.9.0 opens %+v, want the diff of the tagged commit", loc)
	}
	if loc := press(1, tea.KeyPressMsg{Code: 'c', Text: "c"}).Location; loc.Path != "/social/repository/code" || loc.Param("branch") != "v0.9.0" || loc.Param("url") != view.url {
		t.Errorf("c opens %+v, want the code view at v0.9.0", loc)
	}
}
