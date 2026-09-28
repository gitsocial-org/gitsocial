// push_tags_test.go - Tests for the tags step of a push.
package gitmsg

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// TestPushTags_noTagsSkipsEmptyRemote: with no local tags, the tags step sends
// nothing and a dry run against a completely empty remote does not fail with
// git's "No refs in common and none specified".
func TestPushTags_noTagsSkipsEmptyRemote(t *testing.T) {
	remote := t.TempDir()
	if err := git.EnsureBareRepo(remote); err != nil {
		t.Fatalf("init remote: %v", err)
	}
	work := t.TempDir()
	if err := git.Init(work, "main"); err != nil {
		t.Fatalf("init work: %v", err)
	}
	for _, kv := range [][2]string{{"user.name", "Tester"}, {"user.email", "t@example.com"}} {
		git.ExecGit(work, []string{"config", kv[0], kv[1]})
	}
	git.CreateCommit(work, git.CommitOptions{Message: "init", AllowEmpty: true})
	if _, err := git.ExecGit(work, []string{"remote", "add", "origin", remote}); err != nil {
		t.Fatalf("add origin: %v", err)
	}

	count, err := pushTags(work, "origin", true, false)
	if err != nil {
		t.Fatalf("pushTags dry run on an empty remote with no tags: %v", err)
	}
	if count != 0 {
		t.Errorf("pushTags count = %d, want 0", count)
	}

	// With a tag, the same dry run counts it and still succeeds.
	if _, err := git.ExecGit(work, []string{"tag", "v0.0.1-test"}); err != nil {
		t.Fatalf("tag: %v", err)
	}
	count, err = pushTags(work, "origin", true, false)
	if err != nil {
		t.Fatalf("pushTags dry run with one tag: %v", err)
	}
	if count != 1 {
		t.Errorf("pushTags count = %d, want 1", count)
	}
}
