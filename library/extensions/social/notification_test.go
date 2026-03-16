// notification_test.go - Tests for notification queries and read status management
package social

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/notifications"
)

// --- Pure function tests ---

func TestSortNotificationsByTime_empty(t *testing.T) {
	var notifications []Notification
	sortNotificationsByTime(notifications)
	// Should not panic
}

func TestSortNotificationsByTime_alreadySorted(t *testing.T) {
	now := time.Now()
	notifications := []Notification{
		{ID: "a", Timestamp: now},
		{ID: "b", Timestamp: now.Add(-1 * time.Hour)},
		{ID: "c", Timestamp: now.Add(-2 * time.Hour)},
	}
	sortNotificationsByTime(notifications)
	if notifications[0].ID != "a" {
		t.Error("first should be most recent")
	}
	if notifications[2].ID != "c" {
		t.Error("last should be oldest")
	}
}

func TestSortNotificationsByTime_unsorted(t *testing.T) {
	now := time.Now()
	notifications := []Notification{
		{ID: "c", Timestamp: now.Add(-2 * time.Hour)},
		{ID: "a", Timestamp: now},
		{ID: "b", Timestamp: now.Add(-1 * time.Hour)},
	}
	sortNotificationsByTime(notifications)
	if notifications[0].ID != "a" {
		t.Errorf("first = %q, want a", notifications[0].ID)
	}
	if notifications[1].ID != "b" {
		t.Errorf("second = %q, want b", notifications[1].ID)
	}
	if notifications[2].ID != "c" {
		t.Errorf("third = %q, want c", notifications[2].ID)
	}
}

// --- DB tests ---

func TestMarkAsRead_viaCoreNotifications(t *testing.T) {
	setupTestDB(t)
	if err := notifications.MarkAsRead("https://github.com/a/b", "abc123", "main"); err != nil {
		t.Fatalf("MarkAsRead() error = %v", err)
	}
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_notification_reads WHERE repo_url = ? AND hash = ? AND branch = ?`,
			"https://github.com/a/b", "abc123", "main").Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 read record, got %d", count)
	}
}

func TestMarkAsUnread_viaCoreNotifications(t *testing.T) {
	setupTestDB(t)
	notifications.MarkAsRead("https://github.com/a/b", "abc123", "main")
	if err := notifications.MarkAsUnread("https://github.com/a/b", "abc123", "main"); err != nil {
		t.Fatalf("MarkAsUnread() error = %v", err)
	}
	count, _ := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_notification_reads WHERE repo_url = ? AND hash = ? AND branch = ?`,
			"https://github.com/a/b", "abc123", "main").Scan(&c)
		return c, err
	})
	if count != 0 {
		t.Errorf("expected 0 read records after unread, got %d", count)
	}
}

func TestMarkAsRead_idempotent_viaCoreNotifications(t *testing.T) {
	setupTestDB(t)
	notifications.MarkAsRead("https://github.com/a/b", "abc123", "main")
	if err := notifications.MarkAsRead("https://github.com/a/b", "abc123", "main"); err != nil {
		t.Fatalf("second MarkAsRead() error = %v", err)
	}
	count, _ := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_notification_reads WHERE repo_url = ?`, "https://github.com/a/b").Scan(&c)
		return c, err
	})
	if count != 1 {
		t.Errorf("expected 1 record (idempotent), got %d", count)
	}
}

func TestGetFollowNotifications_withData(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/followers"
	followerURL := "https://github.com/follower/repo"
	// Insert follower
	_ = InsertFollower(followerURL, wsURL, "list1", "commit1", time.Now())
	// Insert a commit so author name can be resolved
	_ = cache.InsertCommits([]cache.Commit{{
		Hash: "flw_commit01", RepoURL: followerURL, Branch: "main",
		AuthorName: "Follower Name", AuthorEmail: "follower@test.com",
		Message: "hello", Timestamp: time.Now(),
	}})
	// Insert list repo mapping for branch resolution
	_ = cache.ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO core_lists (id, name, source, version, workdir) VALUES (?, ?, ?, ?, ?)`,
			"list1", "Test", "local", "0.1.0", "/tmp")
		db.Exec(`INSERT INTO core_list_repositories (list_id, repo_url, branch) VALUES (?, ?, ?)`,
			"list1", followerURL, "main")
		return nil
	})
	notifs, err := getFollowNotifications(wsURL, false)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(notifs) == 0 {
		t.Fatal("expected at least 1 follow notification")
	}
	n := notifs[0]
	if n.Type != NotificationTypeFollow {
		t.Errorf("Type = %q", n.Type)
	}
	if n.ActorRepo != followerURL {
		t.Errorf("ActorRepo = %q", n.ActorRepo)
	}
	if n.Actor.Name != "Follower Name" {
		t.Errorf("Actor.Name = %q", n.Actor.Name)
	}
	if n.Branch != "main" {
		t.Errorf("Branch = %q", n.Branch)
	}
}

func TestGetFollowNotifications_unreadOnly(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/fol-unread"
	followerURL := "https://github.com/fol-unread/repo"
	_ = InsertFollower(followerURL, wsURL, "list1", "", time.Now())
	// Mark as read
	_ = notifications.MarkAsRead(followerURL, "follow", "")
	notifs, err := getFollowNotifications(wsURL, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(notifs) != 0 {
		t.Errorf("expected 0 unread follow notifications, got %d", len(notifs))
	}
}

func TestGetFollowNotifications_noAuthor(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/fol-noauth"
	followerURL := "https://github.com/fol-noauth/repo"
	_ = InsertFollower(followerURL, wsURL, "", "", time.Now())
	notifs, err := getFollowNotifications(wsURL, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(notifs) == 0 {
		t.Fatal("expected follow notification")
	}
	// With no commits, author falls back to display name from URL
	if notifs[0].Actor.Name == "" {
		t.Error("Actor.Name should fallback to display name")
	}
}
