// review_test.go - Tests for pull request CRUD and review config
package review

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

func TestPROperations(t *testing.T) {
	t.Parallel()

	t.Run("CreatePR", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		res := CreatePR(dir, "Add login feature", "Implements OAuth2", CreatePROptions{
			Base:      "main",
			Head:      "feature",
			Reviewers: []string{"bob@test.com"},
		})
		if !res.Success {
			t.Fatalf("CreatePR() failed: %s", res.Error.Message)
		}
		if res.Data.Subject != "Add login feature" {
			t.Errorf("Subject = %q", res.Data.Subject)
		}
		if res.Data.Body != "Implements OAuth2" {
			t.Errorf("Body = %q", res.Data.Body)
		}
		if res.Data.State != PRStateOpen {
			t.Errorf("State = %q, want open", res.Data.State)
		}
		if len(res.Data.Reviewers) != 1 {
			t.Errorf("len(Reviewers) = %d, want 1", len(res.Data.Reviewers))
		}
	})

	t.Run("GetPR", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		created := CreatePR(dir, "Test PR", "", CreatePROptions{Base: "main", Head: "feature"})
		if !created.Success {
			t.Fatalf("CreatePR() failed: %s", created.Error.Message)
		}
		res := GetPR(created.Data.ID)
		if !res.Success {
			t.Fatalf("GetPR() failed: %s", res.Error.Message)
		}
		if res.Data.Subject != "Test PR" {
			t.Errorf("Subject = %q", res.Data.Subject)
		}
	})

	t.Run("GetPR_byHashPrefix", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		created := CreatePR(dir, "Prefix PR", "", CreatePROptions{Base: "main", Head: "feature"})
		if !created.Success {
			t.Fatalf("CreatePR() failed: %s", created.Error.Message)
		}
		hash := created.Data.ID
		if len(hash) > 12 {
			hash = hash[:12]
		}
		res := GetPR(hash)
		if !res.Success {
			t.Fatalf("GetPR() with prefix failed: %s", res.Error.Message)
		}
	})

	t.Run("GetPR_byRawHashPrefix", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		created := CreatePR(dir, "Hash PR", "", CreatePROptions{Base: "main"})
		if !created.Success {
			t.Fatalf("CreatePR() failed: %s", created.Error.Message)
		}
		parsed := protocol.ParseRef(created.Data.ID)
		prefix := parsed.Value[:6]
		res := GetPR(prefix)
		if !res.Success {
			t.Fatalf("GetPR() by raw hash prefix failed: %s", res.Error.Message)
		}
		if res.Data.Subject != "Hash PR" {
			t.Errorf("Subject = %q", res.Data.Subject)
		}
	})

	t.Run("UpdatePR", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		created := CreatePR(dir, "Original PR", "Original body", CreatePROptions{Base: "main", Head: "feature"})
		if !created.Success {
			t.Fatalf("CreatePR() failed: %s", created.Error.Message)
		}
		newSubject := "Updated PR"
		newBody := "Updated body"
		newReviewers := []string{"alice@test.com", "bob@test.com"}
		res := UpdatePR(dir, created.Data.ID, UpdatePROptions{
			Subject:   &newSubject,
			Body:      &newBody,
			Reviewers: &newReviewers,
		})
		if !res.Success {
			t.Fatalf("UpdatePR() failed: %s", res.Error.Message)
		}
	})

	t.Run("UpdatePR_notFound", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		res := UpdatePR(dir, "#commit:nonexistent00@gitmsg/review", UpdatePROptions{})
		if res.Success {
			t.Error("UpdatePR() should fail for non-existent PR")
		}
		if res.Error.Code != "NOT_FOUND" {
			t.Errorf("Error.Code = %q, want NOT_FOUND", res.Error.Code)
		}
	})

	t.Run("UpdatePR_allFields", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		created := CreatePR(dir, "PR", "", CreatePROptions{Base: "main", Head: "feature"})
		if !created.Success {
			t.Fatalf("CreatePR failed: %s", created.Error.Message)
		}
		state := PRStateMerged
		base := "develop"
		head := "hotfix"
		closes := []string{"#commit:iss1"}
		reviewers := []string{"alice@test.com"}
		subject := "New Subject"
		body := "New Body"
		mergeBase := "aaa111"
		mergeHead := "bbb222"
		res := UpdatePR(dir, created.Data.ID, UpdatePROptions{
			State: &state, Base: &base, Head: &head, Closes: &closes,
			Reviewers: &reviewers, Subject: &subject, Body: &body,
			MergeBase: &mergeBase, MergeHead: &mergeHead,
		})
		if !res.Success {
			t.Fatalf("UpdatePR() failed: %s", res.Error.Message)
		}
	})

	t.Run("ClosePR", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		created := CreatePR(dir, "Closeable PR", "", CreatePROptions{Base: "main", Head: "feature"})
		if !created.Success {
			t.Fatalf("CreatePR() failed: %s", created.Error.Message)
		}
		res := ClosePR(dir, created.Data.ID)
		if !res.Success {
			t.Fatalf("ClosePR() failed: %s", res.Error.Message)
		}
		if res.Data.State != PRStateClosed {
			t.Errorf("State = %q, want closed", res.Data.State)
		}
	})

	t.Run("ClosePR_invalidState", func(t *testing.T) {
		dir := initTestRepo(t)
		created := CreatePR(dir, "PR to close twice", "", CreatePROptions{Base: "main"})
		if !created.Success {
			t.Fatalf("CreatePR() failed: %s", created.Error.Message)
		}
		first := ClosePR(dir, created.Data.ID)
		if !first.Success {
			t.Fatalf("first ClosePR() failed: %s", first.Error.Message)
		}
		res := ClosePR(dir, created.Data.ID)
		if res.Success {
			t.Error("should fail when closing an already closed PR")
		}
		if res.Error.Code != "INVALID_STATE" {
			t.Errorf("Error.Code = %q, want INVALID_STATE", res.Error.Code)
		}
	})

	t.Run("ClosePR_notFound", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		res := ClosePR(dir, "#commit:nonexistent00@gitmsg/review")
		if res.Success {
			t.Error("should fail for non-existent PR")
		}
		if res.Error.Code != "NOT_FOUND" {
			t.Errorf("Error.Code = %q, want NOT_FOUND", res.Error.Code)
		}
	})

	t.Run("RetractPR", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		created := CreatePR(dir, "Retractable PR", "", CreatePROptions{Base: "main"})
		if !created.Success {
			t.Fatalf("CreatePR() failed: %s", created.Error.Message)
		}
		res := RetractPR(dir, created.Data.ID)
		if !res.Success {
			t.Fatalf("RetractPR() failed: %s", res.Error.Message)
		}
		if !res.Data {
			t.Error("RetractPR() should return true")
		}
	})

	t.Run("RetractPR_notFound", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		res := RetractPR(dir, "#commit:nonexistent00@gitmsg/review")
		if res.Success {
			t.Error("should fail for non-existent PR")
		}
		if res.Error.Code != "NOT_FOUND" {
			t.Errorf("Error.Code = %q, want NOT_FOUND", res.Error.Code)
		}
	})

	t.Run("CreatePR_withCloses", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		res := CreatePR(dir, "Fix bugs", "", CreatePROptions{
			Base:   "main",
			Head:   "feature",
			Closes: []string{"#commit:aabb11223344@gitmsg/pm"},
		})
		if !res.Success {
			t.Fatalf("CreatePR() failed: %s", res.Error.Message)
		}
		if len(res.Data.Closes) != 1 {
			t.Errorf("len(Closes) = %d, want 1", len(res.Data.Closes))
		}
	})

	t.Run("CacheReviewFromCommit_nonReview", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		hash, err := git.CreateCommitOnBranch(dir, "gitmsg/review", "Just a plain commit")
		if err != nil {
			t.Fatalf("CreateCommitOnBranch() error = %v", err)
		}
		err = cacheReviewFromCommit(dir, "https://github.com/test/repo", hash, "gitmsg/review")
		if err != nil {
			t.Fatalf("cacheReviewFromCommit() error = %v", err)
		}
		count, qErr := cache.QueryLocked(func(db *sql.DB) (int, error) {
			var c int
			err := db.QueryRow(`SELECT COUNT(*) FROM review_items WHERE hash = ?`, hash).Scan(&c)
			return c, err
		})
		if qErr != nil {
			t.Fatalf("query error = %v", qErr)
		}
		if count != 0 {
			t.Errorf("expected 0 review_items for non-review commit, got %d", count)
		}
	})

	t.Run("CacheReviewFromCommit_gitError", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		err := cacheReviewFromCommit(dir, "https://github.com/test/repo", "nonexistenthash", "gitmsg/review")
		if err == nil {
			t.Error("should fail with invalid hash")
		}
	})

	t.Run("CacheReviewFromCommit_reviewCommit", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		content := "Test PR\n\n" + `GitMsg: ext="review"; type="pull-request"; state="open"; v="0.1.0"`
		hash, err := git.CreateCommitOnBranch(dir, "gitmsg/review", content)
		if err != nil {
			t.Fatalf("CreateCommitOnBranch() error = %v", err)
		}
		err = cacheReviewFromCommit(dir, "https://github.com/test/repo", hash, "gitmsg/review")
		if err != nil {
			t.Fatalf("cacheReviewFromCommit() error = %v", err)
		}
		count, qErr := cache.QueryLocked(func(db *sql.DB) (int, error) {
			var c int
			err := db.QueryRow(`SELECT COUNT(*) FROM review_items WHERE hash = ?`, hash).Scan(&c)
			return c, err
		})
		if qErr != nil {
			t.Fatalf("query error = %v", qErr)
		}
		if count < 1 {
			t.Errorf("expected at least 1 review item, got %d", count)
		}
	})
}

func TestCreatePR_cacheFailed(t *testing.T) {
	cache.Reset()
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})
	dir := initTestRepo(t)

	res := CreatePR(dir, "PR", "", CreatePROptions{})
	if res.Success {
		t.Error("should fail with cache not open")
	}
	if res.Error.Code != "CACHE_FAILED" {
		t.Errorf("Error.Code = %q, want CACHE_FAILED", res.Error.Code)
	}
}

func TestGetPR_notFound(t *testing.T) {
	setupTestDB(t)
	res := GetPR("nonexistent")
	if res.Success {
		t.Error("GetPR() should fail for non-existent PR")
	}
	if res.Error.Code != "NOT_FOUND" {
		t.Errorf("Error.Code = %q, want NOT_FOUND", res.Error.Code)
	}
}

func TestGetPR_queryError(t *testing.T) {
	cache.Reset()
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})
	res := GetPR("anything")
	if res.Success {
		t.Error("should fail when cache is not open")
	}
}

func TestCacheReviewFromCommit_cacheError(t *testing.T) {
	dir := initTestRepo(t)
	hash, err := git.CreateCommitOnBranch(dir, "gitmsg/review", "Test commit")
	if err != nil {
		t.Fatalf("CreateCommitOnBranch() error = %v", err)
	}

	cache.Reset()
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})

	err = cacheReviewFromCommit(dir, "https://github.com/test/repo", hash, "gitmsg/review")
	if err == nil {
		t.Error("should fail with cache not open")
	}
}

func TestCommitFailedPaths(t *testing.T) {
	setupTestDB(t)

	t.Run("CreatePR_commitFailed", func(t *testing.T) {
		t.Parallel()
		res := CreatePR(t.TempDir(), "PR", "", CreatePROptions{})
		if res.Success {
			t.Error("should fail for non-git directory")
		}
		if res.Error.Code != "COMMIT_FAILED" {
			t.Errorf("Error.Code = %q, want COMMIT_FAILED", res.Error.Code)
		}
	})

	t.Run("UpdatePR_commitFailed", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		created := CreatePR(dir, "PR", "", CreatePROptions{Base: "main"})
		if !created.Success {
			t.Fatalf("CreatePR failed: %s", created.Error.Message)
		}
		os.RemoveAll(filepath.Join(dir, ".git", "objects"))
		res := UpdatePR(dir, created.Data.ID, UpdatePROptions{})
		if res.Success {
			t.Error("should fail with corrupted git repo")
		}
		if res.Error.Code != "COMMIT_FAILED" {
			t.Errorf("Error.Code = %q, want COMMIT_FAILED", res.Error.Code)
		}
	})

	t.Run("RetractPR_commitFailed", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		created := CreatePR(dir, "PR", "", CreatePROptions{Base: "main"})
		if !created.Success {
			t.Fatalf("CreatePR failed: %s", created.Error.Message)
		}
		os.RemoveAll(filepath.Join(dir, ".git", "objects"))
		res := RetractPR(dir, created.Data.ID)
		if res.Success {
			t.Error("should fail with corrupted git repo")
		}
		if res.Error.Code != "COMMIT_FAILED" {
			t.Errorf("Error.Code = %q, want COMMIT_FAILED", res.Error.Code)
		}
	})

	t.Run("MergePR_mergeFailed", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		git.ExecGit(dir, []string{"checkout", "-b", "conflict"})
		git.CreateCommit(dir, git.CommitOptions{Message: "Conflict commit", AllowEmpty: true})
		git.ExecGit(dir, []string{"checkout", "main"})
		res := CreatePR(dir, "PR", "", CreatePROptions{Base: "main", Head: "conflict"})
		if !res.Success {
			t.Fatalf("CreatePR() failed: %s", res.Error.Message)
		}
		os.RemoveAll(filepath.Join(dir, ".git", "refs", "heads", "conflict"))
		mergeRes := MergePR(dir, res.Data.ID, MergeStrategyFF)
		if mergeRes.Success {
			t.Error("should fail when merge fails")
		}
	})

	t.Run("CacheReviewFromCommit_gitGetCommitError", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		err := cacheReviewFromCommit(dir, "https://github.com/test/repo", "abc123456789", "gitmsg/review")
		if err == nil {
			t.Error("should fail when workdir is not a git repo")
		}
	})
}

func TestMergeOperations(t *testing.T) {
	t.Run("MergePR", func(t *testing.T) {
		dir := initTestRepo(t)
		git.ExecGit(dir, []string{"checkout", "-b", "feature"})
		git.CreateCommit(dir, git.CommitOptions{Message: "Feature commit", AllowEmpty: true})
		git.ExecGit(dir, []string{"checkout", "main"})

		res := CreatePR(dir, "Merge feature", "Merge it", CreatePROptions{
			Base: "main",
			Head: "feature",
		})
		if !res.Success {
			t.Fatalf("CreatePR() failed: %s", res.Error.Message)
		}
		mergeRes := MergePR(dir, res.Data.ID, MergeStrategyFF)
		if !mergeRes.Success {
			t.Fatalf("MergePR() failed: %s", mergeRes.Error.Message)
		}
		if mergeRes.Data.State != PRStateMerged {
			t.Errorf("State = %q, want merged", mergeRes.Data.State)
		}
	})

	t.Run("MergePR_invalidState", func(t *testing.T) {
		dir := initTestRepo(t)
		created := CreatePR(dir, "PR", "", CreatePROptions{Base: "main"})
		if !created.Success {
			t.Fatalf("CreatePR() failed: %s", created.Error.Message)
		}
		ClosePR(dir, created.Data.ID)
		res := MergePR(dir, created.Data.ID, MergeStrategyFF)
		if res.Success {
			t.Error("should fail when merging a closed PR")
		}
		if res.Error.Code != "INVALID_STATE" {
			t.Errorf("Error.Code = %q, want INVALID_STATE", res.Error.Code)
		}
	})

	t.Run("MergePR_notFound", func(t *testing.T) {
		dir := initTestRepo(t)
		res := MergePR(dir, "#commit:nonexistent00@gitmsg/review", MergeStrategyFF)
		if res.Success {
			t.Error("should fail for non-existent PR")
		}
		if res.Error.Code != "NOT_FOUND" {
			t.Errorf("Error.Code = %q, want NOT_FOUND", res.Error.Code)
		}
	})

	t.Run("MergePR_withPMCloses", func(t *testing.T) {
		dir := initTestRepo(t)
		git.ExecGit(dir, []string{"checkout", "-b", "feature2"})
		git.CreateCommit(dir, git.CommitOptions{Message: "Feature 2", AllowEmpty: true})
		git.ExecGit(dir, []string{"checkout", "main"})

		res := CreatePR(dir, "Closes issue", "", CreatePROptions{
			Base:   "main",
			Head:   "feature2",
			Closes: []string{"#commit:aabb11223344@gitmsg/pm"},
		})
		if !res.Success {
			t.Fatalf("CreatePR() failed: %s", res.Error.Message)
		}
		mergeRes := MergePR(dir, res.Data.ID, MergeStrategyFF)
		if !mergeRes.Success {
			t.Fatalf("MergePR() failed: %s", mergeRes.Error.Message)
		}
	})

	t.Run("MergePR_remoteHead", func(t *testing.T) {
		dir := initTestRepo(t)
		res := CreatePR(dir, "Remote head PR", "", CreatePROptions{
			Base:                 "#branch:main",
			Head:                 "https://github.com/remote/repo#branch:feature",
			AllowUnpublishedHead: true, // remote URL intentionally unreachable
		})
		if !res.Success {
			t.Fatalf("CreatePR() failed: %s", res.Error.Message)
		}
		mergeRes := MergePR(dir, res.Data.ID, MergeStrategyFF)
		if mergeRes.Success {
			t.Error("should fail when cannot fetch remote head")
		}
		if mergeRes.Error.Code != "HEAD_NOT_FOUND" {
			t.Errorf("Error.Code = %q, want HEAD_NOT_FOUND", mergeRes.Error.Code)
		}
	})
}

func TestForkMerge(t *testing.T) {
	setupTestDB(t)

	t.Run("forkPR", func(t *testing.T) {
		dir := initTestRepo(t)
		repoURL := "https://github.com/test/repo"
		forkURL := "https://github.com/fork/repo"

		git.ExecGit(dir, []string{"checkout", "-b", "feature3"})
		git.CreateCommit(dir, git.CommitOptions{Message: "Fork feature", AllowEmpty: true})
		git.ExecGit(dir, []string{"checkout", "main"})

		forkHash := "deadbeef0123"
		insertReviewTestCommit(t, forkURL, forkHash)
		InsertReviewItem(ReviewItem{
			RepoURL: forkURL, Hash: forkHash, Branch: reviewTestBranch,
			Type: "pull-request", State: cache.ToNullString("open"),
			Base: cache.ToNullString("#branch:main"),
			Head: cache.ToNullString(repoURL + "#branch:feature3"),
		})

		forkRef := forkURL + "#commit:" + forkHash + "@" + reviewTestBranch
		mergeRes := MergePR(dir, forkRef, MergeStrategyFF)
		if !mergeRes.Success {
			t.Fatalf("MergePR() for fork PR failed: %s", mergeRes.Error.Message)
		}
		if mergeRes.Data.State != PRStateMerged {
			t.Errorf("State = %q, want merged", mergeRes.Data.State)
		}
	})

	t.Run("forkPR_withCloses", func(t *testing.T) {
		dir := initTestRepo(t)
		repoURL := "https://github.com/test/repo"
		forkURL := "https://github.com/fork/closes"

		git.ExecGit(dir, []string{"checkout", "-b", "feature4"})
		git.CreateCommit(dir, git.CommitOptions{Message: "Fork feature 4", AllowEmpty: true})
		git.ExecGit(dir, []string{"checkout", "main"})

		forkHash := "deadbeef0124"
		insertReviewTestCommit(t, forkURL, forkHash)
		InsertReviewItem(ReviewItem{
			RepoURL: forkURL, Hash: forkHash, Branch: reviewTestBranch,
			Type: "pull-request", State: cache.ToNullString("open"),
			Base:   cache.ToNullString("#branch:main"),
			Head:   cache.ToNullString(repoURL + "#branch:feature4"),
			Closes: cache.ToNullString("#commit:aabb11223344@gitmsg/pm"),
		})

		forkRef := forkURL + "#commit:" + forkHash + "@" + reviewTestBranch
		mergeRes := MergePR(dir, forkRef, MergeStrategyFF)
		if !mergeRes.Success {
			t.Fatalf("MergePR() for fork PR with closes failed: %s", mergeRes.Error.Message)
		}
	})

	t.Run("remoteHeadViaLocalBare", func(t *testing.T) {
		dir := initTestRepo(t)
		git.ExecGit(dir, []string{"checkout", "-b", "feature-remote"})
		git.CreateCommit(dir, git.CommitOptions{Message: "Remote feature", AllowEmpty: true})
		git.ExecGit(dir, []string{"checkout", "main"})

		bareDir := t.TempDir()
		git.ExecGit(bareDir, []string{"init", "--bare"})
		git.ExecGit(dir, []string{"push", bareDir, "feature-remote"})

		res := CreatePR(dir, "Remote head via bare", "", CreatePROptions{
			Base: "#branch:main",
			Head: bareDir + "#branch:feature-remote",
		})
		if !res.Success {
			t.Fatalf("CreatePR() failed: %s", res.Error.Message)
		}
		mergeRes := MergePR(dir, res.Data.ID, MergeStrategyFF)
		if !mergeRes.Success {
			t.Fatalf("MergePR() with local bare remote failed: %s", mergeRes.Error.Message)
		}
		if mergeRes.Data.State != PRStateMerged {
			t.Errorf("State = %q, want merged", mergeRes.Data.State)
		}
	})

	t.Run("forkPR_commitFailed", func(t *testing.T) {
		dir := initTestRepo(t)
		repoURL := "https://github.com/test/repo"
		forkURL := "https://github.com/fork/cfail"

		forkHash := "deadbeef0125"
		insertReviewTestCommit(t, forkURL, forkHash)
		InsertReviewItem(ReviewItem{
			RepoURL: forkURL, Hash: forkHash, Branch: reviewTestBranch,
			Type: "pull-request", State: cache.ToNullString("open"),
			Base: cache.ToNullString("#branch:main"),
			Head: cache.ToNullString(repoURL + "#branch:feature5"),
		})

		os.RemoveAll(filepath.Join(dir, ".git", "objects"))

		forkRef := forkURL + "#commit:" + forkHash + "@" + reviewTestBranch
		mergeRes := MergePR(dir, forkRef, MergeStrategyFF)
		if mergeRes.Success {
			t.Error("should fail when fork copy commit fails")
		}
		if mergeRes.Error.Code != "COMMIT_FAILED" {
			t.Errorf("Error.Code = %q, want COMMIT_FAILED", mergeRes.Error.Code)
		}
	})
}

func TestReviewConfig(t *testing.T) {
	t.Parallel()

	t.Run("GetReviewConfig_empty", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		config := GetReviewConfig(dir)
		if config.Version != "" {
			t.Errorf("Version = %q, want empty", config.Version)
		}
		if config.Branch != "gitmsg/review" {
			t.Errorf("Branch = %q, want gitmsg/review", config.Branch)
		}
	})

	t.Run("GetForks_empty", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		forks := GetForks(dir)
		if len(forks) != 0 {
			t.Errorf("len(Forks) = %d, want 0", len(forks))
		}
	})

	t.Run("SaveAndGetReviewConfig", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		err := SaveReviewConfig(dir, ReviewConfig{
			Version:       "0.1.0",
			Branch:        "gitmsg/review",
			RequireReview: true,
		})
		if err != nil {
			t.Fatalf("SaveReviewConfig() error = %v", err)
		}
		config := GetReviewConfig(dir)
		if config.Version != "0.1.0" {
			t.Errorf("Version = %q, want 0.1.0", config.Version)
		}
		if config.Branch != "gitmsg/review" {
			t.Errorf("Branch = %q, want gitmsg/review", config.Branch)
		}
		if !config.RequireReview {
			t.Error("RequireReview should be true")
		}
	})

	t.Run("SaveReviewConfig_defaultVersion", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		err := SaveReviewConfig(dir, ReviewConfig{})
		if err != nil {
			t.Fatalf("SaveReviewConfig() error = %v", err)
		}
	})

	t.Run("SaveReviewConfig_update", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		SaveReviewConfig(dir, ReviewConfig{Version: "0.1.0", Branch: "gitmsg/review"})
		SaveReviewConfig(dir, ReviewConfig{Version: "0.2.0", Branch: "gitmsg/review"})
		config := GetReviewConfig(dir)
		if config.Version != "0.2.0" {
			t.Errorf("Version = %q, want 0.2.0", config.Version)
		}
	})

	t.Run("GetForks", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		AddFork(dir, "https://github.com/fork1/repo")
		AddFork(dir, "https://github.com/fork2/repo")
		forks := GetForks(dir)
		if len(forks) != 2 {
			t.Errorf("len(Forks) = %d, want 2", len(forks))
		}
	})

	t.Run("AddFork", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		SaveReviewConfig(dir, ReviewConfig{Version: "0.1.0"})
		if err := AddFork(dir, "https://github.com/fork/repo"); err != nil {
			t.Fatalf("AddFork() error = %v", err)
		}
		forks := GetForks(dir)
		if len(forks) != 1 {
			t.Errorf("len(Forks) = %d, want 1", len(forks))
		}
	})

	t.Run("AddFork_duplicate", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		SaveReviewConfig(dir, ReviewConfig{Version: "0.1.0"})
		AddFork(dir, "https://github.com/fork/repo")
		AddFork(dir, "https://github.com/fork/repo")
		forks := GetForks(dir)
		if len(forks) != 1 {
			t.Errorf("len(Forks) = %d after duplicate add, want 1", len(forks))
		}
	})

	t.Run("RemoveFork", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		SaveReviewConfig(dir, ReviewConfig{Version: "0.1.0"})
		AddFork(dir, "https://github.com/fork1/repo")
		AddFork(dir, "https://github.com/fork2/repo")
		if err := RemoveFork(dir, "https://github.com/fork1/repo"); err != nil {
			t.Fatalf("RemoveFork() error = %v", err)
		}
		forks := GetForks(dir)
		if len(forks) != 1 {
			t.Errorf("len(Forks) = %d, want 1", len(forks))
		}
	})

	t.Run("SaveReviewConfig_gitError", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		os.RemoveAll(filepath.Join(dir, ".git", "objects"))
		err := SaveReviewConfig(dir, ReviewConfig{Version: "0.1.0"})
		if err == nil {
			t.Error("should fail with corrupted git repo")
		}
	})
}
