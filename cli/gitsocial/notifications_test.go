// notifications_test.go - Tests for notification formatting
package main

import (
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/notifications"
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
