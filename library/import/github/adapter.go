// adapter.go - GitHub source adapter using gh CLI
package github

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	importpkg "github.com/gitsocial-org/gitsocial/import"
)

// userProfile holds a resolved GitHub user's display name and email.
type userProfile struct {
	name  string
	email string
}

// Adapter implements SourceAdapter for GitHub repositories.
type Adapter struct {
	owner          string
	repo           string
	userProfiles   map[string]userProfile // login -> resolved profile cache
	emailOverrides map[string]string      // login -> email override
}

// New creates a GitHub adapter for the given owner/repo.
func New(owner, repo string) *Adapter {
	return &Adapter{owner: owner, repo: repo}
}

// Platform returns the platform identifier.
func (a *Adapter) Platform() string { return "github" }

func (a *Adapter) repoSlug() string { return a.owner + "/" + a.repo }

// FetchSocial fetches GitHub Discussions as social posts with comments.
func (a *Adapter) FetchSocial(opts importpkg.FetchOptions) (*importpkg.SocialPlan, error) {
	return a.fetchDiscussions(opts)
}

// gh executes a gh CLI command with retry on rate limit / server errors.
func gh(args ...string) ([]byte, error) {
	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		cmd := exec.Command("gh", args...)
		out, err := cmd.Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				stderr := string(exitErr.Stderr)
				if attempt < maxRetries && isRetryableError(stderr) {
					wait := time.Duration(1<<uint(attempt)) * time.Second
					time.Sleep(wait)
					continue
				}
				return nil, fmt.Errorf("gh %s: %s", strings.Join(args, " "), stderr)
			}
			return nil, fmt.Errorf("gh %s: %w", strings.Join(args, " "), err)
		}
		return out, nil
	}
	return nil, fmt.Errorf("gh %s: exhausted retries", strings.Join(args, " "))
}

// isRetryableError checks if a gh CLI error message indicates a retryable condition.
func isRetryableError(stderr string) bool {
	lower := strings.ToLower(stderr)
	return strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "abuse detection") ||
		strings.Contains(lower, "502") ||
		strings.Contains(lower, "503") ||
		strings.Contains(lower, "secondary rate")
}

// ghJSON executes gh and unmarshals JSON output into dst.
func ghJSON(dst interface{}, args ...string) error {
	out, err := gh(args...)
	if err != nil {
		return err
	}
	if len(out) == 0 {
		return nil
	}
	return json.Unmarshal(out, dst)
}

// loginToEmail converts a GitHub login to a noreply email address.
func loginToEmail(login string) string {
	if login == "" {
		return ""
	}
	return login + "@users.noreply.github.com"
}

// loginToName converts a GitHub login to a fallback display name.
func loginToName(login string) string {
	if login == "" {
		return ""
	}
	return "@" + login
}

// SetUserEmails sets email overrides for logins (highest priority in resolveUser).
func (a *Adapter) SetUserEmails(emails map[string]string) {
	a.emailOverrides = emails
}

// resolveUser looks up a GitHub user's profile via the API, returning both name and email.
// Makes one API call per unique login and caches the result. Falls back to @login / noreply email.
func (a *Adapter) resolveUser(login string) userProfile {
	if login == "" {
		return userProfile{}
	}
	if a.userProfiles == nil {
		a.userProfiles = map[string]userProfile{}
	}
	if p, ok := a.userProfiles[login]; ok {
		return p
	}
	var user struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	p := userProfile{name: loginToName(login), email: loginToEmail(login)}
	if err := ghJSON(&user, "api", "users/"+login); err == nil {
		if user.Name != "" {
			p.name = user.Name
		}
		if user.Email != "" {
			p.email = user.Email
		}
	}
	if override, ok := a.emailOverrides[login]; ok {
		p.email = override
	}
	a.userProfiles[login] = p
	return p
}

// resolveUserEmail returns the resolved email for a GitHub login.
func (a *Adapter) resolveUserEmail(login string) string {
	return a.resolveUser(login).email
}

// CountItems returns exact totals for each importable type via GraphQL + REST.
func (a *Adapter) CountItems(opts importpkg.FetchOptions) (importpkg.ItemCounts, error) {
	counts := importpkg.ItemCounts{Issues: -1, PRs: -1, Releases: -1, Discussions: -1}
	state := opts.State
	if state == "" || state == "all" {
		state = ""
	}
	// Issues + PRs + Discussions via single GraphQL query
	var issueFilter, prFilter string
	switch state {
	case "open":
		issueFilter = "(states: OPEN)"
		prFilter = "(states: OPEN)"
	case "closed":
		issueFilter = "(states: CLOSED)"
		prFilter = "(states: CLOSED)"
	case "merged":
		prFilter = "(states: MERGED)"
	}
	var fields []string
	if state != "merged" {
		fields = append(fields, fmt.Sprintf("issues%s { totalCount }", issueFilter))
	}
	fields = append(fields, fmt.Sprintf("pullRequests%s { totalCount }", prFilter))
	fields = append(fields, "discussions { totalCount }")
	query := fmt.Sprintf(`{ repository(owner: %q, name: %q) { %s } }`,
		a.owner, a.repo, strings.Join(fields, " "))
	var resp struct {
		Data struct {
			Repository struct {
				Issues       struct{ TotalCount int } `json:"issues"`
				PullRequests struct{ TotalCount int } `json:"pullRequests"`
				Discussions  struct{ TotalCount int } `json:"discussions"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := ghJSON(&resp, "api", "graphql", "-f", "query="+query); err == nil {
		repo := resp.Data.Repository
		if state != "merged" {
			counts.Issues = repo.Issues.TotalCount
		}
		counts.PRs = repo.PullRequests.TotalCount
		counts.Discussions = repo.Discussions.TotalCount
	}
	// Releases via REST (GraphQL can't filter out drafts)
	var allReleases []struct {
		Draft bool `json:"draft"`
	}
	if err := ghJSON(&allReleases, "api", "--paginate", fmt.Sprintf("repos/%s/releases?per_page=100", a.repoSlug())); err == nil {
		n := 0
		for _, r := range allReleases {
			if !r.Draft {
				n++
			}
		}
		counts.Releases = n
	}
	return counts, nil
}

// CheckGH verifies that gh CLI is installed and authenticated.
func CheckGH() error {
	_, err := exec.LookPath("gh")
	if err != nil {
		return fmt.Errorf("gh CLI not found — install from https://cli.github.com")
	}
	if _, err := gh("auth", "status"); err != nil {
		return fmt.Errorf("gh not authenticated — run: gh auth login")
	}
	return nil
}
