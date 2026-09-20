// form_edit_load_test.go - The pm edit forms report a failed load before going back
package tuipm

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestEditFormLoadFailuresSurface checks that an edit form says why it bounced back.
func TestEditFormLoadFailuresSurface(t *testing.T) {
	err := errors.New("issue not found")
	tests := []struct {
		name string
		view tuicore.View
		msg  tea.Msg
	}{
		{"issue", newIssueEditFormView(t.TempDir()), editFormLoadedMsg{Err: err}},
		{"milestone", newMilestoneEditFormView(t.TempDir()), milestoneEditFormLoadedMsg{Err: err}},
		{"sprint", newSprintEditFormView(t.TempDir()), sprintEditFormLoadedMsg{Err: err}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &tuicore.State{}
			tt.view.Update(tt.msg, state)
			if state.Message != err.Error() {
				t.Errorf("Message = %q, want the error text", state.Message)
			}
			if state.MessageType != tuicore.MessageTypeError {
				t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
			}
		})
	}
}
