// forge_test.go - Tests for forge interface helpers
package forge

import "testing"

func TestParseRepoURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		host    string
		owner   string
		repo    string
		wantErr bool
	}{
		{"https basic", "https://github.com/alice/foo", "github.com", "alice", "foo", false},
		{"https with .git", "https://github.com/alice/foo.git", "github.com", "alice", "foo", false},
		{"http allowed", "http://gitlab.example.com/alice/foo", "gitlab.example.com", "alice", "foo", false},
		{"nested path ignored beyond owner/repo", "https://gitlab.com/group/project/extra", "gitlab.com", "group", "project", false},
		{"ssh basic", "git@github.com:alice/foo", "github.com", "alice", "foo", false},
		{"ssh with .git", "git@github.com:alice/foo.git", "github.com", "alice", "foo", false},
		{"empty", "", "", "", "", true},
		{"ssh missing repo", "git@github.com:alice", "", "", "", true},
		{"https missing repo", "https://github.com/alice", "", "", "", true},
		{"malformed ssh", "git@nocolon", "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			host, owner, repo, err := ParseRepoURL(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseRepoURL(%q) err = nil, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRepoURL(%q) err = %v", tt.input, err)
			}
			if host != tt.host || owner != tt.owner || repo != tt.repo {
				t.Errorf("ParseRepoURL(%q) = (%q, %q, %q); want (%q, %q, %q)",
					tt.input, host, owner, repo, tt.host, tt.owner, tt.repo)
			}
		})
	}
}
