// discussions.go - Fetch GitHub Discussions via GraphQL for social import
package github

import (
	"fmt"
	"time"

	"github.com/gitsocial-org/gitsocial/core/log"
	importpkg "github.com/gitsocial-org/gitsocial/import"
)

type ghDiscussion struct {
	Number   int        `json:"number"`
	Title    string     `json:"title"`
	Body     string     `json:"body"`
	Author   ghAuthor   `json:"author"`
	Category ghCategory `json:"category"`
	Comments struct {
		Nodes    []ghDiscussionComment `json:"nodes"`
		PageInfo ghPageInfo            `json:"pageInfo"`
	} `json:"comments"`
	CreatedAt time.Time `json:"createdAt"`
}

type ghCategory struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type ghDiscussionComment struct {
	Body      string    `json:"body"`
	Author    ghAuthor  `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
}

type ghPageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

// buildDiscussionQuery builds a GraphQL query for fetching discussions with cursor pagination.
func buildDiscussionQuery(owner, repo string, first int, cursor string) string {
	afterClause := ""
	if cursor != "" {
		afterClause = fmt.Sprintf(", after: %q", cursor)
	}
	return fmt.Sprintf(`{
  repository(owner: %q, name: %q) {
    discussions(first: %d%s, orderBy: {field: CREATED_AT, direction: DESC}) {
      nodes {
        number
        title
        body
        author { login ... on User { name } }
        category { name slug }
        createdAt
        comments(first: 100) {
          nodes {
            body
            author { login ... on User { name } }
            createdAt
          }
          pageInfo { hasNextPage endCursor }
        }
      }
      pageInfo { hasNextPage endCursor }
    }
  }
}`, owner, repo, first, afterClause)
}

func (a *Adapter) fetchDiscussions(opts importpkg.FetchOptions) (*importpkg.SocialPlan, error) {
	unlimited := opts.Limit == 0
	limit := opts.Limit
	if limit <= 0 {
		limit = 999999
	}
	var allowed map[string]bool
	if len(opts.Categories) > 0 {
		allowed = map[string]bool{}
		for _, c := range opts.Categories {
			allowed[c] = true
		}
	}
	// Fetch discussions with cursor pagination
	var allDiscussions []ghDiscussion
	cursor := ""
	for {
		pageSize := 100
		remaining := limit - len(allDiscussions)
		if !unlimited && remaining < pageSize {
			pageSize = remaining
		}
		if pageSize <= 0 {
			break
		}
		query := buildDiscussionQuery(a.owner, a.repo, pageSize, cursor)
		var resp struct {
			Data struct {
				Repository struct {
					Discussions struct {
						Nodes    []ghDiscussion `json:"nodes"`
						PageInfo ghPageInfo     `json:"pageInfo"`
					} `json:"discussions"`
				} `json:"repository"`
			} `json:"data"`
		}
		if err := ghJSON(&resp, "api", "graphql", "-f", "query="+query); err != nil {
			if len(allDiscussions) == 0 {
				return nil, fmt.Errorf("fetch discussions: %w", err)
			}
			log.Warn("discussion pagination failed, returning partial results", "fetched", len(allDiscussions), "error", err)
			break
		}
		allDiscussions = append(allDiscussions, resp.Data.Repository.Discussions.Nodes...)
		if opts.OnFetchProgress != nil {
			opts.OnFetchProgress(len(allDiscussions))
		}
		if !resp.Data.Repository.Discussions.PageInfo.HasNextPage {
			break
		}
		if !unlimited && len(allDiscussions) >= limit {
			break
		}
		cursor = resp.Data.Repository.Discussions.PageInfo.EndCursor
	}
	// Paginate comments for discussions that hit the 100-comment cap (skip already-imported)
	for i, d := range allDiscussions {
		if opts.SkipExternalIDs[fmt.Sprintf("post:%d", d.Number)] {
			continue
		}
		for d.Comments.PageInfo.HasNextPage {
			more, err := a.fetchMoreComments(d.Number, d.Comments.PageInfo.EndCursor)
			if err != nil {
				log.Debug("comment pagination failed", "discussion", d.Number, "error", err)
				break
			}
			allDiscussions[i].Comments.Nodes = append(allDiscussions[i].Comments.Nodes, more.Nodes...)
			allDiscussions[i].Comments.PageInfo = more.PageInfo
			d.Comments.PageInfo = more.PageInfo
		}
	}
	var posts []importpkg.ImportPost
	var comments []importpkg.ImportComment
	var filtered int
	for _, d := range allDiscussions {
		if opts.SkipExternalIDs[fmt.Sprintf("post:%d", d.Number)] {
			continue
		}
		if allowed != nil && !allowed[d.Category.Slug] {
			filtered++
			continue
		}
		if opts.Since != nil && d.CreatedAt.Before(*opts.Since) {
			filtered++
			continue
		}
		if opts.SkipBots && isBot(d.Author.Login) {
			filtered++
			continue
		}
		extID := fmt.Sprintf("%d", d.Number)
		content := "# " + d.Title + "\n\n" + d.Body
		dAuthor := a.resolveUser(d.Author.Login)
		posts = append(posts, importpkg.ImportPost{
			ExternalID:  extID,
			Content:     content,
			AuthorName:  dAuthor.name,
			AuthorEmail: dAuthor.email,
			CreatedAt:   d.CreatedAt,
		})
		for _, c := range d.Comments.Nodes {
			commentExtID := fmt.Sprintf("%d-%s", d.Number, c.CreatedAt.Format("20060102T150405"))
			if opts.SkipExternalIDs["comment:"+commentExtID] {
				continue
			}
			if opts.SkipBots && isBot(c.Author.Login) {
				continue
			}
			cAuthor := a.resolveUser(c.Author.Login)
			comments = append(comments, importpkg.ImportComment{
				ExternalID:  fmt.Sprintf("%d-%s", d.Number, c.CreatedAt.Format("20060102T150405")),
				PostID:      extID,
				Content:     c.Body,
				AuthorName:  cAuthor.name,
				AuthorEmail: cAuthor.email,
				CreatedAt:   c.CreatedAt,
			})
		}
	}
	return &importpkg.SocialPlan{Posts: posts, Comments: comments, Filtered: filtered}, nil
}

// fetchMoreComments paginates through remaining comments on a discussion.
func (a *Adapter) fetchMoreComments(discussionNumber int, cursor string) (struct {
	Nodes    []ghDiscussionComment
	PageInfo ghPageInfo
}, error) {
	type commentsResult struct {
		Nodes    []ghDiscussionComment `json:"nodes"`
		PageInfo ghPageInfo            `json:"pageInfo"`
	}
	var empty struct {
		Nodes    []ghDiscussionComment
		PageInfo ghPageInfo
	}
	query := fmt.Sprintf(`{
  repository(owner: %q, name: %q) {
    discussion(number: %d) {
      comments(first: 100, after: %q) {
        nodes {
          body
          author { login ... on User { name } }
          createdAt
        }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}`, a.owner, a.repo, discussionNumber, cursor)
	var resp struct {
		Data struct {
			Repository struct {
				Discussion struct {
					Comments commentsResult `json:"comments"`
				} `json:"discussion"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := ghJSON(&resp, "api", "graphql", "-f", "query="+query); err != nil {
		return empty, fmt.Errorf("fetch comments page: %w", err)
	}
	return struct {
		Nodes    []ghDiscussionComment
		PageInfo ghPageInfo
	}{
		Nodes:    resp.Data.Repository.Discussion.Comments.Nodes,
		PageInfo: resp.Data.Repository.Discussion.Comments.PageInfo,
	}, nil
}
