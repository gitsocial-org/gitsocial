// avatars_test.go - Tests for the GitHub avatar lookups
package github

import (
	"errors"
	"strings"
	"testing"

	importpkg "github.com/gitsocial-org/gitsocial/library/import"
)

// TestResolveAvatar_noreply: a noreply address resolves with no API call.
func TestResolveAvatar_noreply(t *testing.T) {
	rec := fakeGH(t)
	for email, want := range map[string]string{
		"octocat@users.noreply.github.com":         "https://github.com/octocat.png?size=80",
		"583231+Octo-Cat@users.noreply.github.com": "https://github.com/octo-cat.png?size=80",
	} {
		if got, err := AvatarURL("o/r", email, []string{"https://github.com/o/r/issues/1"}, "abc"); err != nil || got != want {
			t.Errorf("AvatarURL(%q) = %q, %v, want %q", email, got, err, want)
		}
	}
	if _, ok := ParseNoreply("octocat@example.com"); ok {
		t.Error("an address on another domain parses as noreply")
	}
	if rec.callCount() != 0 {
		t.Errorf("a noreply address ran gh %d times", rec.callCount())
	}
}

// TestResolveAvatar_lookups: each origin URL and a commit sha take one gh api call on their endpoint.
func TestResolveAvatar_lookups(t *testing.T) {
	for _, c := range []struct {
		origins   []string
		sha, path string
		field     string
	}{
		{[]string{"https://github.com/o/r/issues/7"}, "", "repos/o/r/issues/7", ".user.avatar_url"},
		{[]string{"https://github.com/o/r/pull/8"}, "", "repos/o/r/issues/8", ".user.avatar_url"},
		{[]string{"https://github.com/o/r/issues/7#issuecomment-99"}, "", "repos/o/r/issues/comments/99", ".user.avatar_url"},
		{[]string{"https://github.com/o/r/releases/tag/v1.0"}, "", "repos/o/r/releases/tags/v1.0", ".author.avatar_url"},
		{nil, "abc123", "repos/o/r/commits/abc123", ".author.avatar_url"},
		{[]string{"https://github.com/o/r/pull/8#discussion_r5", "https://github.com/o/r/issues/7"}, "abc123", "repos/o/r/issues/7", ".user.avatar_url"},
		{[]string{"https://github.com/o/r/discussions/3"}, "abc123", "repos/o/r/commits/abc123", ".author.avatar_url"},
	} {
		name := strings.Join(c.origins, ",") + c.sha
		rec := fakeGH(t, ghResponse{stdout: "https://avatars.githubusercontent.com/u/1?v=4\n"})
		got, err := AvatarURL("o/r", "ada@example.com", c.origins, c.sha)
		if err != nil || got != "https://avatars.githubusercontent.com/u/1?v=4&s=80" {
			t.Errorf("%s: AvatarURL = %q, %v", name, got, err)
		}
		if rec.callCount() != 1 || strings.Join(rec.calls[0][1:], " ") != "api "+c.path+" --jq "+c.field {
			t.Errorf("%s: gh calls = %v", name, rec.calls)
		}
	}
	rec := fakeGH(t)
	for _, origin := range []string{"https://github.com/o/r/discussions/3", "https://github.com/o/r/pull/8#pullrequestreview-5"} {
		if got, err := AvatarURL("o/r", "ada@example.com", []string{origin}, ""); err != nil || got != "" {
			t.Errorf("%s: AvatarURL = %q, %v, want no lookup", origin, got, err)
		}
	}
	if rec.callCount() != 0 {
		t.Errorf("a URL with no lookup ran gh %d times", rec.callCount())
	}
}

// TestResolveAvatar_rateLimited: a rate limit that outlasts the retries returns ErrRateLimited.
func TestResolveAvatar_rateLimited(t *testing.T) {
	limit := ghResponse{stderr: "API rate limit exceeded", exitCode: 1}
	fakeGH(t, limit, limit, limit, limit)
	if _, err := AvatarURL("o/r", "ada@example.com", nil, "abc"); !errors.Is(err, importpkg.ErrRateLimited) {
		t.Errorf("err = %v, want ErrRateLimited", err)
	}
	fakeGH(t, ghResponse{stderr: "gh: Not Found (HTTP 404)", exitCode: 1})
	if got, err := AvatarURL("o/r", "ada@example.com", nil, "abc"); err != nil || got != "" {
		t.Errorf("a missing item = %q, %v, want no picture and no error", got, err)
	}
	fakeGH(t, ghResponse{stderr: "gh: No commit found for SHA: abc (HTTP 422)", exitCode: 1})
	if got, err := AvatarURL("o/r", "ada@example.com", nil, "abc"); err != nil || got != "" {
		t.Errorf("a sha the forge lacks = %q, %v, want no picture and no error", got, err)
	}
}
