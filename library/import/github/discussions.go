// discussions.go - Fetch GitHub Discussions via GraphQL for social import
package github

import (
	"fmt"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/log"
	importpkg "github.com/gitsocial-org/gitsocial/library/import"
)

type ghDiscussion struct {
	Number    int                     `json:"number"`
	Title     string                  `json:"title"`
	Body      string                  `json:"body"`
	Author    ghAuthor                `json:"author"`
	Category  ghCategory              `json:"category"`
	Comments  ghDiscussionCommentPage `json:"comments"`
	CreatedAt time.Time               `json:"createdAt"`
	UpdatedAt time.Time               `json:"updatedAt"`
}

type ghCategory struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type ghDiscussionComment struct {
	ID         string                  `json:"id"`
	DatabaseID int64                   `json:"databaseId"`
	Body       string                  `json:"body"`
	Author     ghAuthor                `json:"author"`
	CreatedAt  time.Time               `json:"createdAt"`
	Replies    ghDiscussionCommentPage `json:"replies"`
}

type ghDiscussionCommentPage struct {
	Nodes    []ghDiscussionComment `json:"nodes"`
	PageInfo ghPageInfo            `json:"pageInfo"`
}

type ghPageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

// discussionCommentFields is the field set every discussion comment selection shares.
const discussionCommentFields = `id databaseId body author { login ... on User { name email } } createdAt`

// discussionCommentSelection selects a comment together with the first page of its replies.
const discussionCommentSelection = discussionCommentFields +
	` replies(first: 100) { nodes { ` + discussionCommentFields + ` } pageInfo { hasNextPage endCursor } }`

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
        author { login ... on User { name email } }
        category { name slug }
        createdAt
        updatedAt
        comments(first: 100) {
          nodes { %s }
          pageInfo { hasNextPage endCursor }
        }
      }
      pageInfo { hasNextPage endCursor }
    }
  }
}`, owner, repo, first, afterClause, discussionCommentSelection)
}

// fetchDiscussions returns GitHub discussions and their comments as a social plan.
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
	// Paginate comments and replies past their 100-item caps
	for i := range allDiscussions {
		a.paginateDiscussionComments(&allDiscussions[i])
	}
	var logins []string
	for _, d := range allDiscussions {
		a.storeQueryProfile(d.Author)
		logins = append(logins, d.Author.Login)
		for _, c := range d.Comments.Nodes {
			a.storeQueryProfile(c.Author)
			logins = append(logins, c.Author.Login)
			for _, r := range c.Replies.Nodes {
				a.storeQueryProfile(r.Author)
				logins = append(logins, r.Author.Login)
			}
		}
	}
	a.prefetchUsers(logins)
	var posts []importpkg.ImportPost
	var comments []importpkg.ImportComment
	var filtered int
	for _, d := range allDiscussions {
		// An imported discussion is not planned again, but its comments still walk the comment path.
		if opts.SkipExternalIDs[fmt.Sprintf("post:%d", d.Number)] {
			comments = append(comments, a.planDiscussionComments(d, opts)...)
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
			UpdatedAt:   d.UpdatedAt,
		})
		comments = append(comments, a.planDiscussionComments(d, opts)...)
	}
	return &importpkg.SocialPlan{Posts: posts, Comments: comments, Filtered: filtered}, nil
}

// discussionCommentExternalID returns the external ID of one discussion comment, the ID its GitHub anchor carries.
func discussionCommentExternalID(number int, c ghDiscussionComment) string {
	return commentExternalID(number, c.DatabaseID, c.CreatedAt)
}

// planDiscussionComments converts a discussion's comments and their replies, each reply naming its parent.
func (a *Adapter) planDiscussionComments(d ghDiscussion, opts importpkg.FetchOptions) []importpkg.ImportComment {
	postID := fmt.Sprintf("%d", d.Number)
	var out []importpkg.ImportComment
	add := func(c ghDiscussionComment, extID, parentID string, parentCreatedAt time.Time) {
		// A mapping written before databaseId keyed the comment by number and second.
		legacyID := commentExternalID(d.Number, 0, c.CreatedAt)
		if opts.SkipExternalIDs["comment:"+extID] || opts.SkipExternalIDs["comment:"+legacyID] {
			return
		}
		if opts.SkipBots && isBot(c.Author.Login) {
			return
		}
		author := a.resolveUser(c.Author.Login)
		out = append(out, importpkg.ImportComment{
			ExternalID:      extID,
			PostID:          postID,
			ParentID:        parentID,
			ParentCreatedAt: parentCreatedAt,
			Content:         c.Body,
			AuthorName:      author.name,
			AuthorEmail:     author.email,
			CreatedAt:       c.CreatedAt,
		})
	}
	for _, c := range d.Comments.Nodes {
		parentID := discussionCommentExternalID(d.Number, c)
		add(c, parentID, "", time.Time{})
		for _, r := range c.Replies.Nodes {
			add(r, discussionCommentExternalID(d.Number, r), parentID, c.CreatedAt)
		}
	}
	return out
}

// paginateDiscussionComments appends a discussion's remaining comment pages and each comment's remaining reply pages.
func (a *Adapter) paginateDiscussionComments(d *ghDiscussion) {
	for d.Comments.PageInfo.HasNextPage {
		more, err := a.fetchMoreComments(d.Number, d.Comments.PageInfo.EndCursor)
		if err != nil {
			log.Debug("comment pagination failed", "discussion", d.Number, "error", err)
			break
		}
		d.Comments.Nodes = append(d.Comments.Nodes, more.Nodes...)
		d.Comments.PageInfo = more.PageInfo
	}
	for i := range d.Comments.Nodes {
		c := &d.Comments.Nodes[i]
		for c.Replies.PageInfo.HasNextPage {
			more, err := a.fetchMoreReplies(c.ID, c.Replies.PageInfo.EndCursor)
			if err != nil {
				log.Debug("reply pagination failed", "comment", c.ID, "error", err)
				break
			}
			c.Replies.Nodes = append(c.Replies.Nodes, more.Nodes...)
			c.Replies.PageInfo = more.PageInfo
		}
	}
}

// fetchMoreComments paginates through remaining comments on a discussion.
func (a *Adapter) fetchMoreComments(discussionNumber int, cursor string) (ghDiscussionCommentPage, error) {
	query := fmt.Sprintf(`{
  repository(owner: %q, name: %q) {
    discussion(number: %d) {
      comments(first: 100, after: %q) {
        nodes { %s }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}`, a.owner, a.repo, discussionNumber, cursor, discussionCommentSelection)
	var resp struct {
		Data struct {
			Repository struct {
				Discussion struct {
					Comments ghDiscussionCommentPage `json:"comments"`
				} `json:"discussion"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := ghJSON(&resp, "api", "graphql", "-f", "query="+query); err != nil {
		return ghDiscussionCommentPage{}, fmt.Errorf("fetch comments page: %w", err)
	}
	return resp.Data.Repository.Discussion.Comments, nil
}

// fetchMoreReplies paginates through remaining replies on a discussion comment.
func (a *Adapter) fetchMoreReplies(commentID, cursor string) (ghDiscussionCommentPage, error) {
	query := fmt.Sprintf(`{
  node(id: %q) {
    ... on DiscussionComment {
      replies(first: 100, after: %q) {
        nodes { %s }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}`, commentID, cursor, discussionCommentFields)
	var resp struct {
		Data struct {
			Node struct {
				Replies ghDiscussionCommentPage `json:"replies"`
			} `json:"node"`
		} `json:"data"`
	}
	if err := ghJSON(&resp, "api", "graphql", "-f", "query="+query); err != nil {
		return ghDiscussionCommentPage{}, fmt.Errorf("fetch replies page: %w", err)
	}
	return resp.Data.Node.Replies, nil
}
