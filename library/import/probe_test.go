// probe_test.go - Tests for host resolution (no network: probing is not exercised)
package importpkg

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

func TestResolveHost(t *testing.T) {
	tests := []struct {
		name         string
		repoURL      string
		hostOverride string
		want         protocol.HostingService
		wantErr      bool
	}{
		{"override github", "https://example.com/a/b", "github", protocol.HostGitHub, false},
		{"override gitlab", "https://example.com/a/b", "gitlab", protocol.HostGitLab, false},
		{"override gitea", "https://example.com/a/b", "gitea", protocol.HostGitea, false},
		{"override bitbucket", "https://example.com/a/b", "bitbucket", protocol.HostBitbucket, false},
		{"unknown override", "https://example.com/a/b", "sourcehut", protocol.HostUnknown, true},
		{"detected github", "https://github.com/acme/widgets", "", protocol.HostGitHub, false},
		{"detected gitlab", "https://gitlab.com/acme/widgets", "", protocol.HostGitLab, false},
		{"s3 remote", "s3://bucket.example.com/repo", "", protocol.HostUnknown, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveHost(tt.repoURL, tt.hostOverride)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveHost(%q, %q) error = %v, wantErr %v", tt.repoURL, tt.hostOverride, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ResolveHost(%q, %q) = %v, want %v", tt.repoURL, tt.hostOverride, got, tt.want)
			}
		})
	}
}
