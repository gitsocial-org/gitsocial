// mergerequests.go - Fetch GitLab merge requests via REST API
package gitlab

import (
	"fmt"
	"net/url"
	"time"

	importpkg "github.com/gitsocial-org/gitsocial/import"
)

type glDiffRefs struct {
	BaseSHA string `json:"base_sha"`
}

type glMergeRequest struct {
	IID             int         `json:"iid"`
	Title           string      `json:"title"`
	Description     string      `json:"description"`
	State           string      `json:"state"`
	Draft           bool        `json:"draft"`
	SourceBranch    string      `json:"source_branch"`
	TargetBranch    string      `json:"target_branch"`
	SourceProjectID int         `json:"source_project_id"`
	TargetProjectID int         `json:"target_project_id"`
	SHA             string      `json:"sha"`
	MergeCommitSHA  *string     `json:"merge_commit_sha"`
	DiffRefs        *glDiffRefs `json:"diff_refs"`
	Labels          []string    `json:"labels"`
	Author          glUser      `json:"author"`
	Reviewers       []glUser    `json:"reviewers"`
	MergedBy        *glUser     `json:"merged_by"`
	ClosedBy        *glUser     `json:"closed_by"`
	MergedAt        *string     `json:"merged_at"`
	ClosedAt        *string     `json:"closed_at"`
	CreatedAt       string      `json:"created_at"`
}

// FetchReview fetches merge requests from GitLab, detecting forks.
func (a *Adapter) FetchReview(opts importpkg.FetchOptions) (*importpkg.ReviewPlan, error) {
	unlimited := opts.Limit == 0
	limit := opts.Limit
	if limit <= 0 {
		limit = 999999
	}
	perPage := 100
	state := mapMRState(opts.State)
	path := fmt.Sprintf("projects/%s/merge_requests?state=%s&per_page=%d&order_by=created_at&sort=desc",
		a.projectPath(), url.QueryEscape(state), perPage)
	var raw []glMergeRequest
	nextPage, err := a.apiGetPage(path, &raw)
	if err != nil {
		return nil, fmt.Errorf("fetch merge requests: %w", err)
	}
	all := raw
	for nextPage != "" && (unlimited || len(all) < limit) {
		var page []glMergeRequest
		pagePath := path + "&page=" + nextPage
		nextPage, err = a.apiGetPage(pagePath, &page)
		if err != nil {
			break
		}
		all = append(all, page...)
		if opts.OnFetchProgress != nil {
			opts.OnFetchProgress(len(all))
		}
	}
	if !unlimited && len(all) > limit {
		all = all[:limit]
	}
	forkSet := map[string]bool{}
	prs := make([]importpkg.ImportPR, 0, len(all))
	var filtered int
	for _, mr := range all {
		if opts.SkipExternalIDs[fmt.Sprintf("pr:%d", mr.IID)] {
			continue
		}
		createdAt, _ := time.Parse(time.RFC3339, mr.CreatedAt)
		if opts.Since != nil && createdAt.Before(*opts.Since) {
			filtered++
			continue
		}
		if opts.SkipBots && isBot(mr.Author.Username, mr.Author.Bot) {
			filtered++
			continue
		}
		labels := mr.Labels
		if labels == nil {
			labels = []string{}
		}
		reviewers := make([]string, 0, len(mr.Reviewers))
		for _, r := range mr.Reviewers {
			reviewers = append(reviewers, a.resolveUser(r.Username).email)
		}
		author := a.resolveUser(mr.Author.Username)
		imp := importpkg.ImportPR{
			ExternalID:  fmt.Sprintf("%d", mr.IID),
			Number:      mr.IID,
			Title:       mr.Title,
			Body:        mr.Description,
			State:       normalizeMRState(mr.State),
			IsDraft:     mr.Draft,
			BaseBranch:  mr.TargetBranch,
			HeadBranch:  mr.SourceBranch,
			Labels:      labels,
			Reviewers:   reviewers,
			AuthorName:  author.name,
			AuthorEmail: author.email,
			CreatedAt:   createdAt,
		}
		imp.HeadSHA = mr.SHA
		if mr.MergeCommitSHA != nil {
			imp.MergeCommit = *mr.MergeCommitSHA
		}
		if mr.MergedBy != nil {
			merged := a.resolveUser(mr.MergedBy.Username)
			imp.MergedByName = merged.name
			imp.MergedByEmail = merged.email
		}
		if mr.ClosedBy != nil {
			closedBy := a.resolveUser(mr.ClosedBy.Username)
			imp.ClosedByName = closedBy.name
			imp.ClosedByEmail = closedBy.email
		}
		if mr.MergedAt != nil {
			if t, err := time.Parse(time.RFC3339, *mr.MergedAt); err == nil {
				imp.MergedAt = t
			}
		}
		if mr.ClosedAt != nil {
			if t, err := time.Parse(time.RFC3339, *mr.ClosedAt); err == nil {
				imp.ClosedAt = t
			}
		}
		if mr.SourceProjectID != 0 && mr.SourceProjectID != mr.TargetProjectID {
			if forkURL := a.resolveProjectURL(mr.SourceProjectID); forkURL != "" {
				imp.HeadRepo = forkURL
				forkSet[forkURL] = true
			}
			if mr.DiffRefs != nil && mr.DiffRefs.BaseSHA != "" {
				imp.DiffBaseSHA = mr.DiffRefs.BaseSHA
			}
		}
		prs = append(prs, imp)
	}
	forks := make([]string, 0, len(forkSet))
	for u := range forkSet {
		forks = append(forks, u)
	}
	return &importpkg.ReviewPlan{Forks: forks, PRs: prs, Filtered: filtered}, nil
}

func normalizeMRState(state string) string {
	switch state {
	case "opened":
		return "open"
	case "closed":
		return "closed"
	case "merged":
		return "merged"
	default:
		return "open"
	}
}

func mapMRState(state string) string {
	switch state {
	case "open":
		return "opened"
	case "closed":
		return "closed"
	case "merged":
		return "merged"
	case "", "all":
		return "all"
	default:
		return "all"
	}
}
