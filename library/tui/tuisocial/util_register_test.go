// util_register_test.go - Tests for push completion toast formatting
package tuisocial

import (
	"errors"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

func TestFormatPushCompletion_TagsAndSitePublished(t *testing.T) {
	text, tone := formatPushCompletion(PushCompletedMsg{
		Commits: 3, Refs: 2, Tags: 1, Remote: "r2", SitePublished: true,
	})
	if !strings.Contains(text, "to r2") || !strings.Contains(text, "1 tags") || !strings.Contains(text, "site published") {
		t.Errorf("completion text = %q", text)
	}
	if tone != tuicore.MessageTypeSuccess {
		t.Errorf("published site tone = %v, want success", tone)
	}
}

func TestFormatPushCompletion_SiteFailureIsWarning(t *testing.T) {
	text, tone := formatPushCompletion(PushCompletedMsg{
		Commits: 1, Remote: "r2", SiteErr: errors.New("bucket denied"),
	})
	if !strings.Contains(text, "site failed: bucket denied") {
		t.Errorf("failed-site text = %q", text)
	}
	if tone != tuicore.MessageTypeWarning {
		t.Errorf("failed site tone = %v, want warning", tone)
	}
}

func TestFormatPushCompletion_SiteSkipped(t *testing.T) {
	text, _ := formatPushCompletion(PushCompletedMsg{
		Commits: 1, Remote: "origin", SiteSkipped: "non-s3 remote",
	})
	if !strings.Contains(text, "site skipped") {
		t.Errorf("skipped-site text = %q", text)
	}
}

func TestFormatPushCompletion_NothingToPush(t *testing.T) {
	text, tone := formatPushCompletion(PushCompletedMsg{Remote: "origin"})
	if !strings.Contains(text, "Nothing to push") {
		t.Errorf("empty push text = %q", text)
	}
	if tone != tuicore.MessageTypeSuccess {
		t.Errorf("empty push tone = %v, want success", tone)
	}
}
