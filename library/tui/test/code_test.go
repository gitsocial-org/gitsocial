// code_test.go - The code view opens from My Repository, lists the tree, opens a file and lands on a line range
package test

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestCodeView drives c on My Repository to the tree, Enter to the file, and a line range to its lines, on an isolated fixture.
func TestCodeView(t *testing.T) {
	f := SetupFixture(t)
	if _, err := git.CommitFiles(f.Workdir, "refs/heads/main", "add code", map[string][]byte{
		"hello.go": []byte("package hello\n\n// Hi greets.\nfunc Hi() string {\n\treturn \"hi\"\n}\n"),
	}); err != nil {
		t.Fatalf("CommitFiles: %v", err)
	}
	h := New(t, f.Workdir, f.CacheDir)

	h.NavigateTo(tuicore.LocMyRepo)
	h.SendKey("c")
	if h.CurrentPath() != "/social/repository/code" {
		t.Fatalf("after c: path = %q, want the code view", h.CurrentPath())
	}
	out := renderedAfterLoad(h, []string{"hello.go"})
	assertContains(t, out, "hello.go")
	assertContains(t, out, "main")
	assertFitsTerminal(t, h)

	h.SendKey("enter")
	out = renderedAfterLoad(h, []string{"package hello"})
	assertContains(t, out, "package hello")
	assertContains(t, out, "4  func Hi() string {")
	assertContains(t, out, "backspace:parent")

	h.NavigateTo(tuicore.LocRepoCode("", "main", "hello.go", 4, 5))
	out = renderedAfterLoad(h, []string{"return"})
	if !strings.Contains(out, "5      return") {
		t.Errorf("line 5 with its tab expanded is missing:\n%s", out)
	}
	assertFitsTerminal(t, h)
}
