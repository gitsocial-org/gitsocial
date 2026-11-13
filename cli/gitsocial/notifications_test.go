// notifications_test.go - Tests for notification formatting
package main

import (
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

// TestFormatNotification_divergedBranchNamesTheBranch checks the CLI line names the branch and its state.
func TestFormatNotification_divergedBranchNamesTheBranch(t *testing.T) {
	t.Parallel()
	out := formatNotification(notifications.Notification{
		RepoURL:   "https://github.com/user/repo",
		Branch:    "gitmsg/social",
		Type:      "branch-diverged",
		Source:    "gitmsg-divergence",
		Item:      notifications.DivergenceNotification{Branch: "gitmsg/social"},
		Timestamp: time.Now(),
	})

	if !strings.Contains(out, "[branch-diverged]") {
		t.Errorf("output missing the type label:\n%s", out)
	}
	if !strings.Contains(out, "gitmsg/social diverged from origin") {
		t.Errorf("output missing the branch and its state:\n%s", out)
	}
}

// TestNotificationKey_roundTrip checks that the id that the list prints is the id that read and unread take, with no branch.
func TestNotificationKey_roundTrip(t *testing.T) {
	for _, n := range []notifications.Notification{
		{RepoURL: "https://github.com/a/b", Hash: "abc123456789"},
		{RepoURL: "https://github.com/a/b", Hash: "follow"},
	} {
		repoURL, hash, ok := notificationKey(notificationID(n))
		if !ok || repoURL != n.RepoURL || hash != n.Hash {
			t.Errorf("notificationKey(%q) = %q, %q, %v, want the key of the event", notificationID(n), repoURL, hash, ok)
		}
	}
	for _, id := range []string{"#commit:abc123456789", "#follow", "https://github.com/a/b#branch:main", "abc123456789"} {
		if _, _, ok := notificationKey(id); ok {
			t.Errorf("notificationKey(%q) is valid, want an error for an id with no repository or no commit", id)
		}
	}
}

// TestNotificationsLevel_setAndList checks that the command sets the level of the workspace and of a repository, prints them, and removes one with default.
func TestNotificationsLevel_setAndList(t *testing.T) {
	t.Setenv("GITSOCIAL_PERSONAL_REPO", t.TempDir()+"/personal")
	dir, cacheDir := initCLITestRepo(t), t.TempDir()
	const other = "https://github.com/test/other-repo"
	run := func(args ...string) string {
		t.Helper()
		stdout, stderr, code := runInProcess(t, dir, cacheDir, append([]string{"notifications", "level"}, args...)...)
		if code != 0 {
			t.Fatalf("notifications level %v: exit %d\n%s%s", args, code, stdout, stderr)
		}
		return stdout
	}
	if out := run(); !strings.Contains(out, "No notification levels.") {
		t.Errorf("levels before a set = %q, want none", out)
	}
	run("all")
	run(other, "off")
	out := run()
	if !strings.Contains(out, "https://github.com/test/cli-repo  all since ") || !strings.Contains(out, other+"  off") {
		t.Errorf("levels = %q, want the workspace at all with its set time and the other repository at off", out)
	}
	if out := run(other); strings.TrimSpace(out) != other+"  off" {
		t.Errorf("level of one repository = %q, want off", out)
	}
	if err := settings.WriteNotificationLevel(other, settings.LevelAll, time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil {
		t.Fatalf("write the level: %v", err)
	}
	if out := run(other, "all"); !strings.Contains(out, "all since 2020-01-02T03:04:05Z") {
		t.Errorf("a second set of all = %q, want the first set time", out)
	}
	run(other, "default")
	if out := run(); strings.Contains(out, other) {
		t.Errorf("levels after default = %q, want the entry removed", out)
	}
	if _, _, code := runInProcess(t, dir, cacheDir, "notifications", "level", other, "loud"); code != ExitInvalidArgs {
		t.Errorf("an invalid level gave exit %d, want %d", code, ExitInvalidArgs)
	}
}
