// activity_test.go - Tests for the activity events of a repository at the notification level all
package social

import (
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

// activityBase is the time of the first commit of each activity test.
var activityBase = time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC)

// activityWorkspace returns a workspace, its URL and the user email, with the personal repository at a temp dir.
func activityWorkspace(t *testing.T) (string, string, string) {
	t.Helper()
	t.Setenv("GITSOCIAL_PERSONAL_REPO", filepath.Join(t.TempDir(), "personal"))
	workdir := initWorkspace(t)
	return workdir, gitmsg.ResolveRepoURL(workdir), git.GetUserEmail(workdir)
}

// activityCommit builds a commit of a repository by the given author at the given second after activityBase.
func activityCommit(repoURL, hash, branch, email string, second int, subject, header string) cache.Commit {
	message := subject
	if header != "" {
		message += "\n\nGitMsg: " + header + "; v=\"0.1.0\""
	}
	return cache.Commit{
		Hash: hash, RepoURL: repoURL, Branch: branch, AuthorName: email, AuthorEmail: email,
		Message: message, Timestamp: activityBase.Add(time.Duration(second) * time.Second),
	}
}

// insertActivity inserts commits into the cache.
func insertActivity(t *testing.T, commits ...cache.Commit) {
	t.Helper()
	if err := cache.InsertCommits(commits); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
}

// setAll sets a repository to the level all with a set time at the given second after activityBase.
func setAll(t *testing.T, repoURL string, second int) {
	t.Helper()
	if err := settings.WriteNotificationLevel(repoURL, settings.LevelAll, activityBase.Add(time.Duration(second)*time.Second)); err != nil {
		t.Fatalf("WriteNotificationLevel() error = %v", err)
	}
}

// activityHashes returns the commit hash of each activity event of the social provider, sorted, and of each other event under "other".
func activityHashes(t *testing.T, workdir string, unreadOnly bool) (activity, other []string) {
	t.Helper()
	items, err := (&notificationProvider{}).GetNotifications(workdir, notifications.Filter{UnreadOnly: unreadOnly})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	for _, n := range items {
		if n.Type == string(NotificationTypeActivity) {
			activity = append(activity, n.Hash)
		} else {
			other = append(other, n.Type+" "+n.Hash)
		}
	}
	slices.Sort(activity)
	return activity, other
}

// TestLevel_defaultAddsNothing: with no entry for the workspace, the provider gives the comment and no activity event.
func TestLevel_defaultAddsNothing(t *testing.T) {
	workdir, workspaceURL, userEmail := activityWorkspace(t)
	const other = "https://github.com/activity/other"
	insertActivity(t,
		activityCommit(workspaceURL, "ac0000000001", "gitmsg/social", userEmail, 0, "Mine", ""),
		activityCommit(workspaceURL, "ac0000000002", "gitmsg/social", "b@example.com", 10, "Theirs", ""),
		activityCommit(other, "ac0000000003", "gitmsg/social", "b@example.com", 20, "A comment", ""),
	)
	if err := InsertSocialItem(SocialItem{RepoURL: other, Hash: "ac0000000003", Branch: "gitmsg/social", Type: "comment",
		OriginalRepoURL: cache.ToNullString(workspaceURL), OriginalHash: cache.ToNullString("ac0000000001"), OriginalBranch: cache.ToNullString("gitmsg/social")}); err != nil {
		t.Fatalf("InsertSocialItem() error = %v", err)
	}
	setAll(t, "https://github.com/activity/unrelated", -10)
	activity, rest := activityHashes(t, workdir, false)
	if len(activity) != 0 || !slices.Equal(rest, []string{"comment ac0000000003"}) {
		t.Errorf("events = activity %v, other %v, want only the comment", activity, rest)
	}
}

// TestLevel_allGivesTimelineRows: the workspace at all gives one event for each timeline row by another author, an action row included.
func TestLevel_allGivesTimelineRows(t *testing.T) {
	workdir, workspaceURL, _ := activityWorkspace(t)
	insertActivity(t,
		activityCommit(workspaceURL, "ac0000000001", "gitmsg/pm", "b@example.com", 0, "Fix the build", `ext="pm"; type="issue"; state="open"`),
		activityCommit(workspaceURL, "ac0000000002", "gitmsg/pm", "b@example.com", 10, "Fix the build", `ext="pm"; type="issue"; edits="#commit:ac0000000001@gitmsg/pm"; state="closed"`),
		activityCommit(workspaceURL, "ac0000000003", "gitmsg/pm", "b@example.com", 20, "Fix the build", `ext="pm"; type="issue"; edits="#commit:ac0000000001@gitmsg/pm"; state="closed"; labels="kind/bug"`),
		activityCommit(workspaceURL, "ac0000000004", "gitmsg/social", "c@example.com", 30, "Hello", ""),
	)
	setAll(t, workspaceURL, -10)
	timeline, err := getTimeline(nil, workspaceURL, workspaceURL, nil, 50, "")
	if err != nil {
		t.Fatalf("getTimeline() error = %v", err)
	}
	want := make([]string, 0, len(timeline))
	for _, item := range timeline {
		want = append(want, item.Hash)
	}
	slices.Sort(want)
	if activity, _ := activityHashes(t, workdir, false); !slices.Equal(activity, want) || len(want) != 3 {
		t.Errorf("activity = %v, want the timeline rows %v", activity, want)
	}
	items, _ := (&notificationProvider{}).GetNotifications(workdir, notifications.Filter{})
	for _, n := range items {
		if post := n.Item.(Notification).Item; n.Hash == "ac0000000002" && (post.Action != "closed" || n.Source != "social" || n.RepoURL != workspaceURL) {
			t.Errorf("close event = action %q, source %q, repository %q, want the action of the timeline row", post.Action, n.Source, n.RepoURL)
		}
	}
}

// TestLevel_allStartsAtSetTime: a row at or before the set time gives no event.
func TestLevel_allStartsAtSetTime(t *testing.T) {
	workdir, workspaceURL, _ := activityWorkspace(t)
	insertActivity(t,
		activityCommit(workspaceURL, "ac0000000001", "gitmsg/social", "b@example.com", 0, "Before", ""),
		activityCommit(workspaceURL, "ac0000000002", "gitmsg/social", "b@example.com", 10, "At", ""),
		activityCommit(workspaceURL, "ac0000000003", "gitmsg/social", "b@example.com", 20, "After", ""),
	)
	setAll(t, workspaceURL, 10)
	if activity, _ := activityHashes(t, workdir, false); !slices.Equal(activity, []string{"ac0000000003"}) {
		t.Errorf("activity = %v, want only the row after the set time", activity)
	}
}

// TestLevel_allSkipsOwnItems: a row by the user gives no event.
func TestLevel_allSkipsOwnItems(t *testing.T) {
	workdir, workspaceURL, userEmail := activityWorkspace(t)
	insertActivity(t,
		activityCommit(workspaceURL, "ac0000000001", "gitmsg/social", userEmail, 0, "Mine", ""),
		activityCommit(workspaceURL, "ac0000000002", "gitmsg/social", "b@example.com", 10, "Theirs", ""),
	)
	setAll(t, workspaceURL, -10)
	if activity, _ := activityHashes(t, workdir, false); !slices.Equal(activity, []string{"ac0000000002"}) {
		t.Errorf("activity = %v, want only the row of the other author", activity)
	}
}

// TestLevel_allOnlyInScope: a repository at all gives events only when it is in a list of the workdir.
func TestLevel_allOnlyInScope(t *testing.T) {
	workdir, _, _ := activityWorkspace(t)
	const followed = "https://github.com/activity/followed"
	insertActivity(t, activityCommit(followed, "ac0000000001", "gitmsg/social", "b@example.com", 0, "Hello", ""))
	setAll(t, followed, -10)
	if activity, _ := activityHashes(t, workdir, false); len(activity) != 0 {
		t.Errorf("activity of a repository that the workdir does not use = %v, want none", activity)
	}
	if err := cache.ExecLocked(func(db *sql.DB) error {
		if _, err := db.Exec(`INSERT INTO core_lists (id, name, source, version, workdir) VALUES (?, ?, ?, ?, ?)`,
			"activity-follow", "Follow", "local", "0.1.0", workdir); err != nil {
			return err
		}
		_, err := db.Exec(`INSERT INTO core_list_repositories (list_id, repo_url, branch) VALUES (?, ?, ?)`, "activity-follow", followed, "*")
		return err
	}); err != nil {
		t.Fatalf("seed list: %v", err)
	}
	if activity, _ := activityHashes(t, workdir, false); !slices.Equal(activity, []string{"ac0000000001"}) {
		t.Errorf("activity of a list repository = %v, want its row", activity)
	}
}

// TestLevel_forkCopyGivesNoEvent: a fork that takes the level all of the workspace gives an event only for a commit that the workspace does not have.
func TestLevel_forkCopyGivesNoEvent(t *testing.T) {
	workdir, workspaceURL, _ := activityWorkspace(t)
	const fork = "https://github.com/activity/fork"
	if err := gitmsg.AddFork(workdir, fork); err != nil {
		t.Fatalf("AddFork() error = %v", err)
	}
	insertActivity(t,
		activityCommit(workspaceURL, "ac0000000001", "gitmsg/social", "b@example.com", 0, "Shared", ""),
		activityCommit(fork, "ac0000000001", "gitmsg/social", "b@example.com", 0, "Shared", ""),
		activityCommit(fork, "ac0000000002", "gitmsg/social", "c@example.com", 10, "Fork only", ""),
	)
	setAll(t, workspaceURL, -10)
	if activity, _ := activityHashes(t, workdir, false); !slices.Equal(activity, []string{"ac0000000001", "ac0000000002"}) {
		t.Errorf("activity = %v, want the workspace row and the fork-only row, once each", activity)
	}
}

// TestLevel_assignAfterReadGivesEvent: an assign after a read activity event of the issue gives a new unread event.
func TestLevel_assignAfterReadGivesEvent(t *testing.T) {
	workdir, workspaceURL, _ := activityWorkspace(t)
	insertActivity(t, activityCommit(workspaceURL, "ac0000000001", "gitmsg/pm", "b@example.com", 0, "Fix the build", `ext="pm"; type="issue"; state="open"`))
	setAll(t, workspaceURL, -10)
	if err := notifications.MarkAsRead(workspaceURL, "ac0000000001"); err != nil {
		t.Fatalf("MarkAsRead() error = %v", err)
	}
	if activity, _ := activityHashes(t, workdir, true); len(activity) != 0 {
		t.Fatalf("unread activity after the read = %v, want none", activity)
	}
	insertActivity(t, activityCommit(workspaceURL, "ac0000000002", "gitmsg/pm", "c@example.com", 10, "Fix the build",
		`ext="pm"; type="issue"; edits="#commit:ac0000000001@gitmsg/pm"; state="open"; assignees="d@example.com"`))
	if activity, _ := activityHashes(t, workdir, true); !slices.Equal(activity, []string{"ac0000000002"}) {
		t.Errorf("unread activity after the assign = %v, want the assign", activity)
	}
}
