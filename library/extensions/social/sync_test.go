// sync_test.go - Tests for social commit processing
package social

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	testutil.OpenTempCache(t, testCacheDir)
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

// TestSchema_noNullFlags pins the schema invariant: after the fetch path, no flag or count column holds NULL, and each is declared NOT NULL.
func TestSchema_noNullFlags(t *testing.T) {
	setupTestDB(t)
	fixture := []struct{ hash, message string }{
		{"a01f12345678", "Hello\n\n" + `GitMsg: ext="social"; type="post"; v="0.1.0"`},
		{"c01f12345678", "Nice post!\n\n" + `GitMsg: ext="social"; type="comment"; original="#commit:a01f12345678@main"; v="0.1.0"`},
		{"e01f12345678", "\n\n" + `GitMsg: ext="social"; type="repost"; original="#commit:a01f12345678@main"; v="0.1.0"`},
		{"d01f12345678", "My thoughts\n\n" + `GitMsg: ext="social"; type="quote"; original="#commit:a01f12345678@main"; v="0.1.0"`},
		{"ed1f12345678", "Hello again\n\n" + `GitMsg: ext="social"; type="post"; edits="#commit:a01f12345678@main"; v="0.1.0"`},
		{"ed2f12345678", "\n\n" + `GitMsg: ext="social"; type="comment"; edits="#commit:c01f12345678@main"; retracted="true"; v="0.1.0"`},
	}
	for _, f := range fixture {
		insertTestCommit(t, f.hash, f.message)
		processSocialCommit(git.Commit{Hash: f.hash, Timestamp: time.Now()}, protocol.ParseMessage(f.message), socialSyncTestRepoURL, socialSyncTestBranch)
	}
	member := []cache.ListRepository{{ListID: "reading", RepoURL: "https://github.com/other/repo", Branch: "main"}}
	if err := cache.InsertList(cache.CachedList{ID: "reading", Name: "Reading", Repositories: member}); err != nil {
		t.Fatalf("InsertList() error = %v", err)
	}
	if err := cache.InsertExternalRepoList(cache.ExternalRepoList{RepoURL: socialSyncTestRepoURL, ListID: "reading", Name: "Reading", Repositories: member}); err != nil {
		t.Fatalf("InsertExternalRepoList() error = %v", err)
	}
	columns := []struct{ table, column string }{
		{"core_list_repositories", "branch"},
		{"social_repo_list_repositories", "branch"},
		{"core_commits", "branch"},
		{"core_commits", "author_name"},
		{"core_commits", "author_email"},
		{"core_commits", "is_virtual"},
		{"core_commits_version", "is_retracted"},
		{"social_interactions", "comments"},
		{"social_interactions", "reposts"},
		{"social_interactions", "quotes"},
	}
	err := cache.ExecLocked(func(db *sql.DB) error {
		for _, table := range []string{"core_commits_version", "social_interactions", "core_list_repositories", "social_repo_list_repositories"} {
			var rows int
			if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&rows); err != nil {
				return err
			}
			if rows == 0 {
				t.Errorf("%s has no rows after the fixture, want rows to check", table)
			}
		}
		for _, c := range columns {
			var nulls, notNull int
			if err := db.QueryRow(`SELECT COUNT(*) FROM ` + c.table + ` WHERE ` + c.column + ` IS NULL`).Scan(&nulls); err != nil {
				return err
			}
			if nulls != 0 {
				t.Errorf("%s.%s holds %d NULLs, want 0", c.table, c.column, nulls)
			}
			if err := db.QueryRow(`SELECT "notnull" FROM pragma_table_info(?) WHERE name = ?`, c.table, c.column).Scan(&notNull); err != nil {
				return err
			}
			if notNull != 1 {
				t.Errorf("%s.%s is not declared NOT NULL", c.table, c.column)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("query the schema: %v", err)
	}
}
