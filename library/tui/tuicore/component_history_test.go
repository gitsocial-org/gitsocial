// component_history_test.go - The shared history view reports a failed load
package tuicore

import (
	"errors"
	"testing"
)

// TestHistoryLoadFailureSurfaces checks that a failed version load reaches the status bar.
func TestHistoryLoadFailureSurfaces(t *testing.T) {
	view := NewHistoryView(t.TempDir(), HistoryConfig{ParamName: "issueID", TitleLabel: "History"})
	view.picker.SetLoading(true)
	state := &State{}
	view.Update(historyLoadedMsg{param: "issueID", err: errors.New("get history: cache closed")}, state)
	if state.Message != "get history: cache closed" {
		t.Errorf("Message = %q, want the error text", state.Message)
	}
	if state.MessageType != MessageTypeError {
		t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
	}
	if view.picker.IsLoading() {
		t.Error("a failed load left the picker loading; the panel would keep saying Loading")
	}
}
