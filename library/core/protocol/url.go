// url.go - Repository URL normalization and display name extraction
package protocol

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

type HostingService string

const (
	HostGitHub    HostingService = "github"
	HostGitLab    HostingService = "gitlab"
	HostBitbucket HostingService = "bitbucket"
	HostGitea     HostingService = "gitea"
	HostUnknown   HostingService = "unknown"
)

var knownProviders = map[string]HostingService{
	"github.com":    HostGitHub,
	"gitlab.com":    HostGitLab,
	"bitbucket.org": HostBitbucket,
	"codeberg.org":  HostGitea,
}

type RepoInfo struct {
	Owner string
	Repo  string
}

var sshPattern = regexp.MustCompile(`^git@([^:]+):([^/]+)/(.+)`)

// NormalizeURL returns a repository URL's canonical identity: one repository has one identity.
// s3 URLs fold through the grammar in s3.go. NormalizeURL(NormalizeURL(x)) == NormalizeURL(x).
func NormalizeURL(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}
	normalized := strings.TrimSpace(rawURL)
	if strings.HasPrefix(strings.ToLower(normalized), "s3://") {
		return canonicalS3URL(normalized)
	}

	if strings.HasPrefix(normalized, "git@") {
		normalized = sshPattern.ReplaceAllString(normalized, "https://$1/$2/$3")
	}

	idx := strings.Index(normalized, "://")
	if idx == -1 {
		return trimRepoSuffixes(normalized)
	}
	scheme := strings.ToLower(normalized[:idx])
	switch scheme {
	case "javascript", "data", "vbscript", "ftp":
		return ""
	}
	rest := normalized[idx+3:]
	slashIdx := strings.Index(rest, "/")
	if slashIdx == -1 {
		return scheme + "://" + trimRepoSuffixes(strings.ToLower(rest))
	}
	return scheme + "://" + strings.ToLower(rest[:slashIdx]) + trimRepoSuffixes(rest[slashIdx:])
}

// trimRepoSuffixes drops trailing whitespace, slashes and .git suffixes until none is left.
func trimRepoSuffixes(path string) string {
	for {
		trimmed := strings.TrimRight(strings.TrimRightFunc(path, unicode.IsSpace), "/")
		trimmed = strings.TrimSuffix(trimmed, ".git")
		if trimmed == path {
			return path
		}
		path = trimmed
	}
}

// ExtractDomain extracts the domain from a repository URL.
func ExtractDomain(rawURL string) string {
	normalized := NormalizeURL(rawURL)
	if normalized == "" {
		return ""
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Host)
}

// DetectHost identifies the git hosting service from a URL.
func DetectHost(rawURL string) HostingService {
	domain := ExtractDomain(rawURL)
	if domain == "" {
		return HostUnknown
	}
	if provider, ok := knownProviders[domain]; ok {
		return provider
	}
	switch {
	case strings.Contains(domain, "gitlab"):
		return HostGitLab
	case strings.Contains(domain, "bitbucket"):
		return HostBitbucket
	case strings.Contains(domain, "gitea"), strings.Contains(domain, "codeberg"):
		return HostGitea
	default:
		return HostUnknown
	}
}

// ParseRepo extracts owner and repo name from a URL. For s3:// remotes the
// bucket stands in for the owner and the last prefix segment for the repo
// (config query params are not part of the name).
func ParseRepo(rawURL string) *RepoInfo {
	normalized := NormalizeURL(rawURL)
	if normalized == "" {
		return nil
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return nil
	}
	if parsed.Scheme == "s3" {
		// Canonical host form: the bucket is the first path segment.
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if segments[0] == "" {
			return nil
		}
		return &RepoInfo{Owner: segments[0], Repo: segments[len(segments)-1]}
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return nil
	}
	return &RepoInfo{Owner: parts[0], Repo: parts[1]}
}

// GetDisplayName returns the repository name for display.
func GetDisplayName(rawURL string) string {
	url := rawURL
	if idx := strings.Index(url, "#"); idx != -1 {
		url = url[:idx]
	}
	info := ParseRepo(url)
	if info == nil {
		return rawURL
	}
	return info.Repo
}

// GetFullDisplayName returns owner/repo for display.
func GetFullDisplayName(rawURL string) string {
	url := rawURL
	if idx := strings.Index(url, "#"); idx != -1 {
		url = url[:idx]
	}
	info := ParseRepo(url)
	if info == nil {
		return rawURL
	}
	return info.Owner + "/" + info.Repo
}

// BranchURL generates host-specific branch/tree URL for web viewing
func BranchURL(repoURL, branch string) string {
	baseURL := NormalizeURL(repoURL)
	if baseURL == "" {
		return ""
	}
	if branch == "" {
		branch = "main"
	}
	switch DetectHost(repoURL) {
	case HostGitLab:
		return baseURL + "/-/tree/" + branch
	case HostBitbucket:
		return baseURL + "/src/" + branch
	case HostGitea:
		return baseURL + "/src/branch/" + branch
	default:
		return baseURL + "/tree/" + branch
	}
}

// FileURL generates a host-specific URL for viewing a file in the web UI.
func FileURL(repoURL, branch, filePath string) string {
	baseURL := NormalizeURL(repoURL)
	if baseURL == "" || filePath == "" {
		return ""
	}
	if branch == "" {
		branch = "HEAD"
	}
	filePath = strings.TrimPrefix(filePath, "/")
	switch DetectHost(repoURL) {
	case HostGitHub:
		return baseURL + "/raw/" + branch + "/" + filePath
	case HostGitLab:
		return baseURL + "/-/blob/" + branch + "/" + filePath
	case HostBitbucket:
		return baseURL + "/raw/" + branch + "/" + filePath
	case HostGitea:
		return baseURL + "/raw/branch/" + branch + "/" + filePath
	default:
		return baseURL + "/raw/" + branch + "/" + filePath
	}
}

// CommitURL generates host-specific commit URL for web viewing
func CommitURL(repoURL, hash string) string {
	if hash == "" {
		return ""
	}
	baseURL := NormalizeURL(repoURL)
	if baseURL == "" {
		return ""
	}
	switch DetectHost(repoURL) {
	case HostGitLab:
		return baseURL + "/-/commit/" + hash
	case HostBitbucket:
		return baseURL + "/commits/" + hash
	default:
		return baseURL + "/commit/" + hash
	}
}
