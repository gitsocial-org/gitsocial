// util_register_test.go - Tests for release card conversion and helpers
package tuirelease

import (
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/extensions/release"
)

func TestBoolToYesNo(t *testing.T) {
	if got := boolToYesNo(true); got != "yes" {
		t.Errorf("boolToYesNo(true) = %q, want yes", got)
	}
	if got := boolToYesNo(false); got != "no" {
		t.Errorf("boolToYesNo(false) = %q, want no", got)
	}
}

func TestReleaseDimmedChecker(t *testing.T) {
	active := release.Release{IsRetracted: false}
	if releaseDimmedChecker(active) {
		t.Error("active release should not be dimmed")
	}

	retracted := release.Release{IsRetracted: true}
	if !releaseDimmedChecker(retracted) {
		t.Error("retracted release should be dimmed")
	}

	wrapped := releaseItemData{Release: release.Release{IsRetracted: true}}
	if !releaseDimmedChecker(wrapped) {
		t.Error("wrapped retracted release should be dimmed")
	}

	if releaseDimmedChecker("invalid") {
		t.Error("invalid type should not be dimmed")
	}
}

func TestReleaseToCard_basic(t *testing.T) {
	rel := release.Release{
		Subject:   "Initial release",
		Version:   "1.0.0",
		Tag:       "v1.0.0",
		Author:    release.Author{Name: "Alice", Email: "alice@test.com"},
		Timestamp: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
	}

	card := ReleaseToCard(rel)
	if card.Header.Icon != "⏏" {
		t.Errorf("Icon = %q, want ⏏", card.Header.Icon)
	}
	if card.Header.Title != "1.0.0 Initial release" {
		t.Errorf("Title = %q, want '1.0.0 Initial release'", card.Header.Title)
	}
}

func TestReleaseToCard_prerelease(t *testing.T) {
	rel := release.Release{
		Subject:    "Beta",
		Prerelease: true,
	}
	card := ReleaseToCard(rel)
	if card.Header.Icon != "⏏" {
		t.Errorf("Icon = %q, want ⏏ for prerelease", card.Header.Icon)
	}
	if card.Header.Badge != "pre-release" {
		t.Errorf("Badge = %q, want 'pre-release'", card.Header.Badge)
	}
}

func TestReleaseToCard_noVersion(t *testing.T) {
	rel := release.Release{Subject: "Hotfix"}
	card := ReleaseToCard(rel)
	if card.Header.Title != "Hotfix" {
		t.Errorf("Title = %q, want 'Hotfix' (no version prefix)", card.Header.Title)
	}
}

func TestReleaseToCardWithOptions_showEmail(t *testing.T) {
	rel := release.Release{
		Subject: "Release",
		Author:  release.Author{Name: "Bob", Email: "bob@test.com"},
	}
	card := ReleaseToCardWithOptions(rel, ReleaseToCardOptions{ShowEmail: true})
	found := false
	for _, p := range card.Header.Subtitle {
		if p.Text == "Bob <bob@test.com>" {
			found = true
		}
	}
	if !found {
		t.Error("ShowEmail=true should include email in subtitle")
	}
}

func TestReleaseToCard_withComments(t *testing.T) {
	rel := release.Release{
		Subject:  "Release",
		Comments: 5,
		ID:       "test-id",
	}
	card := ReleaseToCard(rel)
	found := false
	for _, p := range card.Header.Subtitle {
		if p.Text == "↩ 5" {
			found = true
			if p.Link == nil {
				t.Error("comment count should have a link")
			}
		}
	}
	if !found {
		t.Error("should include comment count in subtitle")
	}
}

func TestReleaseToCard_body(t *testing.T) {
	rel := release.Release{
		Subject: "v2.0",
		Body:    "Major update with breaking changes",
	}
	card := ReleaseToCard(rel)
	if card.Content.Text != "Major update with breaking changes" {
		t.Errorf("Content.Text = %q", card.Content.Text)
	}
}

func TestReleaseCardRenderer_invalidType(t *testing.T) {
	card := releaseCardRenderer("not a release", nil)
	if card.Header.Title != "Invalid release" {
		t.Errorf("Title = %q, want 'Invalid release'", card.Header.Title)
	}
}
