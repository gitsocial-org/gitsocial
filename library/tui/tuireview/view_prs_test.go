// view_prs_test.go - The pull request list reports a failed load
package tuireview

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

// TestPRsLoadFailureSurfaces checks that a failed pull request load reaches the status bar.
func TestPRsLoadFailureSurfaces(t *testing.T) {
	view := newPRsView(t.TempDir())
	state := &tuicore.State{}
	view.Update(prsLoadedMsg{err: errors.New("get pull requests: cache closed")}, state)
	if state.Message != "get pull requests: cache closed" {
		t.Errorf("Message = %q, want the error text", state.Message)
	}
	if state.MessageType != tuicore.MessageTypeError {
		t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
	}
	if !view.loaded || view.pag.Loading {
		t.Errorf("loaded = %v, pagination loading = %v, want a finished load", view.loaded, view.pag.Loading)
	}
}
