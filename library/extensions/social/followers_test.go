// followers_test.go - Tests for follower tracking
package social

import (
	"testing"
	"time"
)

func TestInsertFollower(t *testing.T) {
	setupTestDB(t)
	err := InsertFollower("https://github.com/alice/repo", "https://github.com/bob/repo", "list1", "abc123456789", time.Now())
	if err != nil {
		t.Fatalf("InsertFollower() error = %v", err)
	}
}

func TestGetFollowers(t *testing.T) {
	setupTestDB(t)
	workspace := "https://github.com/bob/repo"
	InsertFollower("https://github.com/alice/repo", workspace, "list1", "aaa111222333", time.Now().Add(-1*time.Hour))
	InsertFollower("https://github.com/charlie/repo", workspace, "list1", "bbb111222333", time.Now())

	followers, err := GetFollowers(workspace)
	if err != nil {
		t.Fatalf("GetFollowers() error = %v", err)
	}
	if len(followers) != 2 {
		t.Errorf("expected 2 followers, got %d", len(followers))
	}
}

func TestGetFollowerSet(t *testing.T) {
	setupTestDB(t)
	workspace := "https://github.com/bob/repo"
	InsertFollower("https://github.com/alice/repo", workspace, "list1", "aaa111222333", time.Now())
	InsertFollower("https://github.com/charlie/repo", workspace, "list1", "bbb111222333", time.Now())

	set, err := GetFollowerSet(workspace)
	if err != nil {
		t.Fatalf("GetFollowerSet() error = %v", err)
	}
	if len(set) != 2 {
		t.Errorf("expected 2 entries in set, got %d", len(set))
	}
	if !set["https://github.com/alice/repo"] {
		t.Error("alice should be in follower set")
	}
}

func TestGetFollowers_empty(t *testing.T) {
	setupTestDB(t)
	followers, err := GetFollowers("https://github.com/nobody/repo")
	if err != nil {
		t.Fatalf("GetFollowers() error = %v", err)
	}
	if len(followers) != 0 {
		t.Errorf("expected 0 followers, got %d", len(followers))
	}
}
