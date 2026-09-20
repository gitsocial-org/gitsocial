// form_pr_test.go - The pull request edit form reports a failed load before going back
package tuireview

import (
	"errors"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestPREditFormLoadFailureSurfaces checks that the edit form says why it bounced back.
func TestPREditFormLoadFailureSurfaces(t *testing.T) {
	view := newPREditFormView(t.TempDir())
	state := &tuicore.State{}
	view.Update(prEditFormLoadedMsg{Err: errors.New("pull request not found")}, state)
	if state.Message != "pull request not found" {
		t.Errorf("Message = %q, want the error text", state.Message)
	}
	if state.MessageType != tuicore.MessageTypeError {
		t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
	}
}
