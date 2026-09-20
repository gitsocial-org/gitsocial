// view_repository_lists_test.go - The repository lists view reports a failed load
package tuisocial

import (
	"errors"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestRepoListsLoadFailureSurfaces checks that a failed lists load reaches the status bar.
func TestRepoListsLoadFailureSurfaces(t *testing.T) {
	view := newRepoListsView(t.TempDir())
	view.loading = true
	state := &tuicore.State{}
	view.Update(repoListsLoadedMsg{Err: errors.New("get external lists: cache closed")}, state)
	if state.Message != "get external lists: cache closed" {
		t.Errorf("Message = %q, want the error text", state.Message)
	}
	if state.MessageType != tuicore.MessageTypeError {
		t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
	}
	if view.loading {
		t.Error("a failed load left the view loading; the panel would keep saying Loading")
	}
}
