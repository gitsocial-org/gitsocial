// show_test.go - Tests for the ambiguity check of the show command
package main

import (
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
)

// TestAmbiguousHashes counts an item and its edit as one item, and two items as ambiguous.
func TestAmbiguousHashes(t *testing.T) {
	cache.Reset()
	if err := cache.Open(t.TempDir()); err != nil {
		t.Fatalf("cache.Open() error = %v", err)
	}
	t.Cleanup(cache.Reset)
	repoURL := "https://example.com/test/ambiguous"
	for _, hash := range []string{"a1b2c3d4e5f6", "a1f6e5d4c3b2"} {
		if err := cache.InsertCommits([]cache.Commit{{Hash: hash, RepoURL: repoURL, Branch: "gitmsg/pm", Message: "issue", Timestamp: time.Now()}}); err != nil {
			t.Fatalf("InsertCommits() error = %v", err)
		}
	}
	if err := cache.InsertVersion(repoURL, "a1f6e5d4c3b2", "gitmsg/pm", repoURL, "a1b2c3d4e5f6", "gitmsg/pm", false); err != nil {
		t.Fatalf("InsertVersion() error = %v", err)
	}
	issue := cache.ExtensionHit{Extension: "pm", RepoURL: repoURL, Hash: "a1b2c3d4e5f6", Branch: "gitmsg/pm"}
	edit := cache.ExtensionHit{Extension: "pm", RepoURL: repoURL, Hash: "a1f6e5d4c3b2", Branch: "gitmsg/pm"}
	post := cache.ExtensionHit{Extension: "social", RepoURL: repoURL, Hash: "a1c3c3c3c3c3", Branch: "gitmsg/social"}
	if _, other := ambiguousHashes([]cache.ExtensionHit{edit, issue}); other != "" {
		t.Errorf("an item and its edit gave %q, want no ambiguity", other)
	}
	if first, other := ambiguousHashes([]cache.ExtensionHit{edit, issue, post}); first != edit.Hash || other != post.Hash {
		t.Errorf("ambiguousHashes() = %q, %q, want %q, %q", first, other, edit.Hash, post.Hash)
	}
}
