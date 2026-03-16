// comment_test.go - Tests for review comment integration with social extension
package review

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/extensions/social"
)

func TestGetPRComments_notFound(t *testing.T) {
	setupTestDB(t)
	res := GetPRComments("#commit:aaaa00000000@gitmsg/review", reviewTestRepoURL)
	if res.Success {
		t.Error("should fail for non-existent PR")
	}
	if res.Error.Code != "NOT_FOUND" {
		t.Errorf("Error.Code = %q, want NOT_FOUND", res.Error.Code)
	}
}

func TestGetPRComments_noComments(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/comments-repo"
	hash := "c0e012345678"
	insertReviewTestCommit(t, repoURL, hash)
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: hash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	refStr := repoURL + "#commit:" + hash + "@" + reviewTestBranch
	res := GetPRComments(refStr, repoURL)
	if !res.Success {
		t.Fatalf("GetPRComments() failed: %s", res.Error.Message)
	}
	if len(res.Data) != 0 {
		t.Errorf("expected 0 comments, got %d", len(res.Data))
	}
}

func TestGetPRComments_withComments(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/cmt-repo2"
	prHash := "c1e112345678"
	socialBranch := "gitmsg/social"
	insertReviewTestCommit(t, repoURL, prHash)
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: prHash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	commentHash := "c2e212345678"
	cache.InsertCommits([]cache.Commit{{
		Hash: commentHash, RepoURL: repoURL, Branch: socialBranch,
		AuthorName: "Commenter", AuthorEmail: "commenter@test.com",
		Message: "Great PR!", Timestamp: time.Date(2025, 10, 22, 12, 0, 0, 0, time.UTC),
	}})
	social.InsertSocialItem(social.SocialItem{
		RepoURL: repoURL, Hash: commentHash, Branch: socialBranch, Type: "comment",
		OriginalRepoURL: sql.NullString{String: repoURL, Valid: true},
		OriginalHash:    sql.NullString{String: prHash, Valid: true},
		OriginalBranch:  sql.NullString{String: reviewTestBranch, Valid: true},
	})

	refStr := repoURL + "#commit:" + prHash + "@" + reviewTestBranch
	res := GetPRComments(refStr, repoURL)
	if !res.Success {
		t.Fatalf("GetPRComments() failed: %s", res.Error.Message)
	}
	if len(res.Data) != 1 {
		t.Errorf("expected 1 comment, got %d", len(res.Data))
	}
}

func TestGetPRComments_socialQueryError(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/sqerr"
	hash := "c3e312345678"
	insertReviewTestCommit(t, repoURL, hash)
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: hash, Branch: reviewTestBranch, Type: "pull-request"})

	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec("DROP VIEW IF EXISTS social_items_resolved")
		return nil
	})

	refStr := repoURL + "#commit:" + hash + "@" + reviewTestBranch
	res := GetPRComments(refStr, repoURL)
	if res.Success {
		t.Error("should fail when social view is dropped")
	}
	if res.Error.Code != "QUERY_FAILED" {
		t.Errorf("Error.Code = %q, want QUERY_FAILED", res.Error.Code)
	}
}

func TestGetFeedbackComments_notFound(t *testing.T) {
	setupTestDB(t)
	res := GetFeedbackComments("#commit:aaaa00000001@gitmsg/review", reviewTestRepoURL)
	if res.Success {
		t.Error("should fail for non-existent feedback")
	}
	if res.Error.Code != "NOT_FOUND" {
		t.Errorf("Error.Code = %q, want NOT_FOUND", res.Error.Code)
	}
}

func TestGetFeedbackComments_noComments(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/fbcmt-repo"
	hash := "fc0e12345678"
	insertReviewTestCommit(t, repoURL, hash)
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: hash, Branch: reviewTestBranch, Type: "feedback",
		ReviewStateField: cache.ToNullString("approved")})

	refStr := repoURL + "#commit:" + hash + "@" + reviewTestBranch
	res := GetFeedbackComments(refStr, repoURL)
	if !res.Success {
		t.Fatalf("GetFeedbackComments() failed: %s", res.Error.Message)
	}
	if len(res.Data) != 0 {
		t.Errorf("expected 0 comments, got %d", len(res.Data))
	}
}

func TestGetFeedbackComments_socialQueryError(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/fbsqerr"
	hash := "fc4e42345678"
	insertReviewTestCommit(t, repoURL, hash)
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: hash, Branch: reviewTestBranch, Type: "feedback"})

	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec("DROP VIEW IF EXISTS social_items_resolved")
		return nil
	})

	refStr := repoURL + "#commit:" + hash + "@" + reviewTestBranch
	res := GetFeedbackComments(refStr, repoURL)
	if res.Success {
		t.Error("should fail when social view is dropped")
	}
	if res.Error.Code != "QUERY_FAILED" {
		t.Errorf("Error.Code = %q, want QUERY_FAILED", res.Error.Code)
	}
}

func TestGetFeedbackComments_withComments(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/fbcmt-repo2"
	fbHash := "fc2e22345678"
	socialBranch := "gitmsg/social"
	insertReviewTestCommit(t, repoURL, fbHash)
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: fbHash, Branch: reviewTestBranch, Type: "feedback",
		ReviewStateField: cache.ToNullString("approved")})

	commentHash := "fc3e32345678"
	cache.InsertCommits([]cache.Commit{{
		Hash: commentHash, RepoURL: repoURL, Branch: socialBranch,
		AuthorName: "Reply", AuthorEmail: "reply@test.com",
		Message: "Thanks!", Timestamp: time.Date(2025, 10, 22, 12, 0, 0, 0, time.UTC),
	}})
	social.InsertSocialItem(social.SocialItem{
		RepoURL: repoURL, Hash: commentHash, Branch: socialBranch, Type: "comment",
		OriginalRepoURL: sql.NullString{String: repoURL, Valid: true},
		OriginalHash:    sql.NullString{String: fbHash, Valid: true},
		OriginalBranch:  sql.NullString{String: reviewTestBranch, Valid: true},
	})

	refStr := repoURL + "#commit:" + fbHash + "@" + reviewTestBranch
	res := GetFeedbackComments(refStr, repoURL)
	if !res.Success {
		t.Fatalf("GetFeedbackComments() failed: %s", res.Error.Message)
	}
	if len(res.Data) != 1 {
		t.Errorf("expected 1 comment, got %d", len(res.Data))
	}
}
