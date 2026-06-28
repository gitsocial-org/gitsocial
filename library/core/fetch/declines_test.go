// declines_test.go - Syncing a fork's published decline markers into the local
// decline table (the reciprocal decline signal).
package fetch

import (
	"fmt"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
)

func TestSyncForkDeclines(t *testing.T) {
	setupTestCache(t)
	dir := t.TempDir()
	if _, err := git.ExecGit(dir, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := git.ExecGit(dir, []string{"config", "user.email", "t@test.com"}); err != nil {
		t.Fatalf("config: %v", err)
	}
	if _, err := git.ExecGit(dir, []string{"config", "user.name", "T"}); err != nil {
		t.Fatalf("config: %v", err)
	}

	// Simulate a fetched fork decline marker: a commit whose subject is the
	// declined proposal's ref, under refs/forks/<hash>/declines/.
	hash := URLHash("https://github.com/alice/repo")
	proposalRef := "https://github.com/bob/repo#commit:abc123def456@gitmsg/pm"
	commitHash, err := git.CreateCommitTree(dir, proposalRef+"\n", "")
	if err != nil {
		t.Fatalf("commit-tree: %v", err)
	}
	if err := git.WriteRef(dir, fmt.Sprintf("refs/forks/%s/declines/x", hash), commitHash); err != nil {
		t.Fatalf("write ref: %v", err)
	}

	syncForkDeclines(dir, hash)

	declined, err := cache.HasDecline("https://github.com/bob/repo", "abc123def456", "gitmsg/pm")
	if err != nil {
		t.Fatalf("HasDecline: %v", err)
	}
	if !declined {
		t.Error("expected the fork's published decline to be recorded locally")
	}
}
