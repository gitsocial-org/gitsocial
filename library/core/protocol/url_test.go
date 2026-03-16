// url_test.go - Tests for URL normalization and host detection
package protocol

import (
	"testing"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "https with .git suffix",
			input: "https://github.com/user/repo.git",
			want:  "https://github.com/user/repo",
		},
		{
			name:  "ssh format",
			input: "git@github.com:user/repo.git",
			want:  "https://github.com/user/repo",
		},
		{
			name:  "ssh without .git",
			input: "git@github.com:user/repo",
			want:  "https://github.com/user/repo",
		},
		{
			name:  "mixed case hostname",
			input: "https://GitHub.com/User/Repo",
			want:  "https://github.com/User/Repo",
		},
		{
			name:  "trailing slash preserved in path",
			input: "https://github.com/user/repo",
			want:  "https://github.com/user/repo",
		},
		{
			name:  "already normalized",
			input: "https://github.com/user/repo",
			want:  "https://github.com/user/repo",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace trimmed",
			input: "  https://github.com/user/repo  ",
			want:  "https://github.com/user/repo",
		},
		{
			name:  "gitlab url",
			input: "https://gitlab.com/user/repo.git",
			want:  "https://gitlab.com/user/repo",
		},
		{
			name:  "bitbucket url",
			input: "https://bitbucket.org/user/repo.git",
			want:  "https://bitbucket.org/user/repo",
		},
		{
			name:  "ssh gitlab format",
			input: "git@gitlab.com:org/project.git",
			want:  "https://gitlab.com/org/project",
		},
		{
			name:  "host-only url",
			input: "https://GITHUB.COM",
			want:  "https://github.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeURL(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "github https",
			input: "https://github.com/user/repo",
			want:  "github.com",
		},
		{
			name:  "gitlab https",
			input: "https://gitlab.com/user/repo",
			want:  "gitlab.com",
		},
		{
			name:  "ssh format",
			input: "git@github.com:user/repo.git",
			want:  "github.com",
		},
		{
			name:  "custom domain",
			input: "https://git.mycompany.com/team/project",
			want:  "git.mycompany.com",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractDomain(tt.input)
			if got != tt.want {
				t.Errorf("ExtractDomain(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDetectHost(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  HostingService
	}{
		{"github", "https://github.com/user/repo", HostGitHub},
		{"gitlab", "https://gitlab.com/user/repo", HostGitLab},
		{"bitbucket", "https://bitbucket.org/user/repo", HostBitbucket},
		{"codeberg", "https://codeberg.org/user/repo", HostGitea},
		{"self-hosted gitlab", "https://gitlab.mycompany.com/user/repo", HostGitLab},
		{"self-hosted bitbucket", "https://bitbucket.mycompany.com/user/repo", HostBitbucket},
		{"self-hosted gitea", "https://gitea.myserver.com/user/repo", HostGitea},
		{"unknown returns unknown", "https://git.example.com/user/repo", HostUnknown},
		{"empty", "", HostUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectHost(tt.input)
			if got != tt.want {
				t.Errorf("DetectHost(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseRepo(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
		wantNil   bool
	}{
		{
			name:      "github https",
			input:     "https://github.com/user/repo",
			wantOwner: "user",
			wantRepo:  "repo",
		},
		{
			name:      "ssh format",
			input:     "git@github.com:org/project.git",
			wantOwner: "org",
			wantRepo:  "project",
		},
		{
			name:      "with .git suffix",
			input:     "https://github.com/user/repo.git",
			wantOwner: "user",
			wantRepo:  "repo",
		},
		{
			name:    "empty string",
			input:   "",
			wantNil: true,
		},
		{
			name:    "bare domain",
			input:   "https://github.com",
			wantNil: true,
		},
		{
			name:    "domain with single path",
			input:   "https://github.com/user",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseRepo(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Errorf("ParseRepo(%q) = %+v, want nil", tt.input, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ParseRepo(%q) = nil, want non-nil", tt.input)
			}
			if got.Owner != tt.wantOwner {
				t.Errorf("Owner = %q, want %q", got.Owner, tt.wantOwner)
			}
			if got.Repo != tt.wantRepo {
				t.Errorf("Repo = %q, want %q", got.Repo, tt.wantRepo)
			}
		})
	}
}

func TestGetDisplayName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"github url", "https://github.com/user/myrepo", "myrepo"},
		{"with hash fragment", "https://github.com/user/repo#branch:main", "repo"},
		{"ssh url", "git@github.com:org/project.git", "project"},
		{"invalid url returns raw", "not-a-url", "not-a-url"},
		{"bare domain returns raw", "https://github.com", "https://github.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetDisplayName(tt.input)
			if got != tt.want {
				t.Errorf("GetDisplayName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGetFullDisplayName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"github url", "https://github.com/user/repo", "user/repo"},
		{"with hash fragment", "https://github.com/org/proj#commit:abc123456789", "org/proj"},
		{"invalid url returns raw", "not-a-url", "not-a-url"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetFullDisplayName(tt.input)
			if got != tt.want {
				t.Errorf("GetFullDisplayName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBranchURL(t *testing.T) {
	tests := []struct {
		name    string
		repoURL string
		branch  string
		want    string
	}{
		{"github", "https://github.com/user/repo", "main", "https://github.com/user/repo/tree/main"},
		{"gitlab", "https://gitlab.com/user/repo", "develop", "https://gitlab.com/user/repo/-/tree/develop"},
		{"bitbucket", "https://bitbucket.org/user/repo", "main", "https://bitbucket.org/user/repo/src/main"},
		{"codeberg/gitea", "https://codeberg.org/user/repo", "main", "https://codeberg.org/user/repo/src/branch/main"},
		{"empty branch defaults to main", "https://github.com/user/repo", "", "https://github.com/user/repo/tree/main"},
		{"empty repo url", "", "main", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BranchURL(tt.repoURL, tt.branch)
			if got != tt.want {
				t.Errorf("BranchURL(%q, %q) = %q, want %q", tt.repoURL, tt.branch, got, tt.want)
			}
		})
	}
}

func TestCommitURL(t *testing.T) {
	tests := []struct {
		name    string
		repoURL string
		hash    string
		want    string
	}{
		{"github", "https://github.com/user/repo", "abc123", "https://github.com/user/repo/commit/abc123"},
		{"gitlab", "https://gitlab.com/user/repo", "abc123", "https://gitlab.com/user/repo/-/commit/abc123"},
		{"bitbucket", "https://bitbucket.org/user/repo", "abc123", "https://bitbucket.org/user/repo/commits/abc123"},
		{"empty hash", "https://github.com/user/repo", "", ""},
		{"empty repo url", "", "abc123", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CommitURL(tt.repoURL, tt.hash)
			if got != tt.want {
				t.Errorf("CommitURL(%q, %q) = %q, want %q", tt.repoURL, tt.hash, got, tt.want)
			}
		})
	}
}

func TestExtractDomain_malformedURL(t *testing.T) {
	got := ExtractDomain("://invalid")
	if got != "" {
		t.Errorf("ExtractDomain() = %q, want empty", got)
	}
}

func TestParseRepo_singlePathSegment(t *testing.T) {
	got := ParseRepo("https://github.com/solo")
	if got != nil {
		t.Errorf("ParseRepo() = %+v, want nil for single path segment", got)
	}
}
