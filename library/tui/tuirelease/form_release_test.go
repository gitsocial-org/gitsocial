// form_release_test.go - The release edit form reports a failed load before going back
package tuirelease

import (
	"errors"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestReleaseEditFormLoadFailureSurfaces checks that the edit form says why it bounced back.
func TestReleaseEditFormLoadFailureSurfaces(t *testing.T) {
	view := newReleaseEditFormView(t.TempDir())
	state := &tuicore.State{}
	view.Update(releaseEditFormLoadedMsg{Err: errors.New("release not found")}, state)
	if state.Message != "release not found" {
		t.Errorf("Message = %q, want the error text", state.Message)
	}
	if state.MessageType != tuicore.MessageTypeError {
		t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
	}
}
