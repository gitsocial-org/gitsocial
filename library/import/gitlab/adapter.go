// adapter.go - GitLab source adapter using REST API v4
package gitlab

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	importpkg "github.com/gitsocial-org/gitsocial/library/import"
)

type userProfile struct {
	name  string
	email string
}

// AdapterOptions configures the GitLab adapter.
type AdapterOptions struct {
	BaseURL string // e.g. "https://gitlab.example.com" (default: https://gitlab.com)
	Token   string
}

// OptionsForRepo builds adapter options for a repository URL, an explicit base URL winning over it.
func OptionsForRepo(repoURL, baseURL, token string) AdapterOptions {
	if baseURL == "" {
		baseURL = instanceURL(repoURL)
	}
	return AdapterOptions{BaseURL: baseURL, Token: token}
}

// instanceURL returns the scheme and host of a repository URL, empty when it carries neither.
func instanceURL(repoURL string) string {
	parsed, err := url.Parse(repoURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// Adapter implements SourceAdapter for GitLab repositories.
type Adapter struct {
	owner          string
	repo           string
	baseURL        string
	token          string
	domain         string
	userProfiles   map[string]userProfile
	projectCache   map[int]string // project ID -> web_url
	httpClient     *http.Client
	emailOverrides map[string]string // username -> email override
	projectID      int               // numeric project ID (cached)
}

// New creates a GitLab adapter for the given owner/repo.
func New(owner, repo string, opts AdapterOptions) *Adapter {
	base := strings.TrimSuffix(opts.BaseURL, "/")
	if base == "" {
		base = "https://gitlab.com"
	}
	domain := "gitlab.com"
	if parsed, err := url.Parse(base); err == nil && parsed.Host != "" {
		domain = parsed.Host
	}
	token := resolveToken(opts.Token)
	return &Adapter{
		owner:        owner,
		repo:         repo,
		baseURL:      base,
		token:        token,
		domain:       domain,
		userProfiles: map[string]userProfile{},
		projectCache: map[int]string{},
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Platform returns the platform identifier.
func (a *Adapter) Platform() string { return "gitlab" }

// projectPath returns the URL-encoded project path for API calls.
func (a *Adapter) projectPath() string {
	return url.PathEscape(a.owner + "/" + a.repo)
}

// doWithRetry executes an HTTP request with retry on 429/5xx, respecting Retry-After.
func (a *Adapter) doWithRetry(req *http.Request) (*http.Response, error) {
	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err := a.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			resp.Body.Close()
			if attempt == maxRetries {
				return nil, fmt.Errorf("GET %s: %d after %d retries", req.URL.Path, resp.StatusCode, maxRetries)
			}
			wait := time.Duration(1<<uint(attempt)) * time.Second
			if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
				if secs, err := strconv.Atoi(retryAfter); err == nil && secs > 0 {
					wait = time.Duration(secs) * time.Second
				}
			}
			time.Sleep(wait)
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("GET %s: exhausted retries", req.URL.Path)
}

// apiGet performs a GET request to the GitLab API and unmarshals JSON into dst.
func (a *Adapter) apiGet(path string, dst interface{}) error {
	apiURL := a.baseURL + "/api/v4/" + path
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if a.token != "" {
		req.Header.Set("PRIVATE-TOKEN", a.token)
	}
	resp, err := a.doWithRetry(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: %d %s", path, resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// apiGetPage performs a single page GET and returns the path of the next page, empty at the last one.
func (a *Adapter) apiGetPage(path string, dst interface{}) (string, error) {
	apiURL := a.baseURL + "/api/v4/" + path
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	if a.token != "" {
		req.Header.Set("PRIVATE-TOKEN", a.token)
	}
	resp, err := a.doWithRetry(req)
	if err != nil {
		return "", fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GET %s: %d %s", path, resp.StatusCode, string(body))
	}
	return nextPagePath(path, resp.Header), json.NewDecoder(resp.Body).Decode(dst)
}

// apiV4Prefix is where an absolute GitLab API URL turns into the path apiGetPage takes.
const apiV4Prefix = "/api/v4/"

// nextPagePath returns the next page's API path, from X-Next-Page or from a Link header.
func nextPagePath(path string, header http.Header) string {
	if next := header.Get("X-Next-Page"); next != "" {
		return withPage(path, next)
	}
	return linkNextPath(header.Get("Link"))
}

// withPage returns the path with its page query parameter set to n.
func withPage(path, n string) string {
	parsed, err := url.Parse(path)
	if err != nil {
		return path
	}
	query := parsed.Query()
	query.Set("page", n)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// linkNextPath returns the API path of a Link header's rel="next" entry, which keyset pagination uses alone.
func linkNextPath(header string) string {
	for _, entry := range strings.Split(header, ",") {
		target, _, found := strings.Cut(entry, ";")
		if !found || !strings.Contains(entry, `rel="next"`) {
			continue
		}
		target = strings.Trim(strings.TrimSpace(target), "<>")
		if idx := strings.Index(target, apiV4Prefix); idx != -1 {
			return target[idx+len(apiV4Prefix):]
		}
	}
	return ""
}

// apiGetTotal makes a per_page=1 request and reads X-Total header for count.
func (a *Adapter) apiGetTotal(path string) (int, error) {
	apiURL := a.baseURL + "/api/v4/" + path
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	if a.token != "" {
		req.Header.Set("PRIVATE-TOKEN", a.token)
	}
	resp, err := a.doWithRetry(req)
	if err != nil {
		return 0, fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GET %s: %d", path, resp.StatusCode)
	}
	totalStr := resp.Header.Get("X-Total")
	if totalStr == "" {
		return -1, nil
	}
	var total int
	if _, err := fmt.Sscanf(totalStr, "%d", &total); err != nil {
		return -1, nil
	}
	return total, nil
}

// CountItems returns estimated totals for each importable type using lightweight API calls.
func (a *Adapter) CountItems(opts importpkg.FetchOptions) (importpkg.ItemCounts, error) {
	counts := importpkg.ItemCounts{Discussions: -1} // GitLab has no discussions
	state := mapIssueState(opts.State)
	issuePath := fmt.Sprintf("projects/%s/issues?state=%s&per_page=1", a.projectPath(), url.QueryEscape(state))
	if n, err := a.apiGetTotal(issuePath); err == nil {
		counts.Issues = n
	} else {
		counts.Issues = -1
	}
	mrState := mapMRState(opts.State)
	mrPath := fmt.Sprintf("projects/%s/merge_requests?state=%s&per_page=1", a.projectPath(), url.QueryEscape(mrState))
	if n, err := a.apiGetTotal(mrPath); err == nil {
		counts.PRs = n
	} else {
		counts.PRs = -1
	}
	relPath := fmt.Sprintf("projects/%s/releases?per_page=1", a.projectPath())
	if n, err := a.apiGetTotal(relPath); err == nil {
		counts.Releases = n
	} else {
		counts.Releases = -1
	}
	return counts, nil
}

// SetUserEmails sets email overrides for usernames (highest priority in resolveUser).
func (a *Adapter) SetUserEmails(emails map[string]string) {
	a.emailOverrides = emails
}

// resolveUser looks up a GitLab user by username and caches the result.
func (a *Adapter) resolveUser(username string) userProfile {
	if username == "" {
		return userProfile{}
	}
	if p, ok := a.userProfiles[username]; ok {
		return p
	}
	p := userProfile{name: "@" + username, email: username + "@users.noreply." + a.domain}
	var users []struct {
		Name        string `json:"name"`
		PublicEmail string `json:"public_email"`
		Email       string `json:"email"`
		CommitEmail string `json:"commit_email"`
	}
	if err := a.apiGet("users?username="+url.QueryEscape(username), &users); err == nil && len(users) > 0 {
		if users[0].Name != "" {
			p.name = users[0].Name
		}
		if users[0].PublicEmail != "" {
			p.email = users[0].PublicEmail
		} else if users[0].CommitEmail != "" {
			p.email = users[0].CommitEmail
		} else if users[0].Email != "" {
			p.email = users[0].Email
		}
	}
	if override, ok := a.emailOverrides[username]; ok {
		p.email = override
	}
	a.userProfiles[username] = p
	return p
}

// resolveProjectURL fetches a project's web_url by ID.
func (a *Adapter) resolveProjectURL(projectID int) string {
	if u, ok := a.projectCache[projectID]; ok {
		return u
	}
	var project struct {
		WebURL string `json:"web_url"`
	}
	if err := a.apiGet(fmt.Sprintf("projects/%d", projectID), &project); err == nil && project.WebURL != "" {
		a.projectCache[projectID] = project.WebURL
		return project.WebURL
	}
	return ""
}

// getProjectID fetches and caches the numeric project ID.
func (a *Adapter) getProjectID() int {
	if a.projectID != 0 {
		return a.projectID
	}
	var project struct {
		ID int `json:"id"`
	}
	if err := a.apiGet(fmt.Sprintf("projects/%s", a.projectPath()), &project); err == nil && project.ID != 0 {
		a.projectID = project.ID
	}
	return a.projectID
}

// PlatformMeta returns platform-specific metadata for storage in the cache.
func (a *Adapter) PlatformMeta() map[string]string {
	pid := a.getProjectID()
	if pid == 0 {
		return nil
	}
	return map[string]string{"platform_project_id": fmt.Sprintf("%d", pid)}
}

// resolveToken finds a token from explicit value, env vars, or glab config.
func resolveToken(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if t := os.Getenv("GITLAB_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("GITLAB_PRIVATE_TOKEN"); t != "" {
		return t
	}
	return ""
}

// graphqlQuery performs a GraphQL query against the GitLab API.
func (a *Adapter) graphqlQuery(query string, variables map[string]interface{}, dst interface{}) error {
	body := map[string]interface{}{"query": query}
	if variables != nil {
		body["variables"] = variables
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal graphql query: %w", err)
	}
	req, err := http.NewRequest("POST", a.baseURL+"/api/graphql", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create graphql request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if a.token != "" {
		req.Header.Set("PRIVATE-TOKEN", a.token)
	}
	resp, err := a.doWithRetry(req)
	if err != nil {
		return fmt.Errorf("graphql request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("graphql: %d %s", resp.StatusCode, string(respBody))
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode graphql response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("graphql error: %s", envelope.Errors[0].Message)
	}
	return json.Unmarshal(envelope.Data, dst)
}

var knownBots = map[string]bool{
	"renovate[bot]":   true,
	"dependabot[bot]": true,
	"gitlab-bot":      true,
}

// isBot checks if a username belongs to a bot account.
func isBot(username string, botFlag bool) bool {
	if botFlag {
		return true
	}
	if knownBots[username] {
		return true
	}
	if strings.HasPrefix(username, "project_") && strings.Contains(username, "_bot") {
		return true
	}
	return false
}
