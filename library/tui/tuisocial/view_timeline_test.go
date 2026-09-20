// view_timeline_test.go - The timeline reports a failed load
package tuisocial

import (
	"errors"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestTimelineLoadFailureSurfaces checks that a failed timeline load reaches the status bar.
func TestTimelineLoadFailureSurfaces(t *testing.T) {
	view := newTimelineView(t.TempDir(), "", false)
	view.pag.StartLoading()
	state := &tuicore.State{}
	view.Update(TimelineLoadedMsg{Err: errors.New("get posts: cache closed")}, state)
	if state.Message != "get posts: cache closed" {
		t.Errorf("Message = %q, want the error text", state.Message)
	}
	if state.MessageType != tuicore.MessageTypeError {
		t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
	}
	if view.pag.Loading {
		t.Error("a failed load left the pagination loading; the footer would keep saying Loading")
	}
}
