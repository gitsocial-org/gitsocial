// pulls.go - Fetch GitHub pull requests via gh CLI
package github

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gitsocial-org/gitsocial/core/log"
	importpkg "github.com/gitsocial-org/gitsocial/import"
)

type ghOid struct {
	Oid string `json:"oid"`
}

type ghReviewRequest struct {
	Login string `json:"login"` // for user reviewers
	Name  string `json:"name"`  // for team reviewers
	Slug  string `json:"slug"`  // team slug
}

type ghPR struct {
	Number              int               `json:"number"`
	Title               string            `json:"title"`
	Body                string            `json:"body"`
	State               string            `json:"state"`
	IsDraft             bool              `json:"isDraft"`
	Author              ghAuthor          `json:"author"`
	Labels              []ghLabel         `json:"labels"`
	BaseRefName         string            `json:"baseRefName"`
	HeadRefName         string            `json:"headRefName"`
	HeadRepository      *ghRepo           `json:"headRepository"`
	HeadRepositoryOwner *ghRepoOwner      `json:"headRepositoryOwner"`
	MergeCommit         *ghOid            `json:"mergeCommit"`
	HeadRefOid          string            `json:"headRefOid"`
	ReviewRequests      []ghReviewRequest `json:"reviewRequests"`
	CreatedAt           time.Time         `json:"createdAt"`
	MergedBy            *ghAuthor         `json:"mergedBy"`
	MergedAt            time.Time         `json:"mergedAt"`
	ClosedAt            time.Time         `json:"closedAt"`
}

type ghRepo struct {
	Name string `json:"name"`
}

type ghRepoOwner struct {
	Login string `json:"login"`
}

// FetchReview fetches pull requests from GitHub, detecting forks.
func (a *Adapter) FetchReview(opts importpkg.FetchOptions) (*importpkg.ReviewPlan, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 999999
	}
	state := opts.State
	if state == "" || state == "all" {
		state = "all"
	}
	var raw []ghPR
	args := []string{
		"pr", "list",
		"--repo", a.repoSlug(),
		"--json", "number,title,body,state,isDraft,author,labels,baseRefName,headRefName,headRepository,headRepositoryOwner,mergeCommit,headRefOid,reviewRequests,createdAt,mergedBy,mergedAt,closedAt",
		"--limit", fmt.Sprintf("%d", limit),
		"--state", state,
	}
	if err := ghJSON(&raw, args...); err != nil {
		return nil, fmt.Errorf("fetch pull requests: %w", err)
	}
	if opts.OnFetchProgress != nil {
		opts.OnFetchProgress(len(raw))
	}
	forkSet := map[string]bool{}
	prs := make([]importpkg.ImportPR, 0, len(raw))
	var closedNotMergedNumbers []int
	var filtered int
	for _, pr := range raw {
		if opts.SkipExternalIDs[fmt.Sprintf("pr:%d", pr.Number)] {
			continue
		}
		if opts.Since != nil && pr.CreatedAt.Before(*opts.Since) {
			filtered++
			continue
		}
		if opts.SkipBots && isBot(pr.Author.Login) {
			filtered++
			continue
		}
		labels := make([]string, 0, len(pr.Labels))
		for _, l := range pr.Labels {
			labels = append(labels, l.Name)
		}
		reviewers := make([]string, 0, len(pr.ReviewRequests))
		for _, r := range pr.ReviewRequests {
			if r.Login != "" {
				reviewers = append(reviewers, a.resolveUserEmail(r.Login))
			} else if r.Slug != "" {
				log.Debug("skipping team reviewer (not resolvable to email)", "team", r.Slug)
			}
		}
		author := a.resolveUser(pr.Author.Login)
		imp := importpkg.ImportPR{
			ExternalID:  fmt.Sprintf("%d", pr.Number),
			Number:      pr.Number,
			Title:       pr.Title,
			Body:        pr.Body,
			State:       normalizeState(pr.State),
			IsDraft:     pr.IsDraft,
			BaseBranch:  pr.BaseRefName,
			HeadBranch:  pr.HeadRefName,
			Labels:      labels,
			Reviewers:   reviewers,
			AuthorName:  author.name,
			AuthorEmail: author.email,
			CreatedAt:   pr.CreatedAt,
			MergedAt:    pr.MergedAt,
			ClosedAt:    pr.ClosedAt,
		}
		imp.HeadSHA = pr.HeadRefOid
		if pr.MergedBy != nil {
			merged := a.resolveUser(pr.MergedBy.Login)
			imp.MergedByName = merged.name
			imp.MergedByEmail = merged.email
		}
		if imp.State == "closed" && pr.MergedBy == nil {
			closedNotMergedNumbers = append(closedNotMergedNumbers, pr.Number)
		}
		if pr.MergeCommit != nil {
			imp.MergeCommit = pr.MergeCommit.Oid
		}
		forkOwner := forkOwnerLogin(pr)
		if forkOwner != "" && forkOwner != a.owner {
			repoName := a.repo
			if pr.HeadRepository != nil && pr.HeadRepository.Name != "" {
				repoName = pr.HeadRepository.Name
			}
			forkURL := fmt.Sprintf("https://github.com/%s/%s", forkOwner, repoName)
			imp.HeadRepo = forkURL
			forkSet[forkURL] = true
		}
		prs = append(prs, imp)
	}
	// Batch fetch closed_by for closed-not-merged PRs via GraphQL
	if len(closedNotMergedNumbers) > 0 {
		closedByMap := a.batchPRClosedBy(closedNotMergedNumbers)
		for i := range prs {
			if prs[i].State == "closed" && prs[i].MergedByName == "" {
				if login, ok := closedByMap[prs[i].Number]; ok && login != "" {
					closedBy := a.resolveUser(login)
					prs[i].ClosedByName = closedBy.name
					prs[i].ClosedByEmail = closedBy.email
				}
			}
		}
	}
	forks := make([]string, 0, len(forkSet))
	for url := range forkSet {
		forks = append(forks, url)
	}
	return &importpkg.ReviewPlan{Forks: forks, PRs: prs, Filtered: filtered}, nil
}

// batchPRClosedBy fetches closed_by info for multiple PRs via GraphQL in batches of 50.
func (a *Adapter) batchPRClosedBy(prNumbers []int) map[int]string {
	result := map[int]string{}
	for i := 0; i < len(prNumbers); i += 50 {
		end := i + 50
		if end > len(prNumbers) {
			end = len(prNumbers)
		}
		batch := prNumbers[i:end]
		var fields string
		for _, n := range batch {
			fields += fmt.Sprintf(`  p%d: pullRequest(number: %d) { timelineItems(itemTypes: [CLOSED_EVENT], last: 1) { nodes { ... on ClosedEvent { actor { login } } } } }
`, n, n)
		}
		query := fmt.Sprintf("{ repository(owner: %q, name: %q) {\n%s} }", a.owner, a.repo, fields)
		var resp struct {
			Data struct {
				Repository map[string]json.RawMessage `json:"repository"`
			} `json:"data"`
		}
		if err := ghJSON(&resp, "api", "graphql", "-f", "query="+query); err != nil {
			log.Debug("graphql batch PR closed-by query failed", "error", err)
			continue
		}
		for _, n := range batch {
			key := fmt.Sprintf("p%d", n)
			raw, ok := resp.Data.Repository[key]
			if !ok {
				continue
			}
			var pr struct {
				TimelineItems struct {
					Nodes []struct {
						Actor *struct {
							Login string `json:"login"`
						} `json:"actor"`
					} `json:"nodes"`
				} `json:"timelineItems"`
			}
			if err := json.Unmarshal(raw, &pr); err != nil {
				log.Debug("failed to unmarshal PR closed-by data", "pr", n, "error", err)
				continue
			}
			if len(pr.TimelineItems.Nodes) > 0 && pr.TimelineItems.Nodes[0].Actor != nil {
				result[n] = pr.TimelineItems.Nodes[0].Actor.Login
			}
		}
	}
	return result
}

// forkOwnerLogin extracts the fork owner from the PR, preferring headRepositoryOwner.
func forkOwnerLogin(pr ghPR) string {
	if pr.HeadRepositoryOwner != nil && pr.HeadRepositoryOwner.Login != "" {
		return pr.HeadRepositoryOwner.Login
	}
	return ""
}
