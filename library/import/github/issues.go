// issues.go - Fetch GitHub milestones and issues via gh CLI
package github

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/gitsocial-org/gitsocial/core/log"
	importpkg "github.com/gitsocial-org/gitsocial/import"
)

type ghMilestone struct {
	Title       string  `json:"title"`
	State       string  `json:"state"`
	Description string  `json:"description"`
	DueOn       *string `json:"dueOn"`
	Number      int     `json:"number"`
	Creator     *struct {
		Login string `json:"login"`
	} `json:"creator"`
	CreatedAt time.Time `json:"created_at"`
}

type ghIssue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	Author    ghAuthor  `json:"author"`
	Labels    []ghLabel `json:"labels"`
	Assignees []ghUser  `json:"assignees"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
	CreatedAt time.Time `json:"createdAt"`
	ClosedAt  time.Time `json:"closedAt"`
}

type ghAuthor struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghUser struct {
	Login string `json:"login"`
}

// FetchPM fetches milestones and issues from GitHub.
func (a *Adapter) FetchPM(opts importpkg.FetchOptions) (*importpkg.PMPlan, error) {
	milestones, err := a.fetchMilestones(opts)
	if err != nil {
		return nil, fmt.Errorf("fetch milestones: %w", err)
	}
	issues, filtered, err := a.fetchIssues(opts)
	if err != nil {
		return nil, fmt.Errorf("fetch issues: %w", err)
	}
	return &importpkg.PMPlan{Milestones: milestones, Issues: issues, Filtered: filtered}, nil
}

func (a *Adapter) fetchMilestones(opts importpkg.FetchOptions) ([]importpkg.ImportMilestone, error) {
	var raw []ghMilestone
	err := ghJSON(&raw, "api", fmt.Sprintf("repos/%s/milestones?state=all&per_page=100", a.repoSlug()))
	if err != nil {
		return nil, err
	}
	out := make([]importpkg.ImportMilestone, 0, len(raw))
	for _, m := range raw {
		if opts.SkipExternalIDs["milestone:"+m.Title] {
			continue
		}
		im := importpkg.ImportMilestone{
			ExternalID: m.Title,
			Number:     m.Number,
			Title:      m.Title,
			Body:       m.Description,
			State:      normalizeState(m.State),
			CreatedAt:  m.CreatedAt,
		}
		if m.Creator != nil {
			creator := a.resolveUser(m.Creator.Login)
			im.AuthorName = creator.name
			im.AuthorEmail = creator.email
		}
		if m.DueOn != nil {
			if t, err := time.Parse(time.RFC3339, *m.DueOn); err == nil {
				im.DueDate = &t
			}
		}
		out = append(out, im)
	}
	return out, nil
}

func (a *Adapter) fetchIssues(opts importpkg.FetchOptions) ([]importpkg.ImportIssue, int, error) {
	state := opts.State
	if state == "" || state == "all" {
		state = "all"
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 999999
	}
	var raw []ghIssue
	args := []string{
		"issue", "list",
		"--repo", a.repoSlug(),
		"--json", "number,title,body,state,author,labels,assignees,milestone,createdAt,closedAt",
		"--limit", fmt.Sprintf("%d", limit),
		"--state", state,
	}
	if err := ghJSON(&raw, args...); err != nil {
		return nil, 0, err
	}
	if opts.OnFetchProgress != nil {
		opts.OnFetchProgress(len(raw))
	}
	out := make([]importpkg.ImportIssue, 0, len(raw))
	var closedNumbers []int
	var filtered int
	for _, issue := range raw {
		if opts.SkipExternalIDs[fmt.Sprintf("issue:%d", issue.Number)] {
			continue
		}
		if opts.Since != nil && issue.CreatedAt.Before(*opts.Since) {
			filtered++
			continue
		}
		if opts.SkipBots && isBot(issue.Author.Login) {
			filtered++
			continue
		}
		labels := make([]string, 0, len(issue.Labels))
		for _, l := range issue.Labels {
			labels = append(labels, l.Name)
		}
		assignees := make([]string, 0, len(issue.Assignees))
		for _, u := range issue.Assignees {
			assignees = append(assignees, a.resolveUserEmail(u.Login))
		}
		author := a.resolveUser(issue.Author.Login)
		im := importpkg.ImportIssue{
			ExternalID:  fmt.Sprintf("%d", issue.Number),
			Number:      issue.Number,
			Title:       issue.Title,
			Body:        issue.Body,
			State:       normalizeState(issue.State),
			Labels:      labels,
			Assignees:   assignees,
			AuthorName:  author.name,
			AuthorEmail: author.email,
			CreatedAt:   issue.CreatedAt,
			ClosedAt:    issue.ClosedAt,
		}
		im.RelatedIDs = extractIssueRefs(issue.Body, issue.Number)
		if issue.Milestone != nil {
			im.MilestoneID = issue.Milestone.Title
		}
		if im.State == "closed" {
			closedNumbers = append(closedNumbers, issue.Number)
		}
		out = append(out, im)
	}
	// Batch fetch closed_by via GraphQL instead of N+1 REST calls
	if len(closedNumbers) > 0 {
		closedByMap := a.batchClosedBy(closedNumbers)
		for i := range out {
			if out[i].State == "closed" {
				if login, ok := closedByMap[out[i].Number]; ok && login != "" {
					closedBy := a.resolveUser(login)
					out[i].ClosedByName = closedBy.name
					out[i].ClosedByEmail = closedBy.email
				}
			}
		}
	}
	return out, filtered, nil
}

func normalizeState(state string) string {
	switch state {
	case "OPEN", "open":
		return "open"
	case "CLOSED", "closed":
		return "closed"
	case "MERGED", "merged":
		return "merged"
	default:
		return "open"
	}
}

var knownBots = map[string]bool{
	"dependabot":          true,
	"dependabot[bot]":     true,
	"renovate":            true,
	"renovate[bot]":       true,
	"github-actions":      true,
	"github-actions[bot]": true,
}

func isBot(login string) bool {
	return knownBots[login]
}

// batchClosedBy fetches closed_by info for multiple issues via GraphQL in batches of 50.
func (a *Adapter) batchClosedBy(issueNumbers []int) map[int]string {
	result := map[int]string{}
	for i := 0; i < len(issueNumbers); i += 50 {
		end := i + 50
		if end > len(issueNumbers) {
			end = len(issueNumbers)
		}
		batch := issueNumbers[i:end]
		var fields string
		for _, n := range batch {
			fields += fmt.Sprintf(`  i%d: issue(number: %d) { timelineItems(itemTypes: [CLOSED_EVENT], last: 1) { nodes { ... on ClosedEvent { actor { login } } } } }
`, n, n)
		}
		query := fmt.Sprintf("{ repository(owner: %q, name: %q) {\n%s} }", a.owner, a.repo, fields)
		var resp struct {
			Data struct {
				Repository map[string]json.RawMessage `json:"repository"`
			} `json:"data"`
		}
		if err := ghJSON(&resp, "api", "graphql", "-f", "query="+query); err != nil {
			log.Debug("graphql batch closed-by query failed", "error", err)
			continue
		}
		for _, n := range batch {
			key := fmt.Sprintf("i%d", n)
			raw, ok := resp.Data.Repository[key]
			if !ok {
				continue
			}
			var issue struct {
				TimelineItems struct {
					Nodes []struct {
						Actor *struct {
							Login string `json:"login"`
						} `json:"actor"`
					} `json:"nodes"`
				} `json:"timelineItems"`
			}
			if err := json.Unmarshal(raw, &issue); err != nil {
				log.Debug("failed to unmarshal issue closed-by data", "issue", n, "error", err)
				continue
			}
			if len(issue.TimelineItems.Nodes) > 0 && issue.TimelineItems.Nodes[0].Actor != nil {
				result[n] = issue.TimelineItems.Nodes[0].Actor.Login
			}
		}
	}
	return result
}

var issueRefRe = regexp.MustCompile(`(?:^|[^\w&])#(\d+)\b`)

// extractIssueRefs extracts #N cross-references from issue body, excluding self.
func extractIssueRefs(body string, selfNumber int) []string {
	matches := issueRefRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	self := fmt.Sprintf("%d", selfNumber)
	seen := map[string]bool{}
	var refs []string
	for _, m := range matches {
		num := m[1]
		if num == self || seen[num] {
			continue
		}
		seen[num] = true
		refs = append(refs, num)
	}
	return refs
}
