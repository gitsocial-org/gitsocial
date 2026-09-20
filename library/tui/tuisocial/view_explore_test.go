// view_explore_test.go - The explore view reports a failed load
package tuisocial

import (
	"errors"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestExploreLoadFailureSurfaces checks that a failed repository listing reaches the status bar.
func TestExploreLoadFailureSurfaces(t *testing.T) {
	view := newExploreView(t.TempDir())
	state := &tuicore.State{}
	view.Update(exploreLoadedMsg{err: errors.New("get repositories: cache closed")}, state)
	if state.Message != "get repositories: cache closed" {
		t.Errorf("Message = %q, want the error text", state.Message)
	}
	if state.MessageType != tuicore.MessageTypeError {
		t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
	}
	if !view.loaded {
		t.Error("a failed load left the view loading; the panel would keep saying Loading")
	}
}
