// issues.go - Fetch GitLab milestones and issues via REST API
package gitlab

import (
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/gitsocial-org/gitsocial/core/log"
	importpkg "github.com/gitsocial-org/gitsocial/import"
)

type glMilestone struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	State       string  `json:"state"`
	DueDate     *string `json:"due_date"`
	CreatedAt   string  `json:"created_at"`
}

type glIssue struct {
	IID       int               `json:"iid"`
	Title     string            `json:"title"`
	Body      string            `json:"description"`
	State     string            `json:"state"`
	Labels    []string          `json:"labels"`
	Author    glUser            `json:"author"`
	Assignees []glUser          `json:"assignees"`
	Milestone *glIssueMilestone `json:"milestone"`
	ClosedBy  *glUser           `json:"closed_by"`
	CreatedAt string            `json:"created_at"`
	ClosedAt  *string           `json:"closed_at"`
}

type glUser struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	Bot      bool   `json:"bot"`
}

type glIssueMilestone struct {
	Title string `json:"title"`
}

// FetchPM fetches milestones and issues from GitLab.
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
	var raw []glMilestone
	path := fmt.Sprintf("projects/%s/milestones?state=all&per_page=100", a.projectPath())
	if err := a.apiGet(path, &raw); err != nil {
		return nil, err
	}
	out := make([]importpkg.ImportMilestone, 0, len(raw))
	for _, m := range raw {
		if opts.SkipExternalIDs["milestone:"+m.Title] {
			continue
		}
		createdAt, _ := time.Parse(time.RFC3339, m.CreatedAt)
		im := importpkg.ImportMilestone{
			ExternalID: m.Title,
			Number:     m.ID,
			Title:      m.Title,
			Body:       m.Description,
			State:      normalizeMilestoneState(m.State),
			CreatedAt:  createdAt,
		}
		if m.DueDate != nil {
			if t, err := time.Parse("2006-01-02", *m.DueDate); err == nil {
				im.DueDate = &t
			}
		}
		out = append(out, im)
	}
	return out, nil
}

func (a *Adapter) fetchIssues(opts importpkg.FetchOptions) ([]importpkg.ImportIssue, int, error) {
	unlimited := opts.Limit == 0
	limit := opts.Limit
	if limit <= 0 {
		limit = 999999
	}
	perPage := 100
	state := mapIssueState(opts.State)
	path := fmt.Sprintf("projects/%s/issues?state=%s&per_page=%d&order_by=created_at&sort=desc",
		a.projectPath(), url.QueryEscape(state), perPage)
	var raw []glIssue
	nextPage, err := a.apiGetPage(path, &raw)
	if err != nil {
		return nil, 0, err
	}
	all := raw
	for nextPage != "" && (unlimited || len(all) < limit) {
		var page []glIssue
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
	out := make([]importpkg.ImportIssue, 0, len(all))
	var filtered int
	for _, issue := range all {
		if opts.SkipExternalIDs[fmt.Sprintf("issue:%d", issue.IID)] {
			continue
		}
		createdAt, _ := time.Parse(time.RFC3339, issue.CreatedAt)
		if opts.Since != nil && createdAt.Before(*opts.Since) {
			filtered++
			continue
		}
		if opts.SkipBots && isBot(issue.Author.Username, issue.Author.Bot) {
			filtered++
			continue
		}
		author := a.resolveUser(issue.Author.Username)
		im := importpkg.ImportIssue{
			ExternalID:  fmt.Sprintf("%d", issue.IID),
			Number:      issue.IID,
			Title:       issue.Title,
			Body:        issue.Body,
			State:       normalizeIssueState(issue.State),
			Labels:      issue.Labels,
			AuthorName:  author.name,
			AuthorEmail: author.email,
			CreatedAt:   createdAt,
		}
		assignees := make([]string, 0, len(issue.Assignees))
		for _, u := range issue.Assignees {
			assignees = append(assignees, a.resolveUser(u.Username).email)
		}
		im.Assignees = assignees
		if issue.Milestone != nil {
			im.MilestoneID = issue.Milestone.Title
		}
		if issue.ClosedBy != nil {
			closedBy := a.resolveUser(issue.ClosedBy.Username)
			im.ClosedByName = closedBy.name
			im.ClosedByEmail = closedBy.email
		}
		if issue.ClosedAt != nil {
			if t, err := time.Parse(time.RFC3339, *issue.ClosedAt); err == nil {
				im.ClosedAt = t
			}
		}
		out = append(out, im)
	}
	// Phase 1: Batch-fetch blocking links via GraphQL (fast, ~N/100 calls)
	iidToIdx := make(map[int]int, len(out))
	for i, issue := range out {
		iidToIdx[issue.Number] = i
	}
	graphqlLinks := a.fetchBlockingLinksGraphQL()
	hasGraphQL := len(graphqlLinks) > 0
	for iid, links := range graphqlLinks {
		idx, ok := iidToIdx[iid]
		if !ok {
			continue
		}
		out[idx].BlocksIDs = links.blocks
		out[idx].BlockedByIDs = links.blockedBy
	}
	// Phase 2: Parallel REST for relates_to links (and full link data if GraphQL unavailable)
	type linkResult struct {
		idx                        int
		blocks, blockedBy, related []string
	}
	linkCh := make(chan int, len(out))
	resultCh := make(chan linkResult, len(out))
	var wg sync.WaitGroup
	workers := 10
	if len(out) < workers {
		workers = len(out)
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range linkCh {
				iid := out[idx].Number
				blocks, blockedBy, related := a.fetchIssueLinks(iid)
				resultCh <- linkResult{idx: idx, blocks: blocks, blockedBy: blockedBy, related: related}
			}
		}()
	}
	for i := range out {
		linkCh <- i
	}
	close(linkCh)
	go func() {
		wg.Wait()
		close(resultCh)
	}()
	done := 0
	for r := range resultCh {
		done++
		if hasGraphQL {
			// GraphQL already provided blocks/blockedBy; only add relates_to and fill gaps
			out[r.idx].RelatedIDs = r.related
			if len(out[r.idx].BlocksIDs) == 0 && len(r.blocks) > 0 {
				out[r.idx].BlocksIDs = r.blocks
			}
			if len(out[r.idx].BlockedByIDs) == 0 && len(r.blockedBy) > 0 {
				out[r.idx].BlockedByIDs = r.blockedBy
			}
		} else {
			out[r.idx].BlocksIDs = r.blocks
			out[r.idx].BlockedByIDs = r.blockedBy
			out[r.idx].RelatedIDs = r.related
		}
		if opts.OnFetchProgress != nil && done%50 == 0 {
			opts.OnFetchProgress(done)
		}
	}
	return out, filtered, nil
}

// graphqlBlockingLinks holds blocking relationship data from GraphQL.
type graphqlBlockingLinks struct {
	blocks    []string
	blockedBy []string
}

// fetchBlockingLinksGraphQL batch-fetches issue blocking links via GitLab GraphQL API.
// Returns a map of iid -> blocking links. Returns empty map if GraphQL is unavailable.
func (a *Adapter) fetchBlockingLinksGraphQL() map[int]graphqlBlockingLinks {
	result := map[int]graphqlBlockingLinks{}
	query := `query($fullPath: ID!, $after: String) {
		project(fullPath: $fullPath) {
			issues(first: 100, after: $after) {
				nodes {
					iid
					blockedByIssues { nodes { iid } }
					blockingIssues { nodes { iid } }
				}
				pageInfo { hasNextPage endCursor }
			}
		}
	}`
	fullPath := a.owner + "/" + a.repo
	var cursor *string
	for {
		vars := map[string]interface{}{"fullPath": fullPath}
		if cursor != nil {
			vars["after"] = *cursor
		}
		var data struct {
			Project struct {
				Issues struct {
					Nodes []struct {
						IID             string `json:"iid"`
						BlockedByIssues struct {
							Nodes []struct {
								IID string `json:"iid"`
							}
						} `json:"blockedByIssues"`
						BlockingIssues struct {
							Nodes []struct {
								IID string `json:"iid"`
							}
						} `json:"blockingIssues"`
					}
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"issues"`
			} `json:"project"`
		}
		if err := a.graphqlQuery(query, vars, &data); err != nil {
			log.Debug("graphql blocking links query failed, falling back to REST", "error", err)
			return result
		}
		for _, node := range data.Project.Issues.Nodes {
			iid := 0
			_, _ = fmt.Sscanf(node.IID, "%d", &iid)
			if iid == 0 {
				continue
			}
			links := graphqlBlockingLinks{}
			for _, b := range node.BlockingIssues.Nodes {
				links.blocks = append(links.blocks, b.IID)
			}
			for _, b := range node.BlockedByIssues.Nodes {
				links.blockedBy = append(links.blockedBy, b.IID)
			}
			result[iid] = links
		}
		if !data.Project.Issues.PageInfo.HasNextPage {
			break
		}
		endCursor := data.Project.Issues.PageInfo.EndCursor
		cursor = &endCursor
	}
	return result
}

func normalizeMilestoneState(state string) string {
	switch state {
	case "active":
		return "open"
	case "closed":
		return "closed"
	default:
		return "open"
	}
}

func normalizeIssueState(state string) string {
	switch state {
	case "opened":
		return "open"
	case "closed":
		return "closed"
	default:
		return "open"
	}
}

type glLinkedIssue struct {
	IID      int    `json:"iid"`
	LinkType string `json:"link_type"` // "relates_to", "blocks", "is_blocked_by"
}

// fetchIssueLinks fetches linked issues for a single issue via GitLab API.
func (a *Adapter) fetchIssueLinks(iid int) (blocks, blockedBy, related []string) {
	var raw []glLinkedIssue
	path := fmt.Sprintf("projects/%s/issues/%d/links", a.projectPath(), iid)
	if err := a.apiGet(path, &raw); err != nil {
		log.Debug("failed to fetch issue links", "iid", iid, "error", err)
		return nil, nil, nil
	}
	for _, link := range raw {
		id := fmt.Sprintf("%d", link.IID)
		switch link.LinkType {
		case "blocks":
			blocks = append(blocks, id)
		case "is_blocked_by":
			blockedBy = append(blockedBy, id)
		case "relates_to":
			related = append(related, id)
		}
	}
	return blocks, blockedBy, related
}

func mapIssueState(state string) string {
	switch state {
	case "open":
		return "opened"
	case "closed":
		return "closed"
	case "", "all":
		return "all"
	default:
		return "all"
	}
}
