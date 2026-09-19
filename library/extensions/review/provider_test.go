// provider_test.go - Tests for review notification provider
package review

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
)

func TestGetForkPRNotifications_noForks(t *testing.T) {
	setupTestDB(t)
	notifs, err := getForkPRNotifications(reviewTestRepoURL, "me@test.com", nil, false)
	if err != nil {
		t.Fatalf("getForkPRNotifications() error = %v", err)
	}
	if len(notifs) != 0 {
		t.Errorf("expected 0 notifications, got %d", len(notifs))
	}
}

func TestGetForkPRNotifications_withForks(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/provider"
	forkURL := "https://github.com/fork/provider"

	hash := "fpn_012345678"
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: forkURL, Branch: reviewTestBranch,
		AuthorName: "Forker", AuthorEmail: "forker@test.com", Message: "Fork PR",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertReviewItem(ReviewItem{
		RepoURL: forkURL, Hash: hash, Branch: reviewTestBranch, Type: "pull-request",
		State: cache.ToNullString("open"), Base: cache.ToNullString("#branch:main"),
	})

	notifs, err := getForkPRNotifications(wsURL, "me@test.com", []string{forkURL}, false)
	if err != nil {
		t.Fatalf("getForkPRNotifications() error = %v", err)
	}
	if len(notifs) != 1 {
		t.Errorf("expected 1 notification, got %d", len(notifs))
	}
	if len(notifs) > 0 && notifs[0].Type != "fork-pr" {
		t.Errorf("Type = %q, want fork-pr", notifs[0].Type)
	}
}

func TestGetForkPRNotifications_excludeOwnPRs(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/excl"
	forkURL := "https://github.com/fork/excl"

	hash := "fpn_112345678"
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: forkURL, Branch: reviewTestBranch,
		AuthorName: "Me", AuthorEmail: "me@test.com", Message: "My PR",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertReviewItem(ReviewItem{
		RepoURL: forkURL, Hash: hash, Branch: reviewTestBranch, Type: "pull-request",
		State: cache.ToNullString("open"), Base: cache.ToNullString("#branch:main"),
	})

	notifs, err := getForkPRNotifications(wsURL, "me@test.com", []string{forkURL}, false)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(notifs) != 0 {
		t.Errorf("expected 0 (own PRs excluded), got %d", len(notifs))
	}
}

func TestGetForkPRNotifications_unreadOnly(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/unread"
	forkURL := "https://github.com/fork/unread"

	hash := "fpn_212345678"
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: forkURL, Branch: reviewTestBranch,
		AuthorName: "Forker", AuthorEmail: "forker@test.com", Message: "PR",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertReviewItem(ReviewItem{
		RepoURL: forkURL, Hash: hash, Branch: reviewTestBranch, Type: "pull-request",
		State: cache.ToNullString("open"), Base: cache.ToNullString("#branch:main"),
	})

	if err := notifications.MarkAsRead(forkURL, hash, reviewTestBranch); err != nil {
		t.Fatalf("MarkAsRead() error = %v", err)
	}

	notifs, err := getForkPRNotifications(wsURL, "me@test.com", []string{forkURL}, true)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(notifs) != 0 {
		t.Errorf("expected 0 unread notifications, got %d", len(notifs))
	}
}

func TestGetFeedbackNotifications(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/fbnotif"

	prHash := "fbn_012345678"
	fbHash := "fbn_112345678"
	insertReviewTestCommit(t, wsURL, prHash)
	InsertReviewItem(ReviewItem{RepoURL: wsURL, Hash: prHash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	if err := cache.InsertCommits([]cache.Commit{{
		Hash: fbHash, RepoURL: wsURL, Branch: reviewTestBranch,
		AuthorName: "Reviewer", AuthorEmail: "reviewer@test.com", Message: "Feedback",
		Timestamp: time.Date(2025, 10, 22, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertReviewItem(ReviewItem{
		RepoURL: wsURL, Hash: fbHash, Branch: reviewTestBranch, Type: "feedback",
		PullRequestRepoURL: cache.ToNullString(wsURL), PullRequestHash: cache.ToNullString(prHash), PullRequestBranch: cache.ToNullString(reviewTestBranch),
		ReviewStateField: cache.ToNullString("approved"),
	})

	notifs, err := getFeedbackNotifications(wsURL, "me@test.com", false)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(notifs) != 1 {
		t.Errorf("expected 1 notification, got %d", len(notifs))
	}
	if len(notifs) > 0 && notifs[0].Type != "approved" {
		t.Errorf("Type = %q, want approved", notifs[0].Type)
	}
}

func TestGetFeedbackNotifications_changesRequested(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/fbcr"
	prHash := "fbc_012345678"
	fbHash := "fbc_112345678"
	insertReviewTestCommit(t, wsURL, prHash)
	InsertReviewItem(ReviewItem{RepoURL: wsURL, Hash: prHash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	if err := cache.InsertCommits([]cache.Commit{{
		Hash: fbHash, RepoURL: wsURL, Branch: reviewTestBranch,
		AuthorName: "Reviewer", AuthorEmail: "reviewer@test.com", Message: "Changes",
		Timestamp: time.Date(2025, 10, 22, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertReviewItem(ReviewItem{
		RepoURL: wsURL, Hash: fbHash, Branch: reviewTestBranch, Type: "feedback",
		PullRequestRepoURL: cache.ToNullString(wsURL), PullRequestHash: cache.ToNullString(prHash), PullRequestBranch: cache.ToNullString(reviewTestBranch),
		ReviewStateField: cache.ToNullString("changes-requested"),
	})

	notifs, err := getFeedbackNotifications(wsURL, "me@test.com", false)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(notifs) > 0 && notifs[0].Type != "changes-requested" {
		t.Errorf("Type = %q, want changes-requested", notifs[0].Type)
	}
}

func TestGetFeedbackNotifications_plainFeedback(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/fbplain"
	prHash := "fbp_012345678"
	fbHash := "fbp_112345678"
	insertReviewTestCommit(t, wsURL, prHash)
	InsertReviewItem(ReviewItem{RepoURL: wsURL, Hash: prHash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	if err := cache.InsertCommits([]cache.Commit{{
		Hash: fbHash, RepoURL: wsURL, Branch: reviewTestBranch,
		AuthorName: "Reviewer", AuthorEmail: "reviewer@test.com", Message: "Comment",
		Timestamp: time.Date(2025, 10, 22, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertReviewItem(ReviewItem{
		RepoURL: wsURL, Hash: fbHash, Branch: reviewTestBranch, Type: "feedback",
		PullRequestRepoURL: cache.ToNullString(wsURL), PullRequestHash: cache.ToNullString(prHash), PullRequestBranch: cache.ToNullString(reviewTestBranch),
	})

	notifs, err := getFeedbackNotifications(wsURL, "me@test.com", false)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(notifs) > 0 && notifs[0].Type != "feedback" {
		t.Errorf("Type = %q, want feedback", notifs[0].Type)
	}
}

func TestCountUnreadFeedback(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/cntfb"
	prHash := "cuf_012345678"
	fbHash := "cuf_112345678"
	insertReviewTestCommit(t, wsURL, prHash)
	InsertReviewItem(ReviewItem{RepoURL: wsURL, Hash: prHash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	if err := cache.InsertCommits([]cache.Commit{{
		Hash: fbHash, RepoURL: wsURL, Branch: reviewTestBranch,
		AuthorName: "Reviewer", AuthorEmail: "reviewer@test.com", Message: "Feedback",
		Timestamp: time.Date(2025, 10, 22, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertReviewItem(ReviewItem{
		RepoURL: wsURL, Hash: fbHash, Branch: reviewTestBranch, Type: "feedback",
		PullRequestRepoURL: cache.ToNullString(wsURL), PullRequestHash: cache.ToNullString(prHash), PullRequestBranch: cache.ToNullString(reviewTestBranch),
	})

	count, err := countUnreadFeedback(wsURL, "me@test.com")
	if err != nil {
		t.Fatalf("countUnreadFeedback() error = %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestCountUnreadFeedback_empty(t *testing.T) {
	setupTestDB(t)
	count, err := countUnreadFeedback(reviewTestRepoURL, "me@test.com")
	if err != nil {
		t.Fatalf("countUnreadFeedback() error = %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestCountUnreadForkPRs_noForks(t *testing.T) {
	setupTestDB(t)
	count, err := countUnreadForkPRs(reviewTestRepoURL, "me@test.com", nil)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestGetNotifications_emptyWorkspace(t *testing.T) {
	setupTestDB(t)
	dir := t.TempDir()
	p := &reviewNotificationProvider{}
	notifs, err := p.GetNotifications(dir, notifications.Filter{})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(notifs) != 0 {
		t.Errorf("expected 0 notifications for empty workspace, got %d", len(notifs))
	}
}

func TestProviderNotifications(t *testing.T) {
	t.Parallel()

	t.Run("GetNotifications_withLimit", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		git.ExecGit(dir, []string{"remote", "set-url", "origin", "https://github.com/test/provider-limit-empty.git"})
		p := &reviewNotificationProvider{}
		_, err := p.GetNotifications(dir, notifications.Filter{Limit: 5})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("GetUnreadCount", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		git.ExecGit(dir, []string{"remote", "set-url", "origin", "https://github.com/test/provider-unread-empty.git"})
		git.ExecGit(dir, []string{"config", "user.email", "provider-unread-empty@test.com"})
		p := &reviewNotificationProvider{}
		count, err := p.GetUnreadCount(dir)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if count != 0 {
			t.Errorf("count = %d, want 0 (no data)", count)
		}
	})

	t.Run("GetNotifications_limitTruncation", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		wsURL := "https://github.com/test/repo"
		prHash := "d0a012345678"
		insertReviewTestCommit(t, wsURL, prHash)
		InsertReviewItem(ReviewItem{RepoURL: wsURL, Hash: prHash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

		for i, hash := range []string{"d0b012345678", "d0c012345678", "d0d012345678"} {
			if err := cache.InsertCommits([]cache.Commit{{
				Hash: hash, RepoURL: wsURL, Branch: reviewTestBranch,
				AuthorName: "Reviewer", AuthorEmail: "reviewer@test.com", Message: "Feedback",
				Timestamp: time.Date(2025, 10, 22, 12+i, 0, 0, 0, time.UTC),
			}}); err != nil {
				t.Fatal(err)
			}
			InsertReviewItem(ReviewItem{
				RepoURL: wsURL, Hash: hash, Branch: reviewTestBranch, Type: "feedback",
				PullRequestRepoURL: cache.ToNullString(wsURL), PullRequestHash: cache.ToNullString(prHash), PullRequestBranch: cache.ToNullString(reviewTestBranch),
			})
		}

		p := &reviewNotificationProvider{}
		notifs, err := p.GetNotifications(dir, notifications.Filter{Limit: 1})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(notifs) != 1 {
			t.Errorf("expected 1 notification with limit=1, got %d", len(notifs))
		}
	})

	t.Run("GetNotifications_withData", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		wsURL := "https://github.com/test/repo"
		prHash := "e0a012345678"
		fbHash := "e0b012345678"
		insertReviewTestCommit(t, wsURL, prHash)
		InsertReviewItem(ReviewItem{RepoURL: wsURL, Hash: prHash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

		if err := cache.InsertCommits([]cache.Commit{{
			Hash: fbHash, RepoURL: wsURL, Branch: reviewTestBranch,
			AuthorName: "Reviewer", AuthorEmail: "reviewer@test.com", Message: "Feedback",
			Timestamp: time.Date(2025, 10, 22, 12, 0, 0, 0, time.UTC),
		}}); err != nil {
			t.Fatal(err)
		}
		InsertReviewItem(ReviewItem{
			RepoURL: wsURL, Hash: fbHash, Branch: reviewTestBranch, Type: "feedback",
			PullRequestRepoURL: cache.ToNullString(wsURL), PullRequestHash: cache.ToNullString(prHash), PullRequestBranch: cache.ToNullString(reviewTestBranch),
		})

		p := &reviewNotificationProvider{}
		notifs, err := p.GetNotifications(dir, notifications.Filter{})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(notifs) < 1 {
			t.Errorf("expected at least 1 notification, got %d", len(notifs))
		}
	})

	t.Run("GetUnreadCount_withData", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		wsURL := "https://github.com/test/repo"
		prHash := "e1a012345678"
		fbHash := "e1b012345678"
		insertReviewTestCommit(t, wsURL, prHash)
		InsertReviewItem(ReviewItem{RepoURL: wsURL, Hash: prHash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

		if err := cache.InsertCommits([]cache.Commit{{
			Hash: fbHash, RepoURL: wsURL, Branch: reviewTestBranch,
			AuthorName: "Reviewer", AuthorEmail: "reviewer@test.com", Message: "Feedback",
			Timestamp: time.Date(2025, 10, 22, 12, 0, 0, 0, time.UTC),
		}}); err != nil {
			t.Fatal(err)
		}
		InsertReviewItem(ReviewItem{
			RepoURL: wsURL, Hash: fbHash, Branch: reviewTestBranch, Type: "feedback",
			PullRequestRepoURL: cache.ToNullString(wsURL), PullRequestHash: cache.ToNullString(prHash), PullRequestBranch: cache.ToNullString(reviewTestBranch),
		})

		p := &reviewNotificationProvider{}
		count, err := p.GetUnreadCount(dir)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if count < 1 {
			t.Errorf("expected at least 1 unread, got %d", count)
		}
	})
}

// countPRReady returns the pr-ready notifications the upstream sees.
func countPRReady(t *testing.T, upstream string) int {
	t.Helper()
	p := &reviewNotificationProvider{}
	notifs, err := p.GetNotifications(upstream, notifications.Filter{})
	if err != nil {
		t.Fatalf("GetNotifications: %v", err)
	}
	count := 0
	for _, n := range notifs {
		if n.Type == "pr-ready" {
			count++
		}
	}
	return count
}

// TestDraftReady_oncePerTransition drives a fork draft PR through the write path.
func TestDraftReady_oncePerTransition(t *testing.T) {
	setupTestDB(t)
	alice, bob, _, forkURL := forkPRFixture(t)
	if err := gitmsg.AddFork(alice, forkURL); err != nil {
		t.Fatalf("AddFork: %v", err)
	}

	created := CreatePR(bob, "Fix the bug", "", CreatePROptions{
		Base:  "#branch:main",
		Head:  "feature",
		Draft: true,
	})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}
	if got := countPRReady(t, alice); got != 0 {
		t.Fatalf("a draft pull request should not be ready, got %d pr-ready", got)
	}

	if res := MarkReady(bob, created.Data.ID); !res.Success {
		t.Fatalf("MarkReady: %s", res.Error.Message)
	}
	if got := countPRReady(t, alice); got != 1 {
		t.Fatalf("after MarkReady: got %d pr-ready, want 1", got)
	}

	subject := "Fix the bug, again"
	nextSecond()
	if res := UpdatePR(bob, created.Data.ID, UpdatePROptions{Subject: &subject}); !res.Success {
		t.Fatalf("UpdatePR subject: %s", res.Error.Message)
	}
	if got := countPRReady(t, alice); got != 1 {
		t.Fatalf("after a subject edit: got %d pr-ready, want 1", got)
	}

	nextSecond()
	if res := ConvertToDraft(bob, created.Data.ID); !res.Success {
		t.Fatalf("ConvertToDraft: %s", res.Error.Message)
	}
	nextSecond()
	if res := MarkReady(bob, created.Data.ID); !res.Success {
		t.Fatalf("second MarkReady: %s", res.Error.Message)
	}
	if got := countPRReady(t, alice); got != 2 {
		t.Fatalf("after a second transition: got %d pr-ready, want 2", got)
	}
}

func TestGetUnreadCount_emptyWorkspace(t *testing.T) {
	setupTestDB(t)
	dir := t.TempDir()
	p := &reviewNotificationProvider{}
	count, err := p.GetUnreadCount(dir)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestCountUnreadForkPRs_withForks(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/cunread"
	forkURL := "https://github.com/fork/cunread"

	hash := "f00a12345678"
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: forkURL, Branch: reviewTestBranch,
		AuthorName: "Forker", AuthorEmail: "forker@test.com", Message: "PR",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertReviewItem(ReviewItem{
		RepoURL: forkURL, Hash: hash, Branch: reviewTestBranch, Type: "pull-request",
		State: cache.ToNullString("open"), Base: cache.ToNullString("#branch:main"),
	})

	count, err := countUnreadForkPRs(wsURL, "me@test.com", []string{forkURL})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestGetForkPRNotifications_notTargetingWorkspace(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/notarget"
	forkURL := "https://github.com/fork/notarget"

	hash := "f1a012345678"
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: forkURL, Branch: reviewTestBranch,
		AuthorName: "Forker", AuthorEmail: "forker@test.com", Message: "Fork PR",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertReviewItem(ReviewItem{
		RepoURL: forkURL, Hash: hash, Branch: reviewTestBranch, Type: "pull-request",
		State: cache.ToNullString("open"),
		Base:  cache.ToNullString("https://github.com/other/repo#branch:main"),
	})

	notifs, err := getForkPRNotifications(wsURL, "me@test.com", []string{forkURL}, false)
	if err != nil {
		t.Fatalf("getForkPRNotifications() error = %v", err)
	}
	if len(notifs) != 0 {
		t.Errorf("expected 0 notifications (PR not targeting workspace), got %d", len(notifs))
	}
}

func TestGetNotifications_errorPaths(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	gitmsg.AddFork(dir, "https://github.com/fork/errpath")

	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec("DROP VIEW IF EXISTS review_items_resolved")
		return nil
	})

	p := &reviewNotificationProvider{}
	notifs, err := p.GetNotifications(dir, notifications.Filter{})
	if err != nil {
		t.Fatalf("GetNotifications should not error: %v", err)
	}
	if len(notifs) != 0 {
		t.Errorf("expected 0 notifications when views are broken, got %d", len(notifs))
	}
}

func TestGetUnreadCount_errorPaths(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	gitmsg.AddFork(dir, "https://github.com/fork/err")

	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec("DROP VIEW IF EXISTS review_items_resolved")
		return nil
	})

	p := &reviewNotificationProvider{}
	count, err := p.GetUnreadCount(dir)
	if err != nil {
		t.Fatalf("GetUnreadCount should not error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 when views are broken, got %d", count)
	}
}

func TestCountUnreadForkPRs_error(t *testing.T) {
	setupTestDB(t)

	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec("DROP VIEW IF EXISTS review_items_resolved")
		return nil
	})

	_, err := countUnreadForkPRs("ws", "me@test.com", []string{"fork"})
	if err == nil {
		t.Error("should fail when view is dropped")
	}
}

func TestGetFeedbackNotifications_unreadOnly(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/fbunread"
	prHash := "e2a012345678"
	fbHash := "e2b012345678"
	insertReviewTestCommit(t, wsURL, prHash)
	InsertReviewItem(ReviewItem{RepoURL: wsURL, Hash: prHash, Branch: reviewTestBranch, Type: "pull-request", State: cache.ToNullString("open")})

	if err := cache.InsertCommits([]cache.Commit{{
		Hash: fbHash, RepoURL: wsURL, Branch: reviewTestBranch,
		AuthorName: "Reviewer", AuthorEmail: "reviewer@test.com", Message: "Feedback",
		Timestamp: time.Date(2025, 10, 22, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertReviewItem(ReviewItem{
		RepoURL: wsURL, Hash: fbHash, Branch: reviewTestBranch, Type: "feedback",
		PullRequestRepoURL: cache.ToNullString(wsURL), PullRequestHash: cache.ToNullString(prHash), PullRequestBranch: cache.ToNullString(reviewTestBranch),
	})

	if err := notifications.MarkAsRead(wsURL, fbHash, reviewTestBranch); err != nil {
		t.Fatalf("MarkAsRead() error = %v", err)
	}

	notifs, err := getFeedbackNotifications(wsURL, "me@test.com", true)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(notifs) != 0 {
		t.Errorf("expected 0 unread feedback, got %d", len(notifs))
	}
}
