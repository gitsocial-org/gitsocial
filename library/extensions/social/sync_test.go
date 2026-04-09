// sync_test.go - Tests for social commit processing
package social

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	cache.Reset()
	dir := t.TempDir()
	if err := cache.Open(dir); err != nil {
		t.Fatalf("cache.Open() error = %v", err)
	}
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})
}

const socialSyncTestRepoURL = "https://github.com/test/repo"
const socialSyncTestBranch = "main"

func insertTestCommit(t *testing.T, hash, message string) {
	t.Helper()
	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        hash,
		RepoURL:     socialSyncTestRepoURL,
		Branch:      socialSyncTestBranch,
		AuthorName:  "Test User",
		AuthorEmail: "test@test.com",
		Message:     message,
		Timestamp:   time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
}

func TestExtractPostType(t *testing.T) {
	tests := []struct {
		name string
		msg  *protocol.Message
		want PostType
	}{
		{"post", &protocol.Message{Header: protocol.Header{Fields: map[string]string{"type": "post"}}}, PostTypePost},
		{"comment", &protocol.Message{Header: protocol.Header{Fields: map[string]string{"type": "comment"}}}, PostTypeComment},
		{"repost", &protocol.Message{Header: protocol.Header{Fields: map[string]string{"type": "repost"}}}, PostTypeRepost},
		{"quote", &protocol.Message{Header: protocol.Header{Fields: map[string]string{"type": "quote"}}}, PostTypeQuote},
		{"default", &protocol.Message{Header: protocol.Header{Fields: map[string]string{}}}, PostTypePost},
		{"unknown", &protocol.Message{Header: protocol.Header{Fields: map[string]string{"type": "unknown"}}}, PostTypePost},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPostType(tt.msg)
			if got != tt.want {
				t.Errorf("extractPostType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProcessSocialCommit_nilMessage(t *testing.T) {
	setupTestDB(t)
	gc := git.Commit{Hash: "abc123456789"}
	processSocialCommit(gc, nil, "https://github.com/test/repo", "main")
	count := countSocialItems(t)
	if count != 0 {
		t.Errorf("expected 0 social_items for nil msg, got %d", count)
	}
}

func TestProcessSocialCommit_wrongExtension(t *testing.T) {
	setupTestDB(t)
	msg := &protocol.Message{
		Content: "A PM issue",
		Header:  protocol.Header{Ext: "pm", V: "0.1.0", Fields: map[string]string{"type": "issue"}},
	}
	gc := git.Commit{Hash: "abc123456789"}
	processSocialCommit(gc, msg, "https://github.com/test/repo", "main")
	count := countSocialItems(t)
	if count != 0 {
		t.Errorf("expected 0 social_items for wrong ext, got %d", count)
	}
}

func TestProcessSocialCommit_basicPost(t *testing.T) {
	setupTestDB(t)
	repoURL := socialSyncTestRepoURL
	hash := "post12345678"
	branch := socialSyncTestBranch
	content := "Hello world!\n\n" + `GitMsg: ext="social"; type="post"; v="0.1.0"`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processSocialCommit(gc, msg, repoURL, branch)

	item := querySocialItem(t, hash)
	if item.Type != "post" {
		t.Errorf("Type = %q, want post", item.Type)
	}
}

func TestProcessSocialCommit_comment(t *testing.T) {
	setupTestDB(t)
	repoURL := socialSyncTestRepoURL
	hash := "cmnt12345678"
	branch := socialSyncTestBranch
	content := "Nice post!\n\n" + `GitMsg: ext="social"; type="comment"; original="#commit:a01f12345678@main"; v="0.1.0"`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processSocialCommit(gc, msg, repoURL, branch)

	item := querySocialItem(t, hash)
	if item.Type != "comment" {
		t.Errorf("Type = %q, want comment", item.Type)
	}
	if !item.OriginalHash.Valid {
		t.Error("OriginalHash should be valid for comment")
	}
}

func TestProcessSocialCommit_repost(t *testing.T) {
	setupTestDB(t)
	repoURL := socialSyncTestRepoURL
	hash := "rpst12345678"
	branch := socialSyncTestBranch
	content := "\n\n" + `GitMsg: ext="social"; type="repost"; original="#commit:a01f12345678@main"; v="0.1.0"`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processSocialCommit(gc, msg, repoURL, branch)

	item := querySocialItem(t, hash)
	if item.Type != "repost" {
		t.Errorf("Type = %q, want repost", item.Type)
	}
}

func TestProcessSocialCommit_quote(t *testing.T) {
	setupTestDB(t)
	repoURL := socialSyncTestRepoURL
	hash := "quot12345678"
	branch := socialSyncTestBranch
	content := "My thoughts on this\n\n" + `GitMsg: ext="social"; type="quote"; original="#commit:a01f12345678@main"; v="0.1.0"`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processSocialCommit(gc, msg, repoURL, branch)

	item := querySocialItem(t, hash)
	if item.Type != "quote" {
		t.Errorf("Type = %q, want quote", item.Type)
	}
}

func TestProcessSocialCommit_withReplyTo(t *testing.T) {
	setupTestDB(t)
	repoURL := socialSyncTestRepoURL
	hash := "rply12345678"
	branch := socialSyncTestBranch
	content := "Replying to comment\n\n" + `GitMsg: ext="social"; type="comment"; original="#commit:a01f12345678@main"; reply-to="#commit:b02f12345678@main"; v="0.1.0"`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processSocialCommit(gc, msg, repoURL, branch)

	item := querySocialItem(t, hash)
	if !item.ReplyToHash.Valid {
		t.Error("ReplyToHash should be valid")
	}
}

// Helper functions

func countSocialItems(t *testing.T) int {
	t.Helper()
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM social_items`).Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("countSocialItems query error = %v", err)
	}
	return count
}

func querySocialItem(t *testing.T, hash string) SocialItem {
	t.Helper()
	item, err := cache.QueryLocked(func(db *sql.DB) (SocialItem, error) {
		var s SocialItem
		err := db.QueryRow(`SELECT repo_url, hash, branch, type,
			original_repo_url, original_hash, original_branch,
			reply_to_repo_url, reply_to_hash, reply_to_branch
			FROM social_items WHERE repo_url = ? AND hash = ? AND branch = ?`,
			socialSyncTestRepoURL, hash, socialSyncTestBranch).Scan(
			&s.RepoURL, &s.Hash, &s.Branch, &s.Type,
			&s.OriginalRepoURL, &s.OriginalHash, &s.OriginalBranch,
			&s.ReplyToRepoURL, &s.ReplyToHash, &s.ReplyToBranch,
		)
		return s, err
	})
	if err != nil {
		t.Fatalf("querySocialItem error = %v", err)
	}
	return item
}
