// view_repository_test.go - The repository view reports a failed load or month fetch
package tuisocial

import (
	"errors"
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestMain initializes the bubblezone global manager the card list requires.
func TestMain(m *testing.M) {
	zone.NewGlobal()
	os.Exit(m.Run())
}

// TestRepositoryFailuresSurface checks that a failed post load and a failed month fetch both reach the status bar.
func TestRepositoryFailuresSurface(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
		want string
	}{
		{"load", repositoryLoadedMsg{Err: errors.New("get posts: cache closed")}, "get posts: cache closed"},
		{"fetch", repositoryFetchedMsg{Err: errors.New("fetch range: host unreachable")}, "fetch range: host unreachable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := newRepositoryView(t.TempDir())
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
