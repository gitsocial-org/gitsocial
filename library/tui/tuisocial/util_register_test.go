// util_register_test.go - Tests for push completion toast formatting
package tuisocial

import (
	"errors"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/client"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

func TestFormatPushCompletion_TagsAndSitePublished(t *testing.T) {
	text, tone := formatPushCompletion(PushCompletedMsg{Results: []client.Result{{
		Push: &gitmsg.PushResult{Remote: "r2", Commits: 3, Refs: 2, Tags: 1},
		Site: client.SiteOutcome{Published: true},
	}}})
	if !strings.Contains(text, "to r2") || !strings.Contains(text, "1 tags") || !strings.Contains(text, "site published") {
		t.Errorf("completion text = %q", text)
	}
	if tone != tuicore.MessageTypeSuccess {
		t.Errorf("published site tone = %v, want success", tone)
	}
}

func TestFormatPushCompletion_SiteFailureIsWarning(t *testing.T) {
	err := errors.New("bucket denied")
	text, tone := formatPushCompletion(PushCompletedMsg{Results: []client.Result{{
		Push: &gitmsg.PushResult{Remote: "r2", Commits: 1},
		Site: client.SiteOutcome{Err: err, Error: err.Error()},
	}}})
	if !strings.Contains(text, "site failed: bucket denied") {
		t.Errorf("failed-site text = %q", text)
	}
	if tone != tuicore.MessageTypeWarning {
		t.Errorf("failed site tone = %v, want warning", tone)
	}
}

func TestFormatPushCompletion_SiteSkipped(t *testing.T) {
	text, _ := formatPushCompletion(PushCompletedMsg{Results: []client.Result{{
		Push: &gitmsg.PushResult{Remote: "origin", Commits: 1},
		Site: client.SiteOutcome{Skipped: "non-s3 remote"},
	}}})
	if !strings.Contains(text, "site skipped") {
		t.Errorf("skipped-site text = %q", text)
	}
}

func TestFormatPushCompletion_NothingToPush(t *testing.T) {
	text, tone := formatPushCompletion(PushCompletedMsg{Results: []client.Result{{
		Push: &gitmsg.PushResult{Remote: "origin"},
	}}})
	if !strings.Contains(text, "Nothing to push") {
		t.Errorf("empty push text = %q", text)
	}
	if tone != tuicore.MessageTypeSuccess {
		t.Errorf("empty push tone = %v, want success", tone)
	}
}

// TestFormatPushCompletion_OneLinePerRemote: a fan-out push reports every remote, in order.
func TestFormatPushCompletion_OneLinePerRemote(t *testing.T) {
	text, _ := formatPushCompletion(PushCompletedMsg{Results: []client.Result{
		{Push: &gitmsg.PushResult{Remote: "r2", Commits: 3, Refs: 1}, Site: client.SiteOutcome{Published: true}},
		{Push: &gitmsg.PushResult{Remote: "backup", Commits: 3, Refs: 1}, Site: client.SiteOutcome{Skipped: "non-s3 remote"}},
	}})
	r2 := strings.Index(text, "to r2")
	backup := strings.Index(text, "to backup")
	if r2 < 0 || backup < 0 {
		t.Fatalf("completion text = %q, want both remotes", text)
	}
	if r2 > backup {
		t.Errorf("completion text = %q, want r2 before backup", text)
	}
}
