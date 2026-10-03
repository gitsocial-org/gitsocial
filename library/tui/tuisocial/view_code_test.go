// view_code_test.go - The code view lists a tree, opens an entry, shows a file with its line range and names a binary file
package tuisocial

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// loadedCodeTree returns a view over a subtree with a directory, a file and a submodule.
func loadedCodeTree() *codeView {
	view := newCodeView()
	view.url, view.name, view.branch, view.path = "https://github.com/user/repo", "user/repo", "main", "src"
	view.Update(codeLoadedMsg{Type: "tree", Branch: "main", Entries: []git.TreeEntry{
		{Name: "lib", Type: "tree"},
		{Name: "main.go", Type: "blob", Size: 2048},
		{Name: "vendor", Type: "commit"},
	}}, &tuicore.State{})
	return view
}

// renderState is a state wide enough for a render.
func renderState() *tuicore.State {
	return &tuicore.State{Width: 120, Height: 40, Registry: tuicore.NewRegistry()}
}

// TestCodeLoadFailureSurfaces checks that a failed read reaches the status bar and the view.
func TestCodeLoadFailureSurfaces(t *testing.T) {
	view := newCodeView()
	view.loading = true
	state := renderState()
	view.Update(codeLoadedMsg{Err: errors.New("object type of x: not found")}, state)
	if state.Message != "object type of x: not found" || state.MessageType != tuicore.MessageTypeError {
		t.Errorf("message = %q (%v), want the error", state.Message, state.MessageType)
	}
	if out := ansiCodes.ReplaceAllString(view.Render(state), ""); !strings.Contains(out, "not found") {
		t.Errorf("render lacks the error:\n%s", out)
	}
}

// TestCodeTreeRender checks the parent row, the directory slash, the file size and the submodule mark.
func TestCodeTreeRender(t *testing.T) {
	out := ansiCodes.ReplaceAllString(loadedCodeTree().Render(renderState()), "")
	for _, want := range []string{"▸ ..", "lib/", "main.go · 2.0KB", "vendor · submodule"} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
}

// TestCodeEnterOpensEntry checks that Enter pushes the entry's path, the parent row replaces with the parent, and a submodule only warns.
func TestCodeEnterOpensEntry(t *testing.T) {
	view := loadedCodeTree()
	state := renderState()
	nav := func(cursor int) tuicore.NavigateMsg {
		t.Helper()
		view.cursor = cursor
		cmd := view.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, state)
		if cmd == nil {
			t.Fatalf("enter on row %d returned no command", cursor)
		}
		msg, ok := cmd().(tuicore.NavigateMsg)
		if !ok {
			t.Fatalf("enter on row %d = %T, want a NavigateMsg", cursor, cmd())
		}
		return msg
	}
	if m := nav(1); m.Action != tuicore.NavPush || m.Location.Param("path") != "src/lib" || m.Location.Param("branch") != "main" || m.Location.Param("url") != view.url {
		t.Errorf("row 1 = %+v, want a push of src/lib on main", m.Location)
	}
	if m := nav(2); m.Location.Param("path") != "src/main.go" {
		t.Errorf("row 2 = %+v, want src/main.go", m.Location)
	}
	if m := nav(0); m.Action != tuicore.NavReplace || m.Location.Param("path") != "" {
		t.Errorf("parent row = %+v, want a replace with the root", m.Location)
	}
	view.cursor = 3
	if cmd := view.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, state); cmd != nil || !strings.Contains(state.Message, "submodule") {
		t.Errorf("a submodule opened or gave no message: %v, %q", cmd, state.Message)
	}
}

// TestCodeFileRender checks the line numbers, the selected range and the scroll that brings the range into view.
func TestCodeFileRender(t *testing.T) {
	view := newCodeView()
	view.name, view.branch, view.path, view.lineStart, view.lineEnd = "user/repo", "main", "main.go", 30, 31
	var body []string
	for i := 1; i <= 40; i++ {
		body = append(body, "line "+strings.Repeat("x", i))
	}
	state := renderState()
	view.Update(codeLoadedMsg{Type: "blob", Branch: "main", Content: strings.Join(body, "\n") + "\n"}, state)
	if len(view.lines) != 40 {
		t.Fatalf("lines = %d, want 40 without the trailing newline", len(view.lines))
	}
	if view.scroll == 0 || view.scroll > 29 {
		t.Errorf("scroll = %d, want the range brought into view", view.scroll)
	}
	raw := view.Render(state)
	out := ansiCodes.ReplaceAllString(raw, "")
	if !strings.Contains(out, "30  line") || !strings.Contains(out, "31  line") {
		t.Errorf("render lacks the numbered range lines:\n%s", out)
	}
	if strings.Contains(out, " 1  line x\n") {
		t.Errorf("render starts at line 1, the range is off screen:\n%s", out)
	}
	if title := view.Title(); !strings.Contains(title, "user/repo · main · main.go") {
		t.Errorf("title = %q", title)
	}
}

// TestCodeBinaryAndLFS checks that a binary file and an LFS pointer show their size and no content.
func TestCodeBinaryAndLFS(t *testing.T) {
	state := renderState()
	view := newCodeView()
	view.path = "img.png"
	view.Update(codeLoadedMsg{Type: "blob", Content: "\x89PNG\x00\x00data"}, state)
	if out := ansiCodes.ReplaceAllString(view.Render(state), ""); !strings.Contains(out, "Binary file · 10B") {
		t.Errorf("binary render = %q", out)
	}
	lfs := newCodeView()
	lfs.path = "model.bin"
	lfs.Update(codeLoadedMsg{Type: "blob", Content: string(git.FormatLFSPointer("abc", 3*1024*1024))}, state)
	if out := ansiCodes.ReplaceAllString(lfs.Render(state), ""); !strings.Contains(out, "Git LFS file · 3.0MB") {
		t.Errorf("lfs render = %q", out)
	}
}

// TestFitCodeLine expands tabs and cuts a long line with an ellipsis.
func TestFitCodeLine(t *testing.T) {
	if got := fitCodeLine("\tx", 10); got != "    x" {
		t.Errorf("tab = %q", got)
	}
	if got := fitCodeLine("abcdefghij", 5); got != "abcd…" {
		t.Errorf("cut = %q", got)
	}
}
