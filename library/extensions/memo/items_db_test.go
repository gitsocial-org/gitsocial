// items_db_test.go - Tests for memo item database lookups and the comment provider
package memo

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

// insertMemoTestCommit caches one commit with its memo row under the given branch.
func insertMemoTestCommit(t *testing.T, repoURL, hash, branch, email string) {
	t.Helper()
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: repoURL, Branch: branch, AuthorName: "Author", AuthorEmail: email,
		Message: "memo", Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	if err := InsertMemoItem(MemoItem{RepoURL: repoURL, Hash: hash, Branch: branch}); err != nil {
		t.Fatalf("InsertMemoItem() error = %v", err)
	}
}

// markStale sets stale_since on one cached row.
func markStale(t *testing.T, repoURL, hash, branch string) {
	t.Helper()
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE core_commits SET stale_since = '2025-10-22T00:00:00Z' WHERE repo_url = ? AND hash = ? AND branch = ?`, repoURL, hash, branch)
		return err
	}); err != nil {
		t.Fatalf("mark stale: %v", err)
	}
}

// TestGetMemoItemByHashPrefix_liveFirst pins invariant 2: a prefix lookup opens the live row of a moved memo, and still finds a stale-only one.
func TestGetMemoItemByHashPrefix_liveFirst(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/prefix-memo"
	const moved, gone = "11fe00000001", "11fe00000002"
	for _, row := range []struct{ hash, branch string }{{moved, "feature/x"}, {moved, MemoBranch}, {gone, "feature/x"}} {
		insertMemoTestCommit(t, repoURL, row.hash, row.branch, "me@x.com")
	}
	markStale(t, repoURL, moved, "feature/x")
	markStale(t, repoURL, gone, "feature/x")

	if _, err := GetMemoItemByHashPrefix("11fe000000"); err == nil {
		t.Fatal("GetMemoItemByHashPrefix() accepted a prefix two memos share")
	}
	item, err := GetMemoItemByHashPrefix(moved)
	if err != nil || item.Branch != MemoBranch {
		t.Errorf("GetMemoItemByHashPrefix(moved) = %+v, %v, want the live row under %s", item, err, MemoBranch)
	}
	item, err = GetMemoItemByHashPrefix(gone)
	if err != nil || item.Branch != "feature/x" || !item.IsStale {
		t.Errorf("GetMemoItemByHashPrefix(gone) = %+v, %v, want the stale row", item, err)
	}
}

// TestMemoCommentNotifications_excludesStaleComment pins invariant 3: a stale comment on the user's memo does not notify.
func TestMemoCommentNotifications_excludesStaleComment(t *testing.T) {
	setupTestDB(t)
	const repoURL, memo, comment = "https://github.com/test/stale-memo-comment", "3e3000000001", "3e3000000002"
	insertMemoTestCommit(t, repoURL, memo, MemoBranch, "me@x.com")
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: comment, RepoURL: repoURL, Branch: "gitmsg/social", AuthorName: "Other", AuthorEmail: "other@x.com",
		Message: "Looks good", Timestamp: time.Date(2025, 10, 21, 13, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	if err := social.InsertSocialItem(social.SocialItem{RepoURL: repoURL, Hash: comment, Branch: "gitmsg/social", Type: "comment",
		OriginalRepoURL: cache.ToNullString(repoURL), OriginalHash: cache.ToNullString(memo), OriginalBranch: cache.ToNullString(MemoBranch)}); err != nil {
		t.Fatalf("InsertSocialItem() error = %v", err)
	}
	if got, err := getMemoCommentNotifications("me@x.com", notifications.Filter{}); err != nil || len(got) != 1 {
		t.Fatalf("getMemoCommentNotifications() before the stale mark = %d, %v, want 1", len(got), err)
	}
	markStale(t, repoURL, comment, "gitmsg/social")
	if got, err := getMemoCommentNotifications("me@x.com", notifications.Filter{}); err != nil || len(got) != 0 {
		t.Errorf("getMemoCommentNotifications() after the stale mark = %d, %v, want 0", len(got), err)
	}
}
