// tags_test.go - The tags view opens from My Repository, counts the commits since the previous tag and opens a release
package test

import (
	"fmt"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestTagsView tags two commits of an isolated fixture, where a release names the newer tag, and drives t and Enter.
func TestTagsView(t *testing.T) {
	f := SetupFixture(t)
	if _, err := git.ExecGit(f.Workdir, []string{"tag", "v0.9.0", "main"}); err != nil {
		t.Fatalf("tag v0.9.0: %v", err)
	}
	if _, err := git.CommitFiles(f.Workdir, "refs/heads/main", "release work", map[string][]byte{"CHANGELOG.md": []byte("# 1.0.0\n")}); err != nil {
		t.Fatalf("CommitFiles: %v", err)
	}
	if _, err := git.ExecGit(f.Workdir, []string{"tag", f.ReleaseTag, "main"}); err != nil {
		t.Fatalf("tag %s: %v", f.ReleaseTag, err)
	}
	history, err := git.CountCommits(f.Workdir, "v0.9.0", "")
	if err != nil {
		t.Fatalf("CountCommits: %v", err)
	}
	h := New(t, f.Workdir, f.CacheDir)

	h.NavigateTo(tuicore.LocMyRepo)
	h.SendKey("t")
	if h.CurrentPath() != "/social/repository/tags" {
		t.Fatalf("after t: path = %q, want the tags view", h.CurrentPath())
	}
	out := renderedAfterLoad(h, []string{"v0.9.0"})
	assertContains(t, out, "▸ v1.0.0 · 1 commit since v0.9.0")
	assertContains(t, out, "release")
	assertContains(t, out, fmt.Sprintf("v0.9.0 · %d commits", history))
	assertFitsTerminal(t, h)

	h.SendKey("enter")
	if h.CurrentPath() != "/release/detail" {
		t.Errorf("after enter on a released tag: path = %q, want the release detail", h.CurrentPath())
	}
	assertContains(t, renderedAfterLoad(h, []string{f.ReleaseSubject}), f.ReleaseSubject)
}
