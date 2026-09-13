// repository_test.go - tests for the repository-removal sequence.
package client

import (
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

// fetchSocialCommit ingests one commit the way the fetch path does.
func fetchSocialCommit(t *testing.T, repoURL, branch, hash, message string) {
	t.Helper()
	now := time.Now()
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: repoURL, Branch: branch,
		AuthorName: "Remote", AuthorEmail: "remote@test.com", Message: message, Timestamp: now,
	}}); err != nil {
		t.Fatalf("InsertCommits(%s): %v", hash, err)
	}
	commit := git.Commit{Hash: hash, Message: message, Author: "Remote", Email: "remote@test.com", Timestamp: now}
	msg := protocol.ParseMessage(message)
	for _, process := range processors() {
		process(commit, msg, repoURL, branch)
	}
}

// TestRemoveRepository_recountsTheItemsItLeaves pins the recount after a removal:
// the comments a deleted repository carried stop counting on the posts that stay.
func TestRemoveRepository_recountsTheItemsItLeaves(t *testing.T) {
	testutil.OpenTempCache(t, "")
	repoA := "https://example.com/counts/a"
	repoB := "https://example.com/counts/b"
	branch := "gitmsg/social"

	fetchSocialCommit(t, repoA, branch, "c66600000001", "Root post")
	fetchSocialCommit(t, repoB, branch, "c66600000002",
		"Nice\n\n"+`GitMsg: ext="social"; type="comment"; original="`+repoA+`#commit:c66600000001@gitmsg/social"; v="0.1.0"`)
	counts, err := social.RefreshInteractionCounts(repoA, "c66600000001", branch)
	if err != nil {
		t.Fatalf("RefreshInteractionCounts(): %v", err)
	}
	if counts.Comments != 1 {
		t.Fatalf("root comments with the comment fetched = %d, want 1", counts.Comments)
	}

	if err := RemoveRepository(repoB, ""); err != nil {
		t.Fatalf("RemoveRepository(): %v", err)
	}
	counts, err = social.RefreshInteractionCounts(repoA, "c66600000001", branch)
	if err != nil {
		t.Fatalf("RefreshInteractionCounts(): %v", err)
	}
	if counts.Comments != 0 {
		t.Errorf("root comments after the commenting repository is removed = %d, want 0", counts.Comments)
	}
}
