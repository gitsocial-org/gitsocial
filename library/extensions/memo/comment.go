// comment.go - Memo comment integration with social extension
package memo

import (
	"github.com/gitsocial-org/gitsocial/library/core/result"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

// GetMemoComments retrieves all social comments on a memo.
func GetMemoComments(memoRef, workspaceURL string) Result[[]social.Post] {
	item, err := GetMemoItemByRef(memoRef, workspaceURL)
	if err != nil {
		return result.Err[[]social.Post]("NOT_FOUND", "memo not found: "+memoRef)
	}
	items, err := social.GetSocialItems(social.SocialQuery{
		Types:           []string{"comment"},
		OriginalRepoURL: item.RepoURL,
		OriginalHash:    item.Hash,
		OriginalBranch:  item.Branch,
	})
	if err != nil {
		return result.Err[[]social.Post]("QUERY_FAILED", err.Error())
	}
	posts := make([]social.Post, len(items))
	for i, it := range items {
		posts[i] = social.SocialItemToPost(it)
	}
	if len(posts) > 0 {
		posts = social.SortThreadTree(memoRef, posts)
	}
	return result.Ok(posts)
}
