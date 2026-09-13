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
	posts, err := social.GetComments(item.RepoURL, item.Hash, item.Branch, memoRef)
	if err != nil {
		return result.Err[[]social.Post]("QUERY_FAILED", err.Error())
	}
	return result.Ok(posts)
}
