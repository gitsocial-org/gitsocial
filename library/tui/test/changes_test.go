// changes_test.go - The changes view lists the working tree, stages and unstages a file, and commits the index only
package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// statusColumns returns the two status columns of a path, or "" when the path has no row.
func statusColumns(t *testing.T, workdir, path string) string {
	t.Helper()
	entries, err := git.WorkingStatus(workdir)
	if err != nil {
		t.Fatalf("WorkingStatus: %v", err)
	}
	for _, e := range entries {
		if e.Path == path {
			return string([]byte{e.Index, e.Worktree})
		}
	}
	return ""
}

// TestChangesCommit stages one of two untracked files from the view and commits the index: the commit tracks that file only, the other stays untracked.
func TestChangesCommit(t *testing.T) {
	f := SetupFixture(t)
	for _, name := range []string{"alpha.txt", "beta.txt"} {
		if err := os.WriteFile(filepath.Join(f.Workdir, name), []byte(name+"\n"), 0600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	h := New(t, f.Workdir, f.CacheDir)

	h.NavigateTo(tuicore.LocMyRepo)
	h.SendKey("w")
	if h.CurrentPath() != "/social/changes" {
		t.Fatalf("after w: path = %q, want the changes view", h.CurrentPath())
	}
	out := renderedAfterLoad(h, []string{"beta.txt"})
	assertContains(t, out, "▸ ?? alpha.txt · untracked")
	assertContains(t, out, "?? beta.txt · untracked")
	assertContains(t, out, "+ alpha.txt")
	assertFitsTerminal(t, h)

	h.SendKey("C")
	if h.CurrentPath() != "/social/changes" || !strings.Contains(rendered(h), "Nothing staged") {
		t.Fatalf("C with an empty index: path = %q, want to stay with the warning\n%s", h.CurrentPath(), rendered(h))
	}

	h.SendKey("s")
	out = renderedAfterLoad(h, []string{"A  alpha.txt"})
	assertContains(t, out, "▸ A  alpha.txt · staged")
	assertContains(t, out, "staged: alpha.txt")
	if got := statusColumns(t, f.Workdir, "alpha.txt"); got != "A " {
		t.Fatalf("alpha.txt after s = %q, want in the index", got)
	}

	h.SendKey("C")
	if h.CurrentPath() != "/social/commit-form" {
		t.Fatalf("after C: path = %q, want the commit form", h.CurrentPath())
	}
	h.SendKeys(strings.Split("Add alpha", "")...)
	submitForm(h)

	if h.CurrentPath() != "/social/changes" {
		t.Fatalf("after the commit: path = %q, want the changes view\n%s", h.CurrentPath(), rendered(h))
	}
	subject, err := git.GetCommitMessage(f.Workdir, "HEAD")
	if err != nil || !strings.HasPrefix(strings.TrimSpace(subject), "Add alpha") {
		t.Errorf("HEAD message = %q, %v, want the subject", subject, err)
	}
	files, _ := git.ExecGit(f.Workdir, []string{"show", "--name-only", "--format=", "HEAD"})
	if files == nil || strings.TrimSpace(files.Stdout) != "alpha.txt" {
		t.Errorf("HEAD files = %+v, want alpha.txt only", files)
	}
	if got := statusColumns(t, f.Workdir, "beta.txt"); got != "??" {
		t.Errorf("beta.txt after the commit = %q, want still untracked", got)
	}
	out = renderedAfterLoad(h, []string{"beta.txt"})
	assertContains(t, out, "▸ ?? beta.txt · untracked")
	if strings.Contains(out, "alpha.txt") {
		t.Errorf("the committed file is still listed:\n%s", out)
	}
}

// TestChangesStageUnstageRoundTrip: s then u from the view leaves an untracked file untracked and a modified file unstaged.
func TestChangesStageUnstageRoundTrip(t *testing.T) {
	f := SetupFixture(t)
	if err := os.WriteFile(filepath.Join(f.Workdir, "loose.txt"), []byte("loose\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	h := New(t, f.Workdir, f.CacheDir)
	h.NavigateTo(tuicore.LocChanges)
	renderedAfterLoad(h, []string{"loose.txt"})
	h.SendKey("s")
	if got := statusColumns(t, f.Workdir, "loose.txt"); got != "A " {
		t.Fatalf("after s = %q, want A", got)
	}
	h.SendKey("u")
	if got := statusColumns(t, f.Workdir, "loose.txt"); got != "??" {
		t.Errorf("after u = %q, want untracked again", got)
	}
	h.SendKey("S")
	if got := statusColumns(t, f.Workdir, "loose.txt"); got != "A " {
		t.Fatalf("after S = %q, want A", got)
	}
	h.SendKey("U")
	if got := statusColumns(t, f.Workdir, "loose.txt"); got != "??" {
		t.Errorf("after U = %q, want untracked again", got)
	}
	assertContains(t, renderedAfterLoad(h, []string{"?? loose.txt"}), "?? loose.txt · untracked")
}
