// tags_test.go - The tags view opens from My Repository, counts the commits since the previous tag and opens a release
package test

import (
	"fmt"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// taggedFixture copies the fixture and tags two commits of it, where the release names the newer tag.
func taggedFixture(t *testing.T) *Fixture {
	t.Helper()
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
	return f
}

// TestTagsView drives t and Enter over the tagged fixture.
func TestTagsView(t *testing.T) {
	f := taggedFixture(t)
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

// TestTagsView_locTagSelectsTheTag opens the tags view at the older tag and finds the cursor on it.
func TestTagsView_locTagSelectsTheTag(t *testing.T) {
	f := taggedFixture(t)
	h := New(t, f.Workdir, f.CacheDir)

	h.NavigateTo(tuicore.LocTag("", "v0.9.0"))
	if h.CurrentPath() != "/social/repository/tags" {
		t.Fatalf("after LocTag: path = %q, want the tags view", h.CurrentPath())
	}
	out := renderedAfterLoad(h, []string{"v0.9.0"})
	assertContains(t, out, "▸ v0.9.0")
	assertContains(t, out, "  "+f.ReleaseTag)

	h.SendKey("enter")
	if h.CurrentPath() != "/diff" {
		t.Errorf("after enter on the selected tag: path = %q, want the commit diff", h.CurrentPath())
	}
}
