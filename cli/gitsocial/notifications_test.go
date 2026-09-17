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
