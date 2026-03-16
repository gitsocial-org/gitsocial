// comment_test.go - Tests for release comment integration
package release

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/extensions/social"
)

func TestGetReleaseComments_socialQueryError(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/social-err"
	hash := "a0b0c0d0e0f0"
	branch := "gitmsg/release"
	insertReleaseTestCommit(t, repoURL, hash)
	InsertReleaseItem(ReleaseItem{RepoURL: repoURL, Hash: hash, Branch: branch})

	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec("DROP VIEW IF EXISTS social_items_resolved")
		return nil
	})

	refStr := repoURL + "#commit:" + hash + "@" + branch
	res := GetReleaseComments(refStr, repoURL)
	if res.Success {
		t.Error("should fail when social view is dropped")
	}
	if res.Error.Code != "QUERY_FAILED" {
		t.Errorf("Error.Code = %q, want QUERY_FAILED", res.Error.Code)
	}
}

func TestGetReleaseComments_notFound(t *testing.T) {
	setupTestDB(t)
	res := GetReleaseComments("#commit:nonexistent00@gitmsg/release", "https://github.com/test/repo")
	if res.Success {
		t.Error("should fail for non-existent release")
	}
	if res.Error.Code != "NOT_FOUND" {
		t.Errorf("Error.Code = %q, want NOT_FOUND", res.Error.Code)
	}
}

func TestGetReleaseComments_noComments(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/comments-repo"
	hash := "c0e0a0123456"
	branch := "gitmsg/release"
	insertReleaseTestCommit(t, repoURL, hash)
	InsertReleaseItem(ReleaseItem{RepoURL: repoURL, Hash: hash, Branch: branch, Tag: cache.ToNullString("v1.0.0")})

	refStr := repoURL + "#commit:" + hash + "@" + branch
	res := GetReleaseComments(refStr, repoURL)
	if !res.Success {
		t.Fatalf("GetReleaseComments() failed: %s", res.Error.Message)
	}
	if len(res.Data) != 0 {
		t.Errorf("expected 0 comments, got %d", len(res.Data))
	}
}

func TestGetReleaseComments_withComments(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/comments-repo2"
	releaseHash := "c0e1a0123456"
	branch := "gitmsg/release"
	socialBranch := "gitmsg/social"

	insertReleaseTestCommit(t, repoURL, releaseHash)
	InsertReleaseItem(ReleaseItem{RepoURL: repoURL, Hash: releaseHash, Branch: branch, Tag: cache.ToNullString("v1.0.0")})

	commentHash := "c0e2a0123456"
	cache.InsertCommits([]cache.Commit{{
		Hash:        commentHash,
		RepoURL:     repoURL,
		Branch:      socialBranch,
		AuthorName:  "Commenter",
		AuthorEmail: "commenter@test.com",
		Message:     "Great release!",
		Timestamp:   time.Date(2025, 10, 22, 12, 0, 0, 0, time.UTC),
	}})
	social.InsertSocialItem(social.SocialItem{
		RepoURL:         repoURL,
		Hash:            commentHash,
		Branch:          socialBranch,
		Type:            "comment",
		OriginalRepoURL: sql.NullString{String: repoURL, Valid: true},
		OriginalHash:    sql.NullString{String: releaseHash, Valid: true},
		OriginalBranch:  sql.NullString{String: branch, Valid: true},
	})

	refStr := repoURL + "#commit:" + releaseHash + "@" + branch
	res := GetReleaseComments(refStr, repoURL)
	if !res.Success {
		t.Fatalf("GetReleaseComments() failed: %s", res.Error.Message)
	}
	if len(res.Data) != 1 {
		t.Errorf("expected 1 comment, got %d", len(res.Data))
	}
}
