// view_memo_detail_test.go - The memo detail view reports a failed load or retract
package tuimemo

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestMemoDetailFailuresSurface checks that a failed load and a failed retract both reach the status bar.
func TestMemoDetailFailuresSurface(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
		want string
	}{
		{"load", memoDetailLoadedMsg{err: errors.New("memo not found")}, "memo not found"},
		{"retract", memoRetractedMsg{err: errors.New("retract memo: read-only tier")}, "retract memo: read-only tier"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := newMemoDetailView(t.TempDir())
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
