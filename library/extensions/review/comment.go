// comment.go - PR comment integration with social extension
package review

import (
	"github.com/gitsocial-org/gitsocial/library/core/result"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

// GetPRComments retrieves all social comments on a pull request.
func GetPRComments(prRef string, workspaceURL string) Result[[]social.Post] {
	item, err := GetReviewItemByRef(prRef, workspaceURL)
	if err != nil {
		return result.Err[[]social.Post]("NOT_FOUND", "item not found: "+prRef)
	}
	return GetCommentsByKey(item.RepoURL, item.Hash, prRef)
}

// GetCommentsByKey reads a review item's comments on any branch, from its known composite key.
func GetCommentsByKey(repoURL, hash, rootRef string) Result[[]social.Post] {
	posts, err := social.GetComments(repoURL, hash, "", rootRef)
	if err != nil {
		return result.Err[[]social.Post]("QUERY_FAILED", err.Error())
	}
	return result.Ok(posts)
}
