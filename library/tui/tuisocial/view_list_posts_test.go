// view_list_posts_test.go - The list posts view reports a failed load
package tuisocial

import (
	"errors"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestListPostsLoadFailureSurfaces checks that a failed list load reaches the status bar.
func TestListPostsLoadFailureSurfaces(t *testing.T) {
	view := newListPostsView(t.TempDir())
	view.pag.StartLoading()
	state := &tuicore.State{}
	view.Update(listPostsLoadedMsg{Err: errors.New("get posts: unknown list")}, state)
	if state.Message != "get posts: unknown list" {
		t.Errorf("Message = %q, want the error text", state.Message)
	}
	if state.MessageType != tuicore.MessageTypeError {
		t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
	}
	if view.pag.Loading {
		t.Error("a failed load left the pagination loading; the panel would keep saying Loading")
	}
}
