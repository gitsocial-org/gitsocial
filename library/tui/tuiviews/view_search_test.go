// view_search_test.go - The search view reports a failed query
package tuiviews

import (
	"errors"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestSearchFailureSurfaces checks that a failed search reaches the status bar.
func TestSearchFailureSurfaces(t *testing.T) {
	view := NewSearchView(t.TempDir(), nil, nil)
	view.query = "needle"
	view.loading = true
	state := &tuicore.State{}
	view.Update(SearchResultsMsg{Query: "needle", Err: errors.New("search: unknown filter")}, state)
	if state.Message != "search: unknown filter" {
		t.Errorf("Message = %q, want the error text", state.Message)
	}
	if state.MessageType != tuicore.MessageTypeError {
		t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
	}
	if view.loading {
		t.Error("a failed search left the view searching; the panel would keep saying Searching")
	}
}
