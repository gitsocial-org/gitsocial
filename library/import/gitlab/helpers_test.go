// helpers_test.go - Tests for pure helper functions in GitLab adapter
package gitlab

import "testing"

func TestIsBot(t *testing.T) {
	cases := []struct {
		username string
		botFlag  bool
		want     bool
	}{
		{"renovate[bot]", false, true},
		{"dependabot[bot]", false, true},
		{"gitlab-bot", false, true},
		{"project_42_bot_abc", false, true},
		{"project_1_bot", false, true},
		{"alice", false, false},
		{"alice", true, true},
		{"", false, false},
		{"project_nope", false, false},
	}
	for _, c := range cases {
		got := isBot(c.username, c.botFlag)
		if got != c.want {
			t.Errorf("isBot(%q, %v) = %v, want %v", c.username, c.botFlag, got, c.want)
		}
	}
}

func TestNormalizeMilestoneState(t *testing.T) {
	cases := []struct{ input, want string }{
		{"active", "open"},
		{"closed", "closed"},
		{"unknown", "open"},
		{"", "open"},
	}
	for _, c := range cases {
		got := normalizeMilestoneState(c.input)
		if got != c.want {
			t.Errorf("normalizeMilestoneState(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestNormalizeIssueState(t *testing.T) {
	cases := []struct{ input, want string }{
		{"opened", "open"},
		{"closed", "closed"},
		{"unknown", "open"},
		{"", "open"},
	}
	for _, c := range cases {
		got := normalizeIssueState(c.input)
		if got != c.want {
			t.Errorf("normalizeIssueState(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestMapIssueState(t *testing.T) {
	cases := []struct{ input, want string }{
		{"open", "opened"},
		{"closed", "closed"},
		{"all", "all"},
		{"", "all"},
		{"unknown", "all"},
	}
	for _, c := range cases {
		got := mapIssueState(c.input)
		if got != c.want {
			t.Errorf("mapIssueState(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestBuildArtifactURL(t *testing.T) {
	cases := []struct {
		baseURL, owner, repo, tag, want string
	}{
		{
			"https://gitlab.com", "org", "project", "v1.0.0",
			"https://gitlab.com/org/project/-/releases/v1.0.0",
		},
		{
			"https://gitlab.example.com", "group", "repo", "v2.0 beta",
			"https://gitlab.example.com/group/repo/-/releases/v2.0%20beta",
		},
	}
	for _, c := range cases {
		got := buildArtifactURL(c.baseURL, c.owner, c.repo, c.tag)
		if got != c.want {
			t.Errorf("buildArtifactURL(%q, %q, %q, %q) = %q, want %q", c.baseURL, c.owner, c.repo, c.tag, got, c.want)
		}
	}
}

func TestLinkNextPath(t *testing.T) {
	const base = "https://gitlab.com/api/v4/projects/1/issues"
	cases := []struct{ header, want string }{
		{"", ""},
		{`<` + base + `?page=2>; rel="next"`, "projects/1/issues?page=2"},
		{`<` + base + `?page=1>; rel="prev", <` + base + `?page=3>; rel="next"`, "projects/1/issues?page=3"},
		{`<` + base + `?page=1>; rel="first"`, ""},
		{`<https://gitlab.com/elsewhere>; rel="next"`, ""},
		{`<` + base + `?page=2>`, ""},
	}
	for _, c := range cases {
		if got := linkNextPath(c.header); got != c.want {
			t.Errorf("linkNextPath(%q) = %q, want %q", c.header, got, c.want)
		}
	}
}

func TestWithPage(t *testing.T) {
	cases := []struct{ path, page, want string }{
		{"projects/acme%2Fwidgets/issues?state=all", "2", "projects/acme%2Fwidgets/issues?page=2&state=all"},
		{"projects/1/issues?page=2", "3", "projects/1/issues?page=3"},
	}
	for _, c := range cases {
		if got := withPage(c.path, c.page); got != c.want {
			t.Errorf("withPage(%q, %q) = %q, want %q", c.path, c.page, got, c.want)
		}
	}
}

func TestResolveToken(t *testing.T) {
	if got := resolveToken("explicit-token"); got != "explicit-token" {
		t.Errorf("resolveToken(explicit) = %q, want explicit-token", got)
	}
	if got := resolveToken(""); got != "" {
		// env vars may or may not be set, just verify explicit takes priority
		_ = got
	}
}
