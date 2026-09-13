// registry_card_test.go - Tests for the core notification card renderers
package tuicore

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/notifications"
)

// TestCardRenderer_divergedBranchNamesTheBranch checks the card names the branch and its state.
func TestCardRenderer_divergedBranchNamesTheBranch(t *testing.T) {
	renderer := GetItemToCardFunc(ItemType{Extension: "gitmsg-divergence", Type: "branch-diverged"})
	card := renderer(notifications.Notification{
		RepoURL: "https://github.com/user/repo",
		Branch:  "gitmsg/social",
		Type:    "branch-diverged",
		Source:  "gitmsg-divergence",
		Item:    notifications.DivergenceNotification{Branch: "gitmsg/social"},
	}, nil)

	if card.Header.Title != "gitmsg/social" {
		t.Errorf("card title = %q, want %q", card.Header.Title, "gitmsg/social")
	}
	if card.Header.Badge != "diverged from origin" {
		t.Errorf("card badge = %q, want %q", card.Header.Badge, "diverged from origin")
	}
}
