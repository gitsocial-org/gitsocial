// feedback_test.go - Tests for feedback CRUD and review summary
package review

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
)

func TestFeedbackOperations(t *testing.T) {
	t.Parallel()

	t.Run("CreateFeedback_reviewState", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		pr := CreatePR(dir, "Test PR", "", CreatePROptions{Base: "main", Head: "feature"})
		if !pr.Success {
			t.Fatalf("CreatePR() failed: %s", pr.Error.Message)
		}
		res := CreateFeedback(dir, "Looks good to me", CreateFeedbackOptions{
			PullRequest: pr.Data.ID,
			ReviewState: ReviewStateApproved,
		})
		if !res.Success {
			t.Fatalf("CreateFeedback() failed: %s", res.Error.Message)
		}
		if res.Data.ReviewState != ReviewStateApproved {
			t.Errorf("ReviewState = %q, want approved", res.Data.ReviewState)
		}
	})

	t.Run("CreateFeedback_inline", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		git.ExecGit(dir, []string{"checkout", "-b", "feature"})
		git.CreateCommit(dir, git.CommitOptions{Message: "Feature commit", AllowEmpty: true})
		git.ExecGit(dir, []string{"checkout", "main"})

		pr := CreatePR(dir, "Test PR", "", CreatePROptions{Base: "main", Head: "feature"})
		if !pr.Success {
			t.Fatalf("CreatePR() failed: %s", pr.Error.Message)
		}
		res := CreateFeedback(dir, "Fix this line", CreateFeedbackOptions{
			PullRequest: pr.Data.ID,
			Commit:      "abc123",
			File:        "main.go",
			NewLine:     42,
		})
		if !res.Success {
			t.Fatalf("CreateFeedback() failed: %s", res.Error.Message)
		}
		if res.Data.File != "main.go" {
			t.Errorf("File = %q, want main.go", res.Data.File)
		}
		if res.Data.NewLine != 42 {
			t.Errorf("NewLine = %d, want 42", res.Data.NewLine)
		}
	})

	t.Run("validationError_noLocationOrState", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		res := CreateFeedback(dir, "Comment without location or state", CreateFeedbackOptions{
			PullRequest: "#commit:pr123@gitmsg/review",
		})
		if res.Success {
			t.Error("should fail without location or review-state")
		}
		if res.Error.Code != "VALIDATION_ERROR" {
			t.Errorf("Error.Code = %q, want VALIDATION_ERROR", res.Error.Code)
		}
	})

	t.Run("validationError_incompleteInline", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		res := CreateFeedback(dir, "Incomplete inline", CreateFeedbackOptions{
			PullRequest: "#commit:pr123@gitmsg/review",
			File:        "main.go",
		})
		if res.Success {
			t.Error("should fail with incomplete inline fields")
		}
		if res.Error.Code != "VALIDATION_ERROR" {
			t.Errorf("Error.Code = %q, want VALIDATION_ERROR", res.Error.Code)
		}
	})

	t.Run("GetFeedback", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		pr := CreatePR(dir, "PR for feedback", "", CreatePROptions{Base: "main"})
		if !pr.Success {
			t.Fatalf("CreatePR() failed: %s", pr.Error.Message)
		}
		created := CreateFeedback(dir, "Nice work", CreateFeedbackOptions{
			PullRequest: pr.Data.ID,
			ReviewState: ReviewStateApproved,
		})
		if !created.Success {
			t.Fatalf("CreateFeedback() failed: %s", created.Error.Message)
		}
		res := GetFeedback(created.Data.ID)
		if !res.Success {
			t.Fatalf("GetFeedback() failed: %s", res.Error.Message)
		}
	})

	t.Run("UpdateFeedback", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		pr := CreatePR(dir, "PR", "", CreatePROptions{Base: "main"})
		if !pr.Success {
			t.Fatalf("CreatePR() failed: %s", pr.Error.Message)
		}
		created := CreateFeedback(dir, "Initial feedback", CreateFeedbackOptions{
			PullRequest: pr.Data.ID,
			ReviewState: ReviewStateChangesRequested,
		})
		if !created.Success {
			t.Fatalf("CreateFeedback() failed: %s", created.Error.Message)
		}
		newContent := "Updated feedback content"
		newState := ReviewStateApproved
		res := UpdateFeedback(dir, created.Data.ID, UpdateFeedbackOptions{
			Content:     &newContent,
			ReviewState: &newState,
		})
		if !res.Success {
			t.Fatalf("UpdateFeedback() failed: %s", res.Error.Message)
		}
	})

	t.Run("UpdateFeedback_notFound", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		res := UpdateFeedback(dir, "#commit:nonexistent00@gitmsg/review", UpdateFeedbackOptions{})
		if res.Success {
			t.Error("should fail for non-existent feedback")
		}
		if res.Error.Code != "NOT_FOUND" {
			t.Errorf("Error.Code = %q, want NOT_FOUND", res.Error.Code)
		}
	})

	t.Run("RetractFeedback", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		pr := CreatePR(dir, "PR", "", CreatePROptions{Base: "main"})
		if !pr.Success {
			t.Fatalf("CreatePR() failed: %s", pr.Error.Message)
		}
		created := CreateFeedback(dir, "Retractable", CreateFeedbackOptions{
			PullRequest: pr.Data.ID,
			ReviewState: ReviewStateApproved,
		})
		if !created.Success {
			t.Fatalf("CreateFeedback() failed: %s", created.Error.Message)
		}
		res := RetractFeedback(dir, created.Data.ID)
		if !res.Success {
			t.Fatalf("RetractFeedback() failed: %s", res.Error.Message)
		}
		if !res.Data {
			t.Error("RetractFeedback() should return true")
		}
	})

	t.Run("RetractFeedback_notFound", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		res := RetractFeedback(dir, "#commit:nonexistent00@gitmsg/review")
		if res.Success {
			t.Error("should fail for non-existent feedback")
		}
		if res.Error.Code != "NOT_FOUND" {
			t.Errorf("Error.Code = %q, want NOT_FOUND", res.Error.Code)
		}
	})
}

func TestCreateFeedback_commitFailed(t *testing.T) {
	setupTestDB(t)
	res := CreateFeedback(t.TempDir(), "Feedback", CreateFeedbackOptions{ReviewState: ReviewStateApproved})
	if res.Success {
		t.Error("should fail for non-git directory")
	}
	if res.Error.Code != "COMMIT_FAILED" {
		t.Errorf("Error.Code = %q, want COMMIT_FAILED", res.Error.Code)
	}
}

func TestGetFeedback_notFound(t *testing.T) {
	setupTestDB(t)
	res := GetFeedback("nonexistent")
	if res.Success {
		t.Error("GetFeedback() should fail for non-existent feedback")
	}
	if res.Error.Code != "NOT_FOUND" {
		t.Errorf("Error.Code = %q, want NOT_FOUND", res.Error.Code)
	}
}

func TestFeedbackCommitFailedPaths(t *testing.T) {
	setupTestDB(t)

	t.Run("UpdateFeedback_commitFailed", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		pr := CreatePR(dir, "PR", "", CreatePROptions{Base: "main"})
		if !pr.Success {
			t.Fatalf("CreatePR() failed: %s", pr.Error.Message)
		}
		created := CreateFeedback(dir, "Feedback", CreateFeedbackOptions{
			PullRequest: pr.Data.ID,
			ReviewState: ReviewStateApproved,
		})
		if !created.Success {
			t.Fatalf("CreateFeedback() failed: %s", created.Error.Message)
		}
		os.RemoveAll(filepath.Join(dir, ".git", "objects"))
		newContent := "Updated"
		res := UpdateFeedback(dir, created.Data.ID, UpdateFeedbackOptions{Content: &newContent})
		if res.Success {
			t.Error("should fail with corrupted git repo")
		}
		if res.Error.Code != "COMMIT_FAILED" {
			t.Errorf("Error.Code = %q, want COMMIT_FAILED", res.Error.Code)
		}
	})

	t.Run("RetractFeedback_commitFailed", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		pr := CreatePR(dir, "PR", "", CreatePROptions{Base: "main"})
		if !pr.Success {
			t.Fatalf("CreatePR() failed: %s", pr.Error.Message)
		}
		created := CreateFeedback(dir, "Feedback", CreateFeedbackOptions{
			PullRequest: pr.Data.ID,
			ReviewState: ReviewStateApproved,
		})
		if !created.Success {
			t.Fatalf("CreateFeedback() failed: %s", created.Error.Message)
		}
		os.RemoveAll(filepath.Join(dir, ".git", "objects"))
		res := RetractFeedback(dir, created.Data.ID)
		if res.Success {
			t.Error("should fail with corrupted git repo")
		}
		if res.Error.Code != "COMMIT_FAILED" {
			t.Errorf("Error.Code = %q, want COMMIT_FAILED", res.Error.Code)
		}
	})
}

func TestGetReviewSummary(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	prHash := "summ_1234567"
	insertReviewTestCommit(t, repoURL, prHash)
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: prHash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	fb1Hash := "sfb1_1234567"
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: fb1Hash, RepoURL: repoURL, Branch: reviewTestBranch,
		AuthorName: "Alice", AuthorEmail: "alice@test.com", Message: "Approved",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertReviewItem(ReviewItem{
		RepoURL: repoURL, Hash: fb1Hash, Branch: reviewTestBranch, Type: "feedback",
		PullRequestRepoURL: cache.ToNullString(repoURL), PullRequestHash: cache.ToNullString(prHash), PullRequestBranch: cache.ToNullString(reviewTestBranch),
		ReviewStateField: cache.ToNullString("approved"),
	})

	fb2Hash := "sfb2_1234567"
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: fb2Hash, RepoURL: repoURL, Branch: reviewTestBranch,
		AuthorName: "Bob", AuthorEmail: "bob@test.com", Message: "Changes",
		Timestamp: time.Date(2025, 10, 21, 13, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertReviewItem(ReviewItem{
		RepoURL: repoURL, Hash: fb2Hash, Branch: reviewTestBranch, Type: "feedback",
		PullRequestRepoURL: cache.ToNullString(repoURL), PullRequestHash: cache.ToNullString(prHash), PullRequestBranch: cache.ToNullString(reviewTestBranch),
		ReviewStateField: cache.ToNullString("changes-requested"),
	})

	summary := GetReviewSummary(repoURL, prHash, reviewTestBranch, []string{"alice@test.com", "bob@test.com", "carol@test.com"})
	if summary.Approved != 1 {
		t.Errorf("Approved = %d, want 1", summary.Approved)
	}
	if summary.ChangesRequested != 1 {
		t.Errorf("ChangesRequested = %d, want 1", summary.ChangesRequested)
	}
	if summary.Pending != 1 {
		t.Errorf("Pending = %d, want 1 (carol hasn't reviewed)", summary.Pending)
	}
	if !summary.IsBlocked {
		t.Error("IsBlocked should be true (has changes-requested)")
	}
	if summary.IsApproved {
		t.Error("IsApproved should be false (has changes-requested)")
	}
}

func TestGetReviewSummary_queryError(t *testing.T) {
	cache.Reset()
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})
	summary := GetReviewSummary("url", "hash", "branch", nil)
	if summary.Approved != 0 {
		t.Error("should return empty summary when cache is not open")
	}
}

func TestGetReviewSummary_noFeedback(t *testing.T) {
	setupTestDB(t)
	summary := GetReviewSummary(reviewTestRepoURL, "nonexistent", reviewTestBranch, nil)
	if summary.Approved != 0 || summary.ChangesRequested != 0 || summary.Pending != 0 {
		t.Error("should be empty summary")
	}
}

func TestGetReviewSummary_allApproved(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/allapp"
	prHash := "appr_1234567"
	insertReviewTestCommit(t, repoURL, prHash)
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: prHash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	for i, email := range []string{"a@t.com", "b@t.com"} {
		hash := []string{"ap1_12345678", "ap2_12345678"}[i]
		if err := cache.InsertCommits([]cache.Commit{{
			Hash: hash, RepoURL: repoURL, Branch: reviewTestBranch,
			AuthorName: "R", AuthorEmail: email, Message: "ok",
			Timestamp: time.Date(2025, 10, 21, 12+i, 0, 0, 0, time.UTC),
		}}); err != nil {
			t.Fatal(err)
		}
		InsertReviewItem(ReviewItem{
			RepoURL: repoURL, Hash: hash, Branch: reviewTestBranch, Type: "feedback",
			PullRequestRepoURL: cache.ToNullString(repoURL), PullRequestHash: cache.ToNullString(prHash), PullRequestBranch: cache.ToNullString(reviewTestBranch),
			ReviewStateField: cache.ToNullString("approved"),
		})
	}

	summary := GetReviewSummary(repoURL, prHash, reviewTestBranch, []string{"a@t.com", "b@t.com"})
	if !summary.IsApproved {
		t.Error("IsApproved should be true when all reviewers approved")
	}
	if summary.IsBlocked {
		t.Error("IsBlocked should be false")
	}
}

func TestGetFeedback_queryError(t *testing.T) {
	cache.Reset()
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})
	res := GetFeedback("anything")
	if res.Success {
		t.Error("should fail when cache is not open")
	}
}

func TestGetReviewSummary_feedbackWithNoReviewState(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/nors"
	prHash := "a0b012345678"
	insertReviewTestCommit(t, repoURL, prHash)
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: prHash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	fbHash := "a0c012345678"
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: fbHash, RepoURL: repoURL, Branch: reviewTestBranch,
		AuthorName: "Reviewer", AuthorEmail: "reviewer@test.com", Message: "Comment",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertReviewItem(ReviewItem{
		RepoURL: repoURL, Hash: fbHash, Branch: reviewTestBranch, Type: "feedback",
		PullRequestRepoURL: cache.ToNullString(repoURL), PullRequestHash: cache.ToNullString(prHash), PullRequestBranch: cache.ToNullString(reviewTestBranch),
	})

	summary := GetReviewSummary(repoURL, prHash, reviewTestBranch, []string{"reviewer@test.com"})
	if summary.Approved != 0 {
		t.Errorf("Approved = %d, want 0 (no review-state feedback)", summary.Approved)
	}
	if summary.ChangesRequested != 0 {
		t.Errorf("ChangesRequested = %d, want 0", summary.ChangesRequested)
	}
	if summary.Pending != 1 {
		t.Errorf("Pending = %d, want 1 (reviewer has no review-state)", summary.Pending)
	}
}

func TestGetReviewSummary_latestStateWins(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/latest"
	prHash := "ltst_1234567"
	insertReviewTestCommit(t, repoURL, prHash)
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: prHash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	for i, state := range []string{"changes-requested", "approved"} {
		hash := []string{"lt1_12345678", "lt2_12345678"}[i]
		if err := cache.InsertCommits([]cache.Commit{{
			Hash: hash, RepoURL: repoURL, Branch: reviewTestBranch,
			AuthorName: "Alice", AuthorEmail: "alice@t.com", Message: "review",
			Timestamp: time.Date(2025, 10, 21, 12+i, 0, 0, 0, time.UTC),
		}}); err != nil {
			t.Fatal(err)
		}
		InsertReviewItem(ReviewItem{
			RepoURL: repoURL, Hash: hash, Branch: reviewTestBranch, Type: "feedback",
			PullRequestRepoURL: cache.ToNullString(repoURL), PullRequestHash: cache.ToNullString(prHash), PullRequestBranch: cache.ToNullString(reviewTestBranch),
			ReviewStateField: cache.ToNullString(state),
		})
	}

	summary := GetReviewSummary(repoURL, prHash, reviewTestBranch, []string{"alice@t.com"})
	if summary.Approved != 1 {
		t.Errorf("Approved = %d, want 1 (latest state wins)", summary.Approved)
	}
	if summary.ChangesRequested != 0 {
		t.Errorf("ChangesRequested = %d, want 0", summary.ChangesRequested)
	}
}
