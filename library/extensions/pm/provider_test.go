// provider_test.go - Tests for the PM notification provider's three sources
package pm

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
)

// otherAuthorRepo clones the fixture under its own committer, so its issues come from someone else.
func otherAuthorRepo(t *testing.T, name, email string) string {
	t.Helper()
	dir := cloneFixture(t)
	if _, err := git.ExecGit(dir, []string{"config", "user.email", email}); err != nil {
		t.Fatalf("config user.email: %v", err)
	}
	if _, err := git.ExecGit(dir, []string{"config", "user.name", name}); err != nil {
		t.Fatalf("config user.name: %v", err)
	}
	return dir
}

// notifTypeCounts counts notifications by type.
func notifTypeCounts(ns []notifications.Notification) map[string]int {
	counts := make(map[string]int)
	for _, n := range ns {
		counts[n.Type]++
	}
	return counts
}

// TestGetNotifications_everySource asserts one notification from each of the fork, assigned and state-change sources.
func TestGetNotifications_everySource(t *testing.T) {
	workdir := initWorkspace(t)
	me := git.GetUserEmail(workdir)
	if me == "" {
		t.Fatal("the workspace has no committer email")
	}
	fork := otherAuthorRepo(t, "Forker", "forker@test.com")
	other := otherAuthorRepo(t, "Other", "other@test.com")

	newIssue(t, fork, "Filed from the fork", CreateIssueOptions{})
	if err := gitmsg.AddFork(workdir, gitmsg.ResolveRepoURL(fork)); err != nil {
		t.Fatalf("AddFork: %v", err)
	}
	assigned := newIssue(t, other, "Assigned to me", CreateIssueOptions{Assignees: []string{me}})
	closed := StateClosed
	if res := UpdateIssue(other, assigned.ID, UpdateIssueOptions{State: &closed}); !res.Success {
		t.Fatalf("UpdateIssue() failed: %s", res.Error.Message)
	}

	provider := &pmNotificationProvider{}
	notifs, err := provider.GetNotifications(workdir, notifications.Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	counts := notifTypeCounts(notifs)
	for _, want := range []string{"fork-issue", "issue-assigned", "issue-closed"} {
		if counts[want] != 1 {
			t.Errorf("%s = %d notifications, want 1; got %v", want, counts[want], counts)
		}
	}

	count, err := provider.GetUnreadCount(workdir)
	if err != nil {
		t.Fatalf("GetUnreadCount() error = %v", err)
	}
	if count != len(notifs) {
		t.Errorf("GetUnreadCount() = %d, want %d: none of them has been read", count, len(notifs))
	}
}

// TestGetNotifications_unreadOnly asserts a read notification leaves the unread filter and the count.
func TestGetNotifications_unreadOnly(t *testing.T) {
	workdir := initWorkspace(t)
	me := git.GetUserEmail(workdir)
	other := otherAuthorRepo(t, "Other", "other@test.com")

	assigned := newIssue(t, other, "Assigned to me", CreateIssueOptions{Assignees: []string{me}})
	provider := &pmNotificationProvider{}
	notifs, err := provider.GetNotifications(workdir, notifications.Filter{UnreadOnly: true})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if len(notifs) != 1 {
		t.Fatalf("GetNotifications(unread) = %d notifications, want 1", len(notifs))
	}
	if err := notifications.MarkAsRead(notifs[0].RepoURL, notifs[0].Hash, notifs[0].Branch); err != nil {
		t.Fatalf("MarkAsRead() error = %v", err)
	}

	unread, err := provider.GetNotifications(workdir, notifications.Filter{UnreadOnly: true})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if len(unread) != 0 {
		t.Errorf("GetNotifications(unread) = %d notifications after MarkAsRead(%s), want 0", len(unread), assigned.ID)
	}
	count, err := provider.GetUnreadCount(workdir)
	if err != nil {
		t.Fatalf("GetUnreadCount() error = %v", err)
	}
	if count != 0 {
		t.Errorf("GetUnreadCount() = %d after MarkAsRead, want 0", count)
	}
}

// TestIssueStateChange_movedOnce checks one state-change notification for an edit with rows on two branches, whose reference names a branch with no row.
func TestIssueStateChange_movedOnce(t *testing.T) {
	setupTestDB(t)
	const repoURL = "https://github.com/test/moved"
	const issue, edit = "aa0011223344", "bb0011223344"
	ts := time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC)
	closeMsg := "Issue\n\nGitMsg: ext=\"pm\"; type=\"issue\"; state=\"closed\"; assignees=\"me@x.com\"; edits=\"#commit:" + issue + "@feature/x\"; v=\"0.1.0\""
	for _, branch := range []string{"gitmsg/pm", "main"} {
		if err := cache.InsertCommits([]cache.Commit{
			{Hash: issue, RepoURL: repoURL, Branch: branch, AuthorName: "Alice", AuthorEmail: "alice@x.com", Message: "Issue", Timestamp: ts},
			{Hash: edit, RepoURL: repoURL, Branch: branch, AuthorName: "Bob", AuthorEmail: "bob@x.com", Message: closeMsg, Timestamp: ts.Add(time.Hour)},
		}); err != nil {
			t.Fatalf("InsertCommits(%s) error = %v", branch, err)
		}
		for _, item := range []PMItem{
			{RepoURL: repoURL, Hash: issue, Branch: branch, Type: "issue", State: "open", Assignees: sql.NullString{String: "me@x.com", Valid: true}},
			{RepoURL: repoURL, Hash: edit, Branch: branch, Type: "issue", State: "closed", Assignees: sql.NullString{String: "me@x.com", Valid: true}},
		} {
			if err := InsertPMItem(item); err != nil {
				t.Fatalf("InsertPMItem() error = %v", err)
			}
		}
	}
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE core_commits SET stale_since = '2025-10-22T00:00:00Z' WHERE repo_url = ? AND branch = 'gitmsg/pm'`, repoURL)
		return err
	}); err != nil {
		t.Fatalf("mark stale: %v", err)
	}
	got, err := getIssueStateChangeNotifications("me@x.com", false, 0)
	if err != nil {
		t.Fatalf("getIssueStateChangeNotifications() error = %v", err)
	}
	if len(got) != 1 || got[0].Hash != edit || got[0].Branch != "main" {
		t.Errorf("notifications = %+v, want one for the edit on its live row under main", got)
	}
}
