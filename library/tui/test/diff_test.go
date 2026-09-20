// diff_test.go - Diff view key handling
package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestDiffTabMovesToNextFile checks that tab in a diff view moves the file cursor and leaves focus alone.
func TestDiffTabMovesToNextFile(t *testing.T) {
	f := SetupFixture(t)
	commit := commitTwoFiles(t, f.Workdir)
	h := New(t, f.Workdir, f.CacheDir)

	h.NavigateTo(tuicore.LocCommitDiff(commit))
	if got := cursorLine(rendered(h)); !strings.Contains(got, "alpha.txt") {
		t.Fatalf("cursor starts on %q, want the alpha.txt header\n%s", got, rendered(h))
	}
	focused := h.model.Host().State().Focused

	h.SendKey("tab")

	if got := cursorLine(rendered(h)); !strings.Contains(got, "beta.txt") {
		t.Errorf("after tab the cursor is on %q, want the beta.txt header\n%s", got, rendered(h))
	}
	if h.model.Host().State().Focused != focused {
		t.Error("tab moved the panel focus; a diff view keeps tab for the next file")
	}
}

// commitTwoFiles adds a commit touching two files and returns its hash.
func commitTwoFiles(t *testing.T, workdir string) string {
	t.Helper()
	for _, name := range []string{"alpha.txt", "beta.txt"} {
		if err := os.WriteFile(filepath.Join(workdir, name), []byte(name+" body\n"), 0600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if _, err := git.ExecGit(workdir, []string{"add", "alpha.txt", "beta.txt"}); err != nil {
		t.Fatalf("git add: %v", err)
	}
	hash, err := git.CreateCommit(workdir, git.CommitOptions{Message: "Add two files"})
	if err != nil {
		t.Fatalf("CreateCommit: %v", err)
	}
	return hash
}

// cursorLine returns the rendered line carrying the diff cursor indicator.
func cursorLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "▌") {
			return line
		}
	}
	return ""
}
