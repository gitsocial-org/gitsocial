// view_releases_test.go - The release list reports a failed load
package tuirelease

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

// TestReleasesLoadFailureSurfaces checks that a failed release load reaches the status bar.
func TestReleasesLoadFailureSurfaces(t *testing.T) {
	view := newReleasesView(t.TempDir())
	state := &tuicore.State{}
	view.Update(releasesLoadedMsg{err: errors.New("get releases: branch missing")}, state)
	if state.Message != "get releases: branch missing" {
		t.Errorf("Message = %q, want the error text", state.Message)
	}
	if state.MessageType != tuicore.MessageTypeError {
		t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
	}
	if !view.loaded || view.pag.Loading {
		t.Errorf("loaded = %v, pagination loading = %v, want a finished load", view.loaded, view.pag.Loading)
	}
}
