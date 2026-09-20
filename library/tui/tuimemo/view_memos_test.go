// view_memos_test.go - The memo list reports a failed load
package tuimemo

import (
	"errors"
	"os"
	"testing"

	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestMain initializes the bubblezone global manager the card list requires.
func TestMain(m *testing.M) {
	zone.NewGlobal()
	os.Exit(m.Run())
}

// TestMemosLoadFailureSurfaces checks that a failed memo load reaches the status bar.
func TestMemosLoadFailureSurfaces(t *testing.T) {
	view := newMemosView(t.TempDir())
	state := &tuicore.State{}
	view.Update(memosLoadedMsg{err: errors.New("list memos: no such tier")}, state)
	if state.Message != "list memos: no such tier" {
		t.Errorf("Message = %q, want the error text", state.Message)
	}
	if state.MessageType != tuicore.MessageTypeError {
		t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
	}
	if !view.loaded {
		t.Error("a failed load left the view loading; the panel would keep saying Loading")
	}
}
