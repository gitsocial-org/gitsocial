// comments.go - Batch-fetch issue and PR conversation comments via GraphQL
package github

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/log"
	importpkg "github.com/gitsocial-org/gitsocial/library/import"
)

// commentBatchSize is how many issues/PRs share one GraphQL query (aliased fields).
const commentBatchSize = 25

type ghItemComment struct {
	DatabaseID int64     `json:"databaseId"`
	Body       string    `json:"body"`
	Author     ghAuthor  `json:"author"`
	CreatedAt  time.Time `json:"createdAt"`
}

type ghItemCommentPage struct {
	Nodes    []ghItemComment `json:"nodes"`
	PageInfo ghPageInfo      `json:"pageInfo"`
}

// buildItemCommentsQuery builds a GraphQL query fetching the first comments of
// several issues or PRs at once via aliased fields. field is "issue" or "pullRequest".
func buildItemCommentsQuery(owner, repo, field string, numbers []int) string {
	var fields string
	for _, n := range numbers {
		fields += fmt.Sprintf(`  c%d: %s(number: %d) { comments(first: 50) { nodes { databaseId body author { login ... on User { name } } createdAt } pageInfo { hasNextPage endCursor } } }
`, n, field, n)
	}
	return fmt.Sprintf("{ repository(owner: %q, name: %q) {\n%s} }", owner, repo, fields)
}

// buildMoreItemCommentsQuery builds a cursor-paginated query for one item's remaining comments.
func buildMoreItemCommentsQuery(owner, repo, field string, number int, cursor string) string {
	return fmt.Sprintf(`{
  repository(owner: %q, name: %q) {
    item: %s(number: %d) {
      comments(first: 100, after: %q) {
        nodes { databaseId body author { login ... on User { name } } createdAt }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}`, owner, repo, field, number, cursor)
}

// batchFetchComments fetches conversation comments for the given issue or PR
// numbers, batched over GraphQL with cursor pagination for long threads.
// Errors are logged and the affected items skipped (partial results win over
// failing the whole import).
func (a *Adapter) batchFetchComments(field string, numbers []int) map[int][]ghItemComment {
	result := map[int][]ghItemComment{}
	for i := 0; i < len(numbers); i += commentBatchSize {
		end := i + commentBatchSize
		if end > len(numbers) {
			end = len(numbers)
		}
		batch := numbers[i:end]
		query := buildItemCommentsQuery(a.owner, a.repo, field, batch)
		var resp struct {
			Data struct {
				Repository map[string]json.RawMessage `json:"repository"`
			} `json:"data"`
		}
		if err := ghJSON(&resp, "api", "graphql", "-f", "query="+query); err != nil {
			log.Warn("graphql batch comments query failed", "field", field, "error", err)
			continue
		}
		for _, n := range batch {
			raw, ok := resp.Data.Repository[fmt.Sprintf("c%d", n)]
			if !ok || string(raw) == "null" {
				continue
			}
			var item struct {
				Comments ghItemCommentPage `json:"comments"`
			}
			if err := json.Unmarshal(raw, &item); err != nil {
				log.Debug("failed to unmarshal item comments", "number", n, "error", err)
				continue
			}
			nodes := item.Comments.Nodes
			page := item.Comments.PageInfo
			for page.HasNextPage {
				more, err := a.fetchMoreItemComments(field, n, page.EndCursor)
				if err != nil {
					log.Debug("comment pagination failed", "number", n, "error", err)
					break
				}
				nodes = append(nodes, more.Nodes...)
				page = more.PageInfo
			}
			if len(nodes) > 0 {
				result[n] = nodes
			}
		}
	}
	return result
}

// fetchMoreItemComments paginates through remaining comments on one issue or PR.
func (a *Adapter) fetchMoreItemComments(field string, number int, cursor string) (ghItemCommentPage, error) {
	query := buildMoreItemCommentsQuery(a.owner, a.repo, field, number, cursor)
	var resp struct {
		Data struct {
			Repository struct {
				Item struct {
					Comments ghItemCommentPage `json:"comments"`
				} `json:"item"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := ghJSON(&resp, "api", "graphql", "-f", "query="+query); err != nil {
		return ghItemCommentPage{}, fmt.Errorf("fetch comments page: %w", err)
	}
	return resp.Data.Repository.Item.Comments, nil
}

// itemCommentExternalID returns a stable external ID for a comment, falling
// back to number+timestamp when the API omits databaseId.
func itemCommentExternalID(number int, c ghItemComment) string {
	if c.DatabaseID != 0 {
		return fmt.Sprintf("%d", c.DatabaseID)
	}
	return fmt.Sprintf("%d-%s", number, c.CreatedAt.Format("20060102T150405"))
}

// fetchItemComments fetches conversation comments for the given item numbers
// and converts them to ImportComments. keyType is the mapping key type used in
// SkipExternalIDs ("issue-comment" or "pr-comment").
func (a *Adapter) fetchItemComments(field, keyType string, numbers []int, opts importpkg.FetchOptions) []importpkg.ImportComment {
	if len(numbers) == 0 {
		return nil
	}
	byNumber := a.batchFetchComments(field, numbers)
	var logins []string
	for _, nodes := range byNumber {
		for _, c := range nodes {
			logins = append(logins, c.Author.Login)
		}
	}
	a.prefetchUsers(logins)
	var out []importpkg.ImportComment
	for _, n := range numbers {
		for _, c := range byNumber[n] {
			extID := itemCommentExternalID(n, c)
			if opts.SkipExternalIDs[keyType+":"+extID] {
				continue
			}
			if opts.SkipBots && isBot(c.Author.Login) {
				continue
			}
			author := a.resolveUser(c.Author.Login)
			out = append(out, importpkg.ImportComment{
				ExternalID:  extID,
				PostID:      fmt.Sprintf("%d", n),
				Content:     c.Body,
				AuthorName:  author.name,
				AuthorEmail: author.email,
				CreatedAt:   c.CreatedAt,
			})
		}
	}
	return out
}
