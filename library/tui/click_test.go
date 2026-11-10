// click_test.go - Tests for the handler of the click socket: the lines it takes and the order of its steps.
package tui

import (
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// clickRecorder returns a handler whose activation and sends append to one ordered list, and that list.
func clickRecorder() (func(string), func() []string) {
	var mu sync.Mutex
	var steps []string
	record := func(step string) {
		mu.Lock()
		defer mu.Unlock()
		steps = append(steps, step)
	}
	handle := clickHandler(func() { record("activate") }, func(msg tea.Msg) {
		if click, ok := msg.(tuicore.ClickMsg); ok {
			record("send " + click.Ref)
		}
	})
	return handle, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), steps...)
	}
}

func TestClickHandler_rejectsOtherLines(t *testing.T) {
	handle, steps := clickRecorder()
	for _, line := range []string{"", "open-x", "close #commit:0123456789ab", "open #branch:main", "open #commit:xyz", "open https://example.com/owner/repo", "OPEN #commit:0123456789ab"} {
		handle(line)
	}
	time.Sleep(100 * time.Millisecond)
	if got := steps(); len(got) != 0 {
		t.Errorf("steps for lines that are not open with an empty or commit ref = %q, want none", got)
	}
}

func TestClickHandler_activatesFirst(t *testing.T) {
	ref := "https://example.com/owner/repo#commit:0123456789ab@main"
	for _, c := range []struct{ line, ref string }{{"open " + ref, ref}, {"open ", ""}, {"open", ""}} {
		handle, steps := clickRecorder()
		handle(c.line)
		deadline := time.Now().Add(5 * time.Second)
		for len(steps()) < 2 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if got := steps(); len(got) != 2 || got[0] != "activate" || got[1] != "send "+c.ref {
			t.Errorf("steps for %q = %q, want activate, then send of the ref", c.line, got)
		}
	}
}
