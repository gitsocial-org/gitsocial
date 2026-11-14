// notifications_test.go - Tests for notification formatting
package main

import (
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
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
		Phrase:    "The branch gitmsg/social diverged from its remote",
	})

	if !strings.Contains(out, "[branch-diverged]") {
		t.Errorf("output missing the type label:\n%s", out)
	}
	if !strings.Contains(out, "gitmsg/social diverged from its remote") {
		t.Errorf("output missing the branch and its state:\n%s", out)
	}
}

// TestFormatNotification_everyType checks that each type prints its label, its title, its line 1, its context and its id, and no control character of a remote text.
func TestFormatNotification_everyType(t *testing.T) {
	t.Parallel()
	const repoURL = "https://github.com/user/repo"
	types := []string{"mention", "reference", "edit", "comment", "repost", "quote", "follow", "issue-assigned", "issue-closed", "issue-reopened",
		"fork-issue", "fork-pr", "review-requested", "feedback", "approved", "changes-requested", "pr-merged", "pr-closed", "pr-ready",
		"head-advanced", "base-advanced", "head-deleted", "base-deleted", "new-release", "memo-comment", "inherited-policy", "branch-diverged", "activity"}
	for _, typ := range types {
		n := notifications.Notification{RepoURL: repoURL, Hash: "abc123456789", Type: typ, Actor: notifications.Actor{Name: "Bob\x1b[2J"},
			Subject: "The \x1b]0;subject\x07item", Excerpt: "A\u202e line\nsecond line", Glyph: "↩", Timestamp: time.Now()}
		switch typ {
		case "follow":
			n.Hash = "follow"
		case "edit":
			n.Item = notifications.EditNotification{CanonicalRepoURL: repoURL, CanonicalHash: "def123456789"}
		case "head-advanced", "base-advanced", "head-deleted", "base-deleted", "branch-diverged":
			n.Condition = true
			n.Item = review.ReviewNotification{Content: "#branch:feature\x1b[31m: aaa → bbb"}
		}
		out := formatNotification(n)
		title, body := notifications.MessageText(n)
		if !strings.HasPrefix(out, "["+typ+"] [unread] "+title+" · ") {
			t.Errorf("%s: output does not start with the label and the title %q:\n%s", typ, title, out)
		}
		for _, line := range strings.Split(body, "\n") {
			if line != "" && !strings.Contains(out, "\n  "+line+"\n") {
				t.Errorf("%s: output is missing the body line %q:\n%s", typ, line, out)
			}
		}
		if typ != "follow" && !strings.Contains(out, "item · user/repo\n") {
			t.Errorf("%s: output is missing the context:\n%s", typ, out)
		}
		if id := "  id: " + notificationID(n); n.Condition == strings.Contains(out, id) {
			t.Errorf("%s: id line present = %v, want it for an event only:\n%s", typ, !n.Condition, out)
		}
		if strings.ContainsAny(out, "\x1b\x07\u202e") || strings.Contains(out, "second line") {
			t.Errorf("%s: output holds a control character or a second line of a remote text:\n%q", typ, out)
		}
	}
	edit := formatNotification(notifications.Notification{RepoURL: repoURL, Hash: "abc123456789", Type: "edit", Item: notifications.EditNotification{CanonicalRepoURL: repoURL, CanonicalHash: "def123456789"}})
	if !strings.Contains(edit, "  item: "+repoURL+"#commit:def123456789") {
		t.Errorf("an edit does not name its item:\n%s", edit)
	}
	branch := formatNotification(notifications.Notification{RepoURL: repoURL, Type: "head-advanced", Condition: true, Item: review.ReviewNotification{Content: "#branch:feature: aaa → bbb"}})
	if !strings.Contains(branch, "  #branch:feature: aaa → bbb") {
		t.Errorf("a branch condition does not name the branch and the tips:\n%s", branch)
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
