// view_list_picker_test.go - The list picker reports a failed load or delete
package tuisocial

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestListPickerFailuresSurface checks that a failed lists load and a failed delete both reach the status bar.
func TestListPickerFailuresSurface(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
		want string
	}{
		{"load", ListsLoadedMsg{Err: errors.New("get lists: cache closed")}, "get lists: cache closed"},
		{"delete", listDeletedMsg{ListID: "team", Err: errors.New("delete list: ref is locked")}, "delete list: ref is locked"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := newListPickerView(t.TempDir())
			view.loading = true
			state := &tuicore.State{}
			view.Update(tt.msg, state)
			if state.Message != tt.want {
				t.Errorf("Message = %q, want %q", state.Message, tt.want)
			}
			if state.MessageType != tuicore.MessageTypeError {
				t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
			}
		})
	}
}
