// push_test.go - Tests for push prompt / remote-picker construction helpers
package tui

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
)

func TestBuildPushConfirmPrompt_NamesRemoteAndHost(t *testing.T) {
	p := &gitmsg.PushPreview{
		Branches: []gitmsg.BranchPushCount{{Branch: "gitmsg/social", Commits: 3}},
		Refs:     2,
	}
	got := buildPushConfirmPrompt(p, "r2", "s3://acct.r2.cloudflarestorage.com/bucket")
	if !strings.Contains(got, "Push to r2 (acct.r2.cloudflarestorage.com)") {
		t.Errorf("prompt missing remote/host: %q", got)
	}
	if !strings.Contains(got, "3 social") || !strings.Contains(got, "2 refs") {
		t.Errorf("prompt missing counts: %q", got)
	}
	if !strings.Contains(got, "tags checked at push") {
		t.Errorf("prompt missing tags note: %q", got)
	}
}

func TestBuildPushConfirmPrompt_EmptyPreviewStillOffers(t *testing.T) {
	got := buildPushConfirmPrompt(&gitmsg.PushPreview{}, "origin", "https://github.com/user/repo")
	if strings.Contains(got, "Nothing to push") {
		t.Errorf("empty preview must still offer a push, got %q", got)
	}
	if !strings.Contains(got, "no counted changes; tags checked at push") {
		t.Errorf("empty prompt missing honest note: %q", got)
	}
	if !strings.Contains(got, "Push to origin") {
		t.Errorf("empty prompt should still name remote: %q", got)
	}
}

func TestBuildPushConfirmPrompt_CodeBranchesNamed(t *testing.T) {
	p := &gitmsg.PushPreview{Code: []gitmsg.BranchPushCount{{Branch: "feature/x", Commits: 2}}}
	got := buildPushConfirmPrompt(p, "origin", "")
	if !strings.Contains(got, "code: feature/x (2)") {
		t.Errorf("prompt missing code branch: %q", got)
	}
	// No parseable host: prompt names the remote alone.
	if strings.Contains(got, "(") && !strings.Contains(got, "(2)") {
		t.Errorf("prompt should not show a host when URL is empty: %q", got)
	}
}

func TestBuildRemotePickerChoices_NumbersThenDefaultAndPersist(t *testing.T) {
	choices := buildRemotePickerChoices([]string{"backup", "r2"})
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
