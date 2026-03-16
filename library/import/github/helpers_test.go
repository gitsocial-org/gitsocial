// helpers_test.go - Tests for pure helper functions in GitHub adapter
package github

import "testing"

func TestLoginToEmail(t *testing.T) {
	cases := []struct{ input, want string }{
		{"octocat", "octocat@users.noreply.github.com"},
		{"alice", "alice@users.noreply.github.com"},
		{"", ""},
	}
	for _, c := range cases {
		got := loginToEmail(c.input)
		if got != c.want {
			t.Errorf("loginToEmail(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestLoginToName(t *testing.T) {
	cases := []struct{ input, want string }{
		{"octocat", "@octocat"},
		{"alice", "@alice"},
		{"", ""},
	}
	for _, c := range cases {
		got := loginToName(c.input)
		if got != c.want {
			t.Errorf("loginToName(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestNormalizeState(t *testing.T) {
	cases := []struct{ input, want string }{
		{"OPEN", "open"},
		{"open", "open"},
		{"CLOSED", "closed"},
		{"closed", "closed"},
		{"MERGED", "merged"},
		{"merged", "merged"},
		{"unknown", "open"},
		{"", "open"},
	}
	for _, c := range cases {
		got := normalizeState(c.input)
		if got != c.want {
			t.Errorf("normalizeState(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestIsBot(t *testing.T) {
	bots := []string{"dependabot", "dependabot[bot]", "renovate", "renovate[bot]", "github-actions", "github-actions[bot]"}
	for _, login := range bots {
		if !isBot(login) {
			t.Errorf("isBot(%q) = false, want true", login)
		}
	}
	nonBots := []string{"alice", "bob", "octocat", ""}
	for _, login := range nonBots {
		if isBot(login) {
			t.Errorf("isBot(%q) = true, want false", login)
		}
	}
}

func TestBuildArtifactURL(t *testing.T) {
	cases := []struct {
		owner, repo, tag, want string
	}{
		{"user", "repo", "v1.0.0", "https://github.com/user/repo/releases/download/v1.0.0"},
		{"org", "project", "v2.0 beta", "https://github.com/org/project/releases/download/v2.0%20beta"},
	}
	for _, c := range cases {
		got := buildArtifactURL(c.owner, c.repo, c.tag)
		if got != c.want {
			t.Errorf("buildArtifactURL(%q, %q, %q) = %q, want %q", c.owner, c.repo, c.tag, got, c.want)
		}
	}
}

func TestForkOwnerLogin(t *testing.T) {
	cases := []struct {
		name string
		pr   ghPR
		want string
	}{
		{
			name: "with owner",
			pr:   ghPR{HeadRepositoryOwner: &ghRepoOwner{Login: "forker"}},
			want: "forker",
		},
		{
			name: "nil owner",
			pr:   ghPR{HeadRepositoryOwner: nil},
			want: "",
		},
		{
			name: "empty login",
			pr:   ghPR{HeadRepositoryOwner: &ghRepoOwner{Login: ""}},
			want: "",
		},
	}
	for _, c := range cases {
		got := forkOwnerLogin(c.pr)
		if got != c.want {
			t.Errorf("forkOwnerLogin(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}
