// probe.go - Detect platform type for unknown hosts
package importpkg

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gitsocial-org/gitsocial/core/protocol"
)

var probeClient = &http.Client{Timeout: 5 * time.Second}

// ProbeHost attempts to identify the platform of an unknown host by checking API endpoints.
func ProbeHost(baseURL string) protocol.HostingService {
	domain := protocol.ExtractDomain(baseURL)
	if domain == "" {
		return protocol.HostUnknown
	}
	scheme := "https://" + domain
	// Gitea/Forgejo: GET /api/v1/version
	if resp, err := probeClient.Get(scheme + "/api/v1/version"); err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return protocol.HostGitea
		}
	}
	// GitLab: GET /api/v4/version
	if resp, err := probeClient.Get(scheme + "/api/v4/version"); err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
			return protocol.HostGitLab
		}
	}
	return protocol.HostUnknown
}

// ResolveHost detects the host, falling back to probe and manual override.
func ResolveHost(repoURL, hostOverride string) (protocol.HostingService, error) {
	if hostOverride != "" {
		switch hostOverride {
		case "github":
			return protocol.HostGitHub, nil
		case "gitlab":
			return protocol.HostGitLab, nil
		case "gitea":
			return protocol.HostGitea, nil
		case "bitbucket":
			return protocol.HostBitbucket, nil
		default:
			return protocol.HostUnknown, fmt.Errorf("unknown host type: %s", hostOverride)
		}
	}
	host := protocol.DetectHost(repoURL)
	if host != protocol.HostUnknown {
		return host, nil
	}
	probed := ProbeHost(repoURL)
	if probed != protocol.HostUnknown {
		return probed, nil
	}
	return protocol.HostUnknown, fmt.Errorf("could not detect platform for %s — use --host to specify", repoURL)
}
