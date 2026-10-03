// view_changes_test.go - The changes view lists the status rows, moves along them, writes the index and opens the commit form for a staged index
package tuisocial

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// loadedChangesView returns a view over a staged file, a file staged and changed again, and an untracked file.
func loadedChangesView(state *tuicore.State) *changesView {
	view := newChangesView("")
	view.SetSize(100, 30)
	view.Update(changesLoadedMsg{Entries: []git.StatusEntry{
		{Path: "added.txt", Index: 'A', Worktree: ' '},
		{Path: "both.txt", Index: 'M', Worktree: 'M'},
		{Path: "new.txt", Index: '?', Worktree: '?'},
	}}, state)
	return view
}

// pressKey sends one key to the view.
func pressKey(view *changesView, state *tuicore.State, key rune) tea.Cmd {
	return view.Update(tea.KeyPressMsg{Code: key, Text: string(key)}, state)
}

// TestChangesView_rowsAndCursor checks the two status columns, the state words, the cursor moves and the title counts.
func TestChangesView_rowsAndCursor(t *testing.T) {
	state := renderState()
	view := loadedChangesView(state)
	out := ansiCodes.ReplaceAllString(view.Render(state), "")
	for _, want := range []string{"▸ A  added.txt · staged", "MM both.txt · staged, then changed again", "?? new.txt · untracked"} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
	if title := view.Title(); !strings.Contains(title, "3 files · 2 staged") {
		t.Errorf("title = %q", title)
	}
	pressKey(view, state, 'j')
	pressKey(view, state, 'j')
	if e, _ := view.selected(); e.Path != "new.txt" {
		t.Errorf("after j j the cursor is on %q, want new.txt", e.Path)
	}
	if cmd := pressKey(view, state, 'j'); cmd != nil || view.cursor != 2 {
		t.Errorf("j past the last row moved the cursor to %d", view.cursor)
	}
	pressKey(view, state, 'k')
	if e, _ := view.selected(); e.Path != "both.txt" {
		t.Errorf("after k the cursor is on %q, want both.txt", e.Path)
	}
}

// TestChangesView_selectedFileDiff checks that the staged diff heads the unstaged one for the selected file, and that a stale diff is dropped.
func TestChangesView_selectedFileDiff(t *testing.T) {
	state := renderState()
	view := loadedChangesView(state)
	view.cursor = 1
	hunk := git.Hunk{OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 2, Lines: []git.DiffLine{{Type: git.LineContext, Content: "one", OldNum: 1, NewNum: 1}, {Type: git.LineAdded, Content: "two", NewNum: 2}}}
	view.Update(fileDiffLoadedMsg{Path: "both.txt", Diffs: []git.FileDiff{
		{OldPath: "both.txt", NewPath: "staged: both.txt", Status: git.DiffStatusModified, Hunks: []git.Hunk{hunk}},
		{OldPath: "both.txt", NewPath: "unstaged: both.txt", Status: git.DiffStatusModified, Hunks: []git.Hunk{hunk}},
	}}, state)
	out := ansiCodes.ReplaceAllString(view.Render(state), "")
	staged, unstaged := strings.Index(out, "staged: both.txt"), strings.Index(out, "unstaged: both.txt")
	if staged < 0 || unstaged < 0 || staged > unstaged {
		t.Errorf("the staged heading should come before the unstaged one:\n%s", out)
	}
	view.Update(fileDiffLoadedMsg{Path: "added.txt", Diffs: []git.FileDiff{{NewPath: "stale: added.txt", Status: git.DiffStatusAdded}}}, state)
	if out := ansiCodes.ReplaceAllString(view.Render(state), ""); strings.Contains(out, "stale: added.txt") {
		t.Errorf("a diff of a file that is not selected replaced the view:\n%s", out)
	}
}

// TestChangesView_commitNeedsAStagedFile checks that C opens the form with the staged count, and warns when the index is empty.
func TestChangesView_commitNeedsAStagedFile(t *testing.T) {
	state := renderState()
	view := loadedChangesView(state)
	cmd := pressKey(view, state, 'C')
	if cmd == nil {
		t.Fatal("C returned no command")
	}
	nav, ok := cmd().(tuicore.NavigateMsg)
	if !ok || nav.Location.Path != "/social/commit-form" || nav.Location.Param("files") != "2" {
		t.Errorf("C = %+v, want the commit form for the 2 staged files", nav)
	}
	unstaged := newChangesView("")
	unstaged.SetSize(100, 30)
	unstaged.Update(changesLoadedMsg{Entries: []git.StatusEntry{{Path: "new.txt", Index: '?', Worktree: '?'}}}, state)
	if cmd := pressKey(unstaged, state, 'C'); cmd != nil || !strings.Contains(state.Message, "Nothing staged") {
		t.Errorf("an empty index opened the form or gave no warning: %v, %q", cmd, state.Message)
	}
	clean := newChangesView("")
	clean.Update(changesLoadedMsg{}, state)
	if out := ansiCodes.ReplaceAllString(clean.Render(state), ""); !strings.Contains(out, "No changes") {
		t.Errorf("clean render = %q", out)
	}
}

// TestChangesView_indexKeys checks that s, u, S and U return a command where the index has work, none where it has not, and that a reload keeps the cursor on its path.
func TestChangesView_indexKeys(t *testing.T) {
	state := renderState()
	view := loadedChangesView(state)
	view.cursor = 1
	for _, key := range []rune{'s', 'u', 'S', 'U'} {
		if cmd := pressKey(view, state, key); cmd == nil {
			t.Errorf("%c on a file staged and changed again returned no command", key)
		}
	}
	view.cursor = 2
	if cmd := pressKey(view, state, 'u'); cmd != nil {
		t.Error("u on an untracked file should do nothing")
	}
	view.cursor = 0
	if cmd := pressKey(view, state, 's'); cmd != nil {
		t.Error("s on a file that is staged in full should do nothing")
	}
	view.cursor = 2
	view.Update(indexChangedMsg{}, state)
	view.Update(changesLoadedMsg{Entries: []git.StatusEntry{
		{Path: "both.txt", Index: 'M', Worktree: 'M'},
		{Path: "new.txt", Index: 'A', Worktree: ' '},
	}}, state)
	if e, _ := view.selected(); e.Path != "new.txt" {
		t.Errorf("after the reload the cursor is on %q, want new.txt", e.Path)
	}
	view.Update(indexChangedMsg{Err: errors.New("stage: index locked")}, state)
	if state.Message != "stage: index locked" {
		t.Errorf("message = %q", state.Message)
	}
}

// TestChangesView_loadFailureSurfaces checks that a failed status read reaches the status bar.
func TestChangesView_loadFailureSurfaces(t *testing.T) {
	state := renderState()
	view := newChangesView("")
	view.Update(changesLoadedMsg{Err: errors.New("working status: not a repository")}, state)
	if state.Message != "working status: not a repository" || state.MessageType != tuicore.MessageTypeError {
		t.Errorf("message = %q (%v)", state.Message, state.MessageType)
	}
}

// TestCommitForm_messageAndRetry checks the message shape, the button label and that a failed commit keeps the text for another try.
func TestCommitForm_messageAndRetry(t *testing.T) {
	form := newCommitForm("", 2)
	form.data.Subject = " Fix the thing "
	form.data.Body = "Because.\n"
	if got := form.Message(); got != "Fix the thing\n\nBecause." {
		t.Errorf("Message = %q", got)
	}
	if commitButtonLabel(1) != "Commit 1 file" || commitButtonLabel(3) != "Commit 3 files" {
		t.Errorf("button labels = %q, %q", commitButtonLabel(1), commitButtonLabel(3))
	}
	view := newCommitFormView("")
	view.AttachForm(form)
	view.Update(commitCreatedMsg{Err: errors.New("git commit: hook refused")}, renderState())
	kept, ok := view.CurrentForm().(*commitForm)
	if !ok || kept.data.Subject != " Fix the thing " || kept.data.Body != "Because.\n" {
		t.Errorf("the form lost its text after a failed commit: %+v", kept)
	}
}
