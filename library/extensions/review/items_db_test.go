// items_db_test.go - Tests for review item database operations
package review

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
)

func TestInsertReviewItem(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	hash := "ins123456789"
	insertReviewTestCommit(t, repoURL, hash)

	err := InsertReviewItem(ReviewItem{
		RepoURL: repoURL,
		Hash:    hash,
		Branch:  reviewTestBranch,
		Type:    "pull-request",
		State:   cache.ToNullString("open"),
	})
	if err != nil {
		t.Fatalf("InsertReviewItem() error = %v", err)
	}
}

func TestInsertReviewItem_upsert(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	hash := "ups123456789"
	insertReviewTestCommit(t, repoURL, hash)

	item := ReviewItem{
		RepoURL: repoURL,
		Hash:    hash,
		Branch:  reviewTestBranch,
		Type:    "pull-request",
		State:   cache.ToNullString("open"),
	}
	if err := InsertReviewItem(item); err != nil {
		t.Fatalf("first InsertReviewItem() error = %v", err)
	}
	item.State = cache.ToNullString("merged")
	if err := InsertReviewItem(item); err != nil {
		t.Fatalf("second InsertReviewItem() error = %v", err)
	}
	got := queryReviewItem(t, repoURL, hash, reviewTestBranch)
	if !got.State.Valid || got.State.String != "merged" {
		t.Errorf("State = %v after upsert, want merged", got.State)
	}
}

func TestGetReviewItem_notFound(t *testing.T) {
	setupTestDB(t)
	_, err := GetReviewItem(reviewTestRepoURL, "nonexistent12", reviewTestBranch)
	if err == nil {
		t.Error("GetReviewItem() should return error for non-existent item")
	}
}

func TestGetReviewItemByRef_emptyRef(t *testing.T) {
	setupTestDB(t)
	_, err := GetReviewItemByRef("", reviewTestRepoURL)
	if err == nil {
		t.Error("GetReviewItemByRef with empty ref should return error")
	}
}

func TestGetReviewItemByRef_noRepoInRef(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	hash := "aabb11cc2233"
	insertReviewTestCommit(t, repoURL, hash)
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: hash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	item, err := GetReviewItemByRef("#commit:"+hash+"@gitmsg/review", repoURL)
	if err != nil {
		t.Fatalf("GetReviewItemByRef() error = %v", err)
	}
	if item.Hash != hash {
		t.Errorf("Hash = %q, want %q", item.Hash, hash)
	}
}

func TestGetReviewItemByRef(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	hash := "aabb11223344"
	insertReviewTestCommit(t, repoURL, hash)
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: hash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	refStr := repoURL + "#commit:" + hash + "@gitmsg/review"
	item, err := GetReviewItemByRef(refStr, repoURL)
	if err != nil {
		t.Fatalf("GetReviewItemByRef() error = %v", err)
	}
	if item == nil {
		t.Fatal("GetReviewItemByRef() returned nil")
	}
	if item.Hash != hash {
		t.Errorf("Hash = %q, want %q", item.Hash, hash)
	}
}

func TestGetReviewItemByRef_noBranch(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	hash := "a0be11234560"
	insertReviewTestCommit(t, repoURL, hash)
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: hash, Branch: reviewTestBranch, Type: "pull-request"})

	refStr := repoURL + "#commit:" + hash
	item, err := GetReviewItemByRef(refStr, repoURL)
	if err != nil {
		t.Fatalf("GetReviewItemByRef() error = %v", err)
	}
	if item.Hash != hash {
		t.Errorf("Hash = %q, want %q", item.Hash, hash)
	}
}

func TestGetReviewItems_filterByType(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	for i, typ := range []string{"pull-request", "pull-request", "feedback"} {
		hash := []string{"tp1_12345678", "tp2_12345678", "tp3_12345678"}[i]
		insertReviewTestCommit(t, repoURL, hash)
		InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: hash, Branch: reviewTestBranch, Type: typ, State: cache.ToNullString("open")})
	}

	items, err := GetReviewItems(ReviewQuery{Types: []string{"pull-request"}, RepoURL: repoURL, Branch: reviewTestBranch})
	if err != nil {
		t.Fatalf("GetReviewItems() error = %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 pull-requests, got %d", len(items))
	}
}

func TestGetReviewItems_filterByState(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	hashes := []string{"st1_12345678", "st2_12345678"}
	states := []string{"open", "merged"}
	for i := range hashes {
		insertReviewTestCommit(t, repoURL, hashes[i])
		InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: hashes[i], Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString(states[i])})
	}

	items, err := GetReviewItems(ReviewQuery{States: []string{"open"}, RepoURL: repoURL, Branch: reviewTestBranch})
	if err != nil {
		t.Fatalf("GetReviewItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 open item, got %d", len(items))
	}
}

func TestGetReviewItems_filterByRepo(t *testing.T) {
	setupTestDB(t)
	repo1 := "https://github.com/user/repo1"
	repo2 := "https://github.com/user/repo2"
	insertReviewTestCommit(t, repo1, "rp1_12345678")
	insertReviewTestCommit(t, repo2, "rp2_12345678")
	InsertReviewItem(ReviewItem{RepoURL: repo1, Hash: "rp1_12345678", Branch: reviewTestBranch, Type: "pull-request"})
	InsertReviewItem(ReviewItem{RepoURL: repo2, Hash: "rp2_12345678", Branch: reviewTestBranch, Type: "pull-request"})

	items, err := GetReviewItems(ReviewQuery{RepoURL: repo1, Branch: reviewTestBranch})
	if err != nil {
		t.Fatalf("GetReviewItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item for repo1, got %d", len(items))
	}
}

func TestGetReviewItems_filterByReviewer(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	insertReviewTestCommit(t, repoURL, "rv1_12345678")
	insertReviewTestCommit(t, repoURL, "rv2_12345678")
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: "rv1_12345678", Branch: reviewTestBranch, Type: "pull-request", Reviewers: cache.ToNullString("alice@test.com")})
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: "rv2_12345678", Branch: reviewTestBranch, Type: "pull-request", Reviewers: cache.ToNullString("bob@test.com")})

	items, err := GetReviewItems(ReviewQuery{RepoURL: repoURL, Branch: reviewTestBranch, Reviewer: "alice@test.com"})
	if err != nil {
		t.Fatalf("GetReviewItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item for alice, got %d", len(items))
	}
}

func TestGetReviewItems_filterByPR(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	insertReviewTestCommit(t, repoURL, "fbp_12345678")
	insertReviewTestCommit(t, repoURL, "fbp_23456789")
	InsertReviewItem(ReviewItem{
		RepoURL: repoURL, Hash: "fbp_12345678", Branch: reviewTestBranch, Type: "feedback",
		PullRequestRepoURL: cache.ToNullString(repoURL), PullRequestHash: cache.ToNullString("pr123"), PullRequestBranch: cache.ToNullString(reviewTestBranch),
	})
	InsertReviewItem(ReviewItem{
		RepoURL: repoURL, Hash: "fbp_23456789", Branch: reviewTestBranch, Type: "feedback",
		PullRequestRepoURL: cache.ToNullString(repoURL), PullRequestHash: cache.ToNullString("pr456"), PullRequestBranch: cache.ToNullString(reviewTestBranch),
	})

	items, err := GetReviewItems(ReviewQuery{PRRepoURL: repoURL, PRHash: "pr123", PRBranch: reviewTestBranch})
	if err != nil {
		t.Fatalf("GetReviewItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 feedback for pr123, got %d", len(items))
	}
}

func TestGetReviewItems_filterByPR_noBranch(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	insertReviewTestCommit(t, repoURL, "fbn_12345678")
	InsertReviewItem(ReviewItem{
		RepoURL: repoURL, Hash: "fbn_12345678", Branch: reviewTestBranch, Type: "feedback",
		PullRequestRepoURL: cache.ToNullString(repoURL), PullRequestHash: cache.ToNullString("pr789"),
	})

	items, err := GetReviewItems(ReviewQuery{PRRepoURL: repoURL, PRHash: "pr789"})
	if err != nil {
		t.Fatalf("GetReviewItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 feedback, got %d", len(items))
	}
}

func TestGetReviewItems_limit(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	for i := 0; i < 5; i++ {
		hash := []string{"lm1_12345678", "lm2_12345678", "lm3_12345678", "lm4_12345678", "lm5_12345678"}[i]
		insertReviewTestCommit(t, repoURL, hash)
		InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: hash, Branch: reviewTestBranch, Type: "pull-request"})
	}

	items, err := GetReviewItems(ReviewQuery{RepoURL: repoURL, Branch: reviewTestBranch, Limit: 2})
	if err != nil {
		t.Fatalf("GetReviewItems() error = %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items with limit=2, got %d", len(items))
	}
}

func TestGetReviewItems_offset(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	for i := 0; i < 3; i++ {
		hash := []string{"of1_12345678", "of2_12345678", "of3_12345678"}[i]
		insertReviewTestCommit(t, repoURL, hash)
		InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: hash, Branch: reviewTestBranch, Type: "pull-request"})
	}

	items, err := GetReviewItems(ReviewQuery{RepoURL: repoURL, Branch: reviewTestBranch, Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("GetReviewItems() error = %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items with offset=1 limit=2, got %d", len(items))
	}
}

func TestGetReviewItems_sortAsc(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	hashes := []string{"sa1_12345678", "sa2_12345678"}
	for i, hash := range hashes {
		if err := cache.InsertCommits([]cache.Commit{{
			Hash: hash, RepoURL: repoURL, Branch: reviewTestBranch,
			AuthorName: "Test", AuthorEmail: "test@test.com", Message: "test",
			Timestamp: time.Date(2025, 10, 21+i, 12, 0, 0, 0, time.UTC),
		}}); err != nil {
			t.Fatal(err)
		}
		InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: hash, Branch: reviewTestBranch, Type: "pull-request"})
	}

	items, err := GetReviewItems(ReviewQuery{RepoURL: repoURL, Branch: reviewTestBranch, SortOrder: "asc"})
	if err != nil {
		t.Fatalf("GetReviewItems() error = %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("expected at least 2 items, got %d", len(items))
	}
	if items[0].Timestamp.After(items[1].Timestamp) {
		t.Error("first item should be older when sorted asc")
	}
}

func TestGetPullRequests_result(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	hash := "prr_12345678"
	insertReviewTestCommit(t, repoURL, hash)
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: hash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	result := GetPullRequests(repoURL, reviewTestBranch, nil, "", 10)
	if !result.Success {
		t.Fatalf("GetPullRequests() failed: %s", result.Error.Message)
	}
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(result.Data))
	}
}

func TestGetPullRequests_withStates(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	hashes := []string{"prs1_1234567", "prs2_1234567"}
	states := []string{"open", "merged"}
	for i := range hashes {
		insertReviewTestCommit(t, repoURL, hashes[i])
		InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: hashes[i], Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString(states[i])})
	}

	result := GetPullRequests(repoURL, reviewTestBranch, []string{"open"}, "", 10)
	if !result.Success {
		t.Fatalf("GetPullRequests() failed: %s", result.Error.Message)
	}
	if len(result.Data) != 1 {
		t.Errorf("expected 1 open PR, got %d", len(result.Data))
	}
}

func TestGetPullRequests_queryError(t *testing.T) {
	cache.Reset()
	t.Cleanup(func() { cache.Reset() })
	res := GetPullRequests("", "", nil, "", 10)
	if res.Success {
		t.Error("should fail when cache is not initialized")
	}
}

func TestGetPullRequestsWithForks_noForks(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	hash := "nf0_12345678"
	insertReviewTestCommit(t, repoURL, hash)
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: hash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	result := GetPullRequestsWithForks(repoURL, reviewTestBranch, nil, nil, "", 10)
	if !result.Success {
		t.Fatalf("GetPullRequestsWithForks() failed: %s", result.Error.Message)
	}
	if len(result.Data) != 1 {
		t.Errorf("expected 1 PR, got %d", len(result.Data))
	}
}

func TestGetPullRequestsWithForks_withFiltering(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/upstream/repo"
	forkURL := "https://github.com/fork/repo"

	// Workspace PR
	insertReviewTestCommit(t, wsURL, "wf1_12345678")
	InsertReviewItem(ReviewItem{
		RepoURL: wsURL, Hash: "wf1_12345678", Branch: reviewTestBranch, Type: "pull-request",
		State: cache.ToNullString("open"), Base: cache.ToNullString("#branch:main"),
	})

	// Fork PR targeting workspace (local base ref = targets upstream)
	insertReviewTestCommit(t, forkURL, "wf2_12345678")
	InsertReviewItem(ReviewItem{
		RepoURL: forkURL, Hash: "wf2_12345678", Branch: reviewTestBranch, Type: "pull-request",
		State: cache.ToNullString("open"), Base: cache.ToNullString("#branch:main"), Head: cache.ToNullString("#branch:feature"),
	})

	// Fork PR NOT targeting workspace (explicit base ref to different repo)
	insertReviewTestCommit(t, forkURL, "wf3_12345678")
	InsertReviewItem(ReviewItem{
		RepoURL: forkURL, Hash: "wf3_12345678", Branch: reviewTestBranch, Type: "pull-request",
		State: cache.ToNullString("open"), Base: cache.ToNullString("https://github.com/other/repo#branch:main"),
	})

	result := GetPullRequestsWithForks(wsURL, reviewTestBranch, []string{forkURL}, nil, "", 10)
	if !result.Success {
		t.Fatalf("GetPullRequestsWithForks() failed: %s", result.Error.Message)
	}
	// Should include workspace PR + fork PR targeting workspace, but not the third one
	if len(result.Data) != 2 {
		t.Errorf("expected 2 PRs (ws + fork targeting ws), got %d", len(result.Data))
	}
}

func TestGetPullRequestsWithForks_withStates(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/upstream/stfork"
	forkURL := "https://github.com/fork/stfork"
	insertReviewTestCommit(t, wsURL, "sf1_12345678")
	InsertReviewItem(ReviewItem{RepoURL: wsURL, Hash: "sf1_12345678", Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})
	insertReviewTestCommit(t, forkURL, "sf2_12345678")
	InsertReviewItem(ReviewItem{RepoURL: forkURL, Hash: "sf2_12345678", Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("merged"), Base: cache.ToNullString("#branch:main")})

	result := GetPullRequestsWithForks(wsURL, reviewTestBranch, []string{forkURL}, []string{"open"}, "", 10)
	if !result.Success {
		t.Fatalf("failed: %s", result.Error.Message)
	}
	if len(result.Data) != 1 {
		t.Errorf("expected 1 open PR, got %d", len(result.Data))
	}
}

func TestGetPullRequestsWithForks_queryError(t *testing.T) {
	cache.Reset()
	t.Cleanup(func() { cache.Reset() })
	res := GetPullRequestsWithForks("ws", "", []string{"fork"}, nil, "", 10)
	if res.Success {
		t.Error("should fail when cache is not initialized")
	}
}

func TestGetFeedbackForPR_result(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	prHash := "fbpr_1234567"
	fbHash := "fbfb_1234567"
	insertReviewTestCommit(t, repoURL, prHash)
	insertReviewTestCommit(t, repoURL, fbHash)
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: prHash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})
	InsertReviewItem(ReviewItem{
		RepoURL: repoURL, Hash: fbHash, Branch: reviewTestBranch, Type: "feedback",
		PullRequestRepoURL: cache.ToNullString(repoURL), PullRequestHash: cache.ToNullString(prHash), PullRequestBranch: cache.ToNullString(reviewTestBranch),
		ReviewStateField: cache.ToNullString("approved"),
	})

	result := GetFeedbackForPR(repoURL, prHash, reviewTestBranch)
	if !result.Success {
		t.Fatalf("GetFeedbackForPR() failed: %s", result.Error.Message)
	}
	if len(result.Data) != 1 {
		t.Errorf("expected 1 feedback, got %d", len(result.Data))
	}
}

func TestGetStateChangeInfo(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	canonHash := "sc0123456789"
	editHash := "sc0234567890"

	// Insert canonical PR
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: canonHash, RepoURL: repoURL, Branch: reviewTestBranch,
		AuthorName: "Author", AuthorEmail: "author@test.com", Message: "Original PR",
		Timestamp: time.Date(2025, 10, 20, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: canonHash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	// Insert edit that changes state to merged (with merge-base/merge-head)
	mergeContent := "Merged PR\n\n" + `GitMsg: ext="review"; type="pull-request"; state="merged"; merge-base="aaa111"; merge-head="bbb222"; edits="#commit:` + canonHash + `@gitmsg/review"; v="0.1.0"`
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: editHash, RepoURL: repoURL, Branch: reviewTestBranch,
		AuthorName: "Merger", AuthorEmail: "merger@test.com", Message: mergeContent,
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: editHash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("merged")})
	cache.InsertVersion(repoURL, editHash, reviewTestBranch, repoURL, canonHash, reviewTestBranch, false)

	info, err := GetStateChangeInfo(repoURL, canonHash, reviewTestBranch, PRStateMerged)
	if err != nil {
		t.Fatalf("GetStateChangeInfo() error = %v", err)
	}
	if info.AuthorName != "Merger" {
		t.Errorf("AuthorName = %q, want Merger", info.AuthorName)
	}
	if info.MergeBase != "aaa111" {
		t.Errorf("MergeBase = %q, want aaa111", info.MergeBase)
	}
	if info.MergeHead != "bbb222" {
		t.Errorf("MergeHead = %q, want bbb222", info.MergeHead)
	}
}

func TestGetStateChangeInfo_notFound(t *testing.T) {
	setupTestDB(t)
	_, err := GetStateChangeInfo(reviewTestRepoURL, "nonexistent12", reviewTestBranch, PRStateMerged)
	if err == nil {
		t.Error("GetStateChangeInfo() should return error when not found")
	}
}

func TestForkPRTargetsWorkspace(t *testing.T) {
	tests := []struct {
		name         string
		base         string
		workspaceURL string
		want         bool
	}{
		{"empty base", "", "https://github.com/ws/repo", false},
		{"local ref targets upstream", "#branch:main", "https://github.com/ws/repo", true},
		{"matches workspace", "https://github.com/ws/repo#branch:main", "https://github.com/ws/repo", true},
		{"different repo", "https://github.com/other/repo#branch:main", "https://github.com/ws/repo", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := ReviewItem{Base: cache.ToNullString(tt.base)}
			if tt.base == "" {
				item.Base = sql.NullString{}
			}
			got := forkPRTargetsWorkspace(item, tt.workspaceURL)
			if got != tt.want {
				t.Errorf("forkPRTargetsWorkspace() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetReviewItems_sortByTimestamp(t *testing.T) {
	setupTestDB(t)
	repoURL := reviewTestRepoURL
	hashes := []string{"st1a12345678", "st2a12345678"}
	for i, hash := range hashes {
		if err := cache.InsertCommits([]cache.Commit{{
			Hash: hash, RepoURL: repoURL, Branch: reviewTestBranch,
			AuthorName: "Test", AuthorEmail: "test@test.com", Message: "test",
			Timestamp: time.Date(2025, 10, 21+i, 12, 0, 0, 0, time.UTC),
		}}); err != nil {
			t.Fatal(err)
		}
		InsertReviewItem(ReviewItem{RepoURL: repoURL, Hash: hash, Branch: reviewTestBranch, Type: "pull-request"})
	}

	items, err := GetReviewItems(ReviewQuery{RepoURL: repoURL, Branch: reviewTestBranch, SortField: "timestamp", SortOrder: "desc"})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("expected at least 2 items, got %d", len(items))
	}
	if items[0].Timestamp.Before(items[1].Timestamp) {
		t.Error("first item should be newer when sorted desc")
	}
}

func TestGetPullRequestsWithForks_noLimit(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/upstream/nolimit"
	insertReviewTestCommit(t, wsURL, "nl1_12345678")
	InsertReviewItem(ReviewItem{RepoURL: wsURL, Hash: "nl1_12345678", Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	result := GetPullRequestsWithForks(wsURL, reviewTestBranch, []string{"https://github.com/fork/nolimit"}, nil, "", 0)
	if !result.Success {
		t.Fatalf("failed: %s", result.Error.Message)
	}
}

func TestGetFeedbackForPR_queryError(t *testing.T) {
	cache.Reset()
	t.Cleanup(func() { cache.Reset() })
	res := GetFeedbackForPR("", "", "")
	if res.Success {
		t.Error("should fail when cache is not initialized")
	}
}

func TestGetReviewItems_viewError(t *testing.T) {
	setupTestDB(t)
	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec("DROP VIEW IF EXISTS review_items_resolved")
		return nil
	})
	_, err := GetReviewItems(ReviewQuery{Limit: 10})
	if err == nil {
		t.Error("should fail when view is dropped")
	}
}

func TestGetReviewItems_scanError(t *testing.T) {
	setupTestDB(t)
	// Replace the view with one that returns fewer columns, causing scan to fail
	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec("DROP VIEW IF EXISTS review_items_resolved")
		db.Exec(`CREATE VIEW review_items_resolved AS SELECT 1 AS repo_url, 2 AS hash, 3 AS branch,
			'a' AS author_name, 'b' AS author_email, 'c' AS resolved_message, 'd' AS timestamp,
			'e' AS type, 'f' AS state, 'g' AS base, 'h' AS head, 'i' AS closes, 'j' AS reviewers,
			'k' AS pull_request_repo_url, 'l' AS pull_request_hash, 'm' AS pull_request_branch,
			'n' AS commit_ref, 'o' AS file, 'p' AS old_line, 'q' AS new_line, 'r' AS old_line_end, 's' AS new_line_end,
			't' AS review_state, 'u' AS suggestion,
			'v' AS edits, 'w' AS is_virtual, 'x' AS is_retracted, 'y' AS has_edits`)
		return nil
	})
	// Query expects 'comments' column which is missing — scan should fail
	_, err := GetReviewItems(ReviewQuery{Limit: 10})
	if err == nil {
		t.Error("should fail when view has wrong columns")
	}
}

func TestGetPullRequestsWithForks_viewError(t *testing.T) {
	setupTestDB(t)
	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec("DROP VIEW IF EXISTS review_items_resolved")
		return nil
	})
	res := GetPullRequestsWithForks("ws", "", []string{"fork"}, nil, "", 10)
	if res.Success {
		t.Error("should fail when view is dropped")
	}
	if res.Error.Code != "QUERY_FAILED" {
		t.Errorf("Error.Code = %q, want QUERY_FAILED", res.Error.Code)
	}
}

func TestGetPullRequestsWithForks_scanError(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/upstream/scan"
	forkURL := "https://github.com/fork/scan"
	// Replace view with one that has wrong column count
	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec("DROP VIEW IF EXISTS review_items_resolved")
		db.Exec(`CREATE VIEW review_items_resolved AS SELECT 1 AS repo_url, 2 AS hash, 3 AS branch,
			'a' AS author_name, 'b' AS author_email, 'c' AS resolved_message, 'd' AS timestamp,
			'e' AS type, 'f' AS state, 'g' AS base, 'h' AS head, 'i' AS closes, 'j' AS reviewers,
			'k' AS pull_request_repo_url, 'l' AS pull_request_hash, 'm' AS pull_request_branch,
			'n' AS commit_ref, 'o' AS file, 'p' AS old_line, 'q' AS new_line, 'r' AS old_line_end, 's' AS new_line_end,
			't' AS review_state, 'u' AS suggestion,
			'v' AS edits, 0 AS is_virtual, 0 AS is_retracted, 0 AS has_edits, 0 AS is_edit_commit`)
		return nil
	})
	res := GetPullRequestsWithForks(wsURL, "", []string{forkURL}, nil, "", 10)
	if res.Success {
		t.Error("should fail when view has wrong columns")
	}
}
