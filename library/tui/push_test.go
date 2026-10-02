// push_test.go - Tests for push prompt / remote-picker construction helpers
package tui

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

func TestBuildPushConfirmPrompt_NamesRemoteAndHost(t *testing.T) {
	p := &gitmsg.PushPreview{
		Branches: []gitmsg.BranchPushCount{{Branch: "gitmsg/social", Commits: 3}},
		Refs:     2,
	}
	got := buildPushConfirmPrompt(p, []string{pushTargetLabel("r2", "s3://acct.r2.cloudflarestorage.com/bucket")}, "configured")
	if !strings.Contains(got, "Push to [configured] r2 (acct.r2.cloudflarestorage.com)") {
		t.Errorf("prompt missing remote/host/source: %q", got)
	}
	if !strings.Contains(got, "3 social") || !strings.Contains(got, "2 refs") {
		t.Errorf("prompt missing counts: %q", got)
	}
	if !strings.Contains(got, "tags checked at push") {
		t.Errorf("prompt missing tags note: %q", got)
	}
}

func TestBuildPushConfirmPrompt_EmptyPreviewStillOffers(t *testing.T) {
	got := buildPushConfirmPrompt(&gitmsg.PushPreview{}, []string{pushTargetLabel("origin", "https://github.com/user/repo")}, "origin")
	if strings.Contains(got, "Nothing to push") {
		t.Errorf("empty preview must still offer a push, got %q", got)
	}
	if !strings.Contains(got, "no counted changes; tags checked at push") {
		t.Errorf("empty prompt missing the no-counts note: %q", got)
	}
	if !strings.Contains(got, "Push to [origin] origin") {
		t.Errorf("empty prompt should still name remote: %q", got)
	}
}

func TestBuildPushConfirmPrompt_CodeBranchesNamed(t *testing.T) {
	p := &gitmsg.PushPreview{Code: []gitmsg.BranchPushCount{{Branch: "feature/x", Commits: 2}}}
	got := buildPushConfirmPrompt(p, []string{pushTargetLabel("origin", "")}, "")
	if !strings.Contains(got, "code: feature/x (2)") {
		t.Errorf("prompt missing code branch: %q", got)
	}
	// No parseable host: prompt names the remote alone.
	if strings.Contains(got, "(") && !strings.Contains(got, "(2)") {
		t.Errorf("prompt should not show a host when URL is empty: %q", got)
	}
}

func TestBuildRemotePickerChoices_NumbersThenDefaultAndPersist(t *testing.T) {
	choices := buildRemotePickerChoices([]string{"backup", "r2"}, false)
	keys := make([]string, len(choices))
	for i, c := range choices {
		keys[i] = c.Key
	}
	want := []string{"1", "2", "enter", "D"}
	if len(keys) != len(want) {
		t.Fatalf("choices = %v, want keys %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("choice %d key = %q, want %q", i, keys[i], want[i])
		}
	}
	if !strings.Contains(choices[0].Label, "backup") || !strings.Contains(choices[1].Label, "r2") {
		t.Errorf("choice labels lost remote names: %+v", choices)
	}
}

func TestPushRemoteHost(t *testing.T) {
	cases := map[string]string{
		"s3://acct.r2.cloudflarestorage.com/b/p": "acct.r2.cloudflarestorage.com",
		"https://github.com/user/repo":           "github.com",
		"":                                       "",
		"not a url":                              "",
	}
	for in, want := range cases {
		if got := pushRemoteHost(in); got != want {
			t.Errorf("pushRemoteHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildPushConfirmPrompt_NamesEveryRemote(t *testing.T) {
	p := &gitmsg.PushPreview{Branches: []gitmsg.BranchPushCount{{Branch: "gitmsg/social", Commits: 1}}}
	got := buildPushConfirmPrompt(p, []string{
		pushTargetLabel("r2", "s3://acct.r2.cloudflarestorage.com/bucket"),
		pushTargetLabel("backup", "s3://s3.example.com/bucket"),
	}, "all remotes")
	if !strings.Contains(got, "Push to [all remotes] r2 (acct.r2.cloudflarestorage.com), backup (s3.example.com)") {
		t.Errorf("prompt should name both remotes in order with the source: %q", got)
	}
}

func TestBuildRemotePickerChoices_PersistRoundDropsExtras(t *testing.T) {
	choices := buildRemotePickerChoices([]string{"backup", "r2"}, true)
	if len(choices) != 2 {
		t.Fatalf("persist choices = %+v, want the two remotes alone", choices)
	}
	if choices[0].Key != "1" || choices[1].Key != "2" {
		t.Errorf("persist choice keys = %q %q, want 1 2", choices[0].Key, choices[1].Key)
	}
}

// TestResolveRefLocation maps a ref through the cache: a pm hit opens the issue, a cached commit its post, a miss the raw commit.
func TestResolveRefLocation(t *testing.T) {
	testutil.OpenTempCache(t, "")
	hash := "cafe12345678"
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: "https://example.com/r", Branch: "gitmsg/pm",
		AuthorName: "T", AuthorEmail: "t@t.com", Message: "an issue", Timestamp: time.Now(),
	}}); err != nil {
		t.Fatalf("InsertCommits: %v", err)
	}
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`INSERT INTO pm_items (repo_url, hash, branch, type, state) VALUES (?, ?, ?, 'issue', 'open')`,
			"https://example.com/r", hash, "gitmsg/pm")
		return err
	}); err != nil {
		t.Fatalf("insert pm item: %v", err)
	}
	got := resolveRefLocation("#commit:"+hash+"@gitmsg/pm", "https://example.com/r")
	want := tuicore.LocPMIssueDetail(hash)
	if got.Path != want.Path || got.Param("issueID") != want.Param("issueID") {
		t.Errorf("resolveRefLocation = %+v, want %+v", got, want)
	}
	code := "beef12345678"
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: code, RepoURL: "https://example.com/r", Branch: "feature/x",
		AuthorName: "T", AuthorEmail: "t@t.com", Message: "a code commit", Timestamp: time.Now(),
	}}); err != nil {
		t.Fatalf("InsertCommits: %v", err)
	}
	post := resolveRefLocation("#commit:"+code, "https://example.com/r")
	wantPost := tuicore.LocDetail("https://example.com/r#commit:" + code + "@feature/x")
	if post.Path != wantPost.Path || post.Param("postID") != wantPost.Param("postID") {
		t.Errorf("cached commit = %+v, want %+v", post, wantPost)
	}
	miss := resolveRefLocation("#commit:0123456789ab", "https://example.com/r")
	if miss.Path != tuicore.LocCommitDiff("0123456789ab").Path {
		t.Errorf("unknown hash = %+v, want the commit diff view", miss)
	}
}
