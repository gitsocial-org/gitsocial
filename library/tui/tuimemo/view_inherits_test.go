// view_inherits_test.go - Tests for the inherited-sources view's result messages
package tuimemo

import (
	"errors"
	"os"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/extensions/memo"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// newInheritsFixture builds a workspace holding the given inherited sources and an activated view over it.
func newInheritsFixture(t *testing.T, urls ...string) (*inheritsView, *tuicore.State) {
	t.Helper()
	testutil.OpenTempCache(t, "")
	dir, err := testutil.NewRepoTemplate()
	if err != nil {
		t.Fatalf("NewRepoTemplate() error = %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	for _, url := range urls {
		if res := memo.AddInherit(dir, url); !res.Success {
			t.Fatalf("AddInherit(%s) error = %s", url, res.Error.Message)
		}
	}
	view := newInheritsView(dir)
	state := &tuicore.State{Workdir: dir}
	view.Activate(state)
	if len(view.urls) != len(urls) {
		t.Fatalf("Activate() listed %d sources, want %d", len(view.urls), len(urls))
	}
	return view, state
}

// TestInheritsErrMsgSurfacesError checks that a failed add or remove reaches the status bar.
func TestInheritsErrMsgSurfacesError(t *testing.T) {
	view := newInheritsView(t.TempDir())
	state := &tuicore.State{}
	view.Update(inheritsErrMsg{err: errors.New("write inherit ref: permission denied")}, state)
	if state.Message != "write inherit ref: permission denied" {
		t.Errorf("Message = %q, want the error text", state.Message)
	}
	if state.MessageType != tuicore.MessageTypeError {
		t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
	}
}

// TestInheritsFailedAddSurfacesError checks that doAdd's failure command carries an error the view shows.
func TestInheritsFailedAddSurfacesError(t *testing.T) {
	view := newInheritsView(t.TempDir())
	state := &tuicore.State{}
	cmd := view.doAdd("not a url")
	if cmd == nil {
		t.Fatal("doAdd() on a non-repository workdir returned no command")
	}
	msg := cmd()
	if _, ok := msg.(inheritsErrMsg); !ok {
		t.Fatalf("doAdd() produced %T, want inheritsErrMsg", msg)
	}
	view.Update(msg, state)
	if state.Message == "" || state.MessageType != tuicore.MessageTypeError {
		t.Errorf("Message = %q, type = %v, want a non-empty error", state.Message, state.MessageType)
	}
}

// TestInheritsRemovedMsgReloads checks that a successful remove drops the row and keeps the cursor on its successor.
func TestInheritsRemovedMsgReloads(t *testing.T) {
	view, state := newInheritsFixture(t,
		"https://example.com/one.git",
		"https://example.com/two.git",
		"https://example.com/three.git",
	)
	view.cursor = 1
	removed := view.urls[1]
	successor := view.urls[2]
	if res := memo.RemoveInherit(view.workdir, removed); !res.Success {
		t.Fatalf("RemoveInherit(%s) error = %s", removed, res.Error.Message)
	}
	view.Update(inheritsRemovedMsg{url: removed}, state)
	if len(view.urls) != 2 {
		t.Fatalf("urls = %v, want 2 entries after the remove", view.urls)
	}
	if view.cursor != 1 || view.urls[view.cursor] != successor {
		t.Errorf("cursor = %d on %q, want 1 on %q", view.cursor, view.urls[view.cursor], successor)
	}
	if state.MessageType != tuicore.MessageTypeSuccess {
		t.Errorf("MessageType = %v, want MessageTypeSuccess", state.MessageType)
	}
}

// TestInheritsRemovedMsgClampsCursor checks that removing the last row, then the only row, keeps the cursor in range.
func TestInheritsRemovedMsgClampsCursor(t *testing.T) {
	view, state := newInheritsFixture(t,
		"https://example.com/one.git",
		"https://example.com/two.git",
	)
	view.cursor = 1
	last := view.urls[1]
	if res := memo.RemoveInherit(view.workdir, last); !res.Success {
		t.Fatalf("RemoveInherit(%s) error = %s", last, res.Error.Message)
	}
	view.Update(inheritsRemovedMsg{url: last}, state)
	if len(view.urls) != 1 || view.cursor != 0 {
		t.Fatalf("urls = %v, cursor = %d, want 1 entry with cursor 0", view.urls, view.cursor)
	}
	only := view.urls[0]
	if res := memo.RemoveInherit(view.workdir, only); !res.Success {
		t.Fatalf("RemoveInherit(%s) error = %s", only, res.Error.Message)
	}
	view.Update(inheritsRemovedMsg{url: only}, state)
	if len(view.urls) != 0 {
		t.Fatalf("urls = %v, want none left", view.urls)
	}
	if view.cursor != 0 {
		t.Errorf("cursor = %d on an empty list, want 0", view.cursor)
	}
}
