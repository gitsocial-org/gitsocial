// view_notifications_test.go - The notifications view reports a failed load
package tuiviews

import (
	"errors"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestNotificationsLoadFailureSurfaces checks that a failed notifications load reaches the status bar.
func TestNotificationsLoadFailureSurfaces(t *testing.T) {
	view := NewNotificationsView(t.TempDir(), nil, nil, nil, nil)
	view.loading = true
	state := &tuicore.State{}
	view.Update(NotificationsLoadedMsg{Err: errors.New("get notifications: cache closed")}, state)
	if state.Message != "get notifications: cache closed" {
		t.Errorf("Message = %q, want the error text", state.Message)
	}
	if state.MessageType != tuicore.MessageTypeError {
		t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
	}
	if view.loading {
		t.Error("a failed load left the view loading; the panel would keep saying Loading")
	}
}
