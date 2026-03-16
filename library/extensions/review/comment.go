// comment.go - PR comment integration with social extension
package review

import (
	"github.com/gitsocial-org/gitsocial/core/result"
	"github.com/gitsocial-org/gitsocial/extensions/social"
)

// GetPRComments retrieves all social comments on a pull request.
func GetPRComments(prRef string, workspaceURL string) Result[[]social.Post] {
	item, err := GetReviewItemByRef(prRef, workspaceURL)
	if err != nil {
		return result.Err[[]social.Post]("NOT_FOUND", "item not found: "+prRef)
	}
	return getCommentsByKey(item.RepoURL, item.Hash, prRef)
}

// GetPRCommentsByKey retrieves social comments using a known composite key.
func GetPRCommentsByKey(repoURL, hash, rootRef string) Result[[]social.Post] {
	return getCommentsByKey(repoURL, hash, rootRef)
}

// GetFeedbackComments retrieves all social comments on a feedback item.
func GetFeedbackComments(feedbackRef string, workspaceURL string) Result[[]social.Post] {
	item, err := GetReviewItemByRef(feedbackRef, workspaceURL)
	if err != nil {
		return result.Err[[]social.Post]("NOT_FOUND", "item not found: "+feedbackRef)
	}
	return getCommentsByKey(item.RepoURL, item.Hash, feedbackRef)
}

// GetFeedbackCommentsByKey retrieves social comments using a known composite key.
func GetFeedbackCommentsByKey(repoURL, hash, rootRef string) Result[[]social.Post] {
	return getCommentsByKey(repoURL, hash, rootRef)
}

func getCommentsByKey(repoURL, hash, rootRef string) Result[[]social.Post] {
	items, err := social.GetSocialItems(social.SocialQuery{
		Types:           []string{"comment"},
		OriginalRepoURL: repoURL,
		OriginalHash:    hash,
	})
	if err != nil {
		return result.Err[[]social.Post]("QUERY_FAILED", err.Error())
	}
	posts := make([]social.Post, len(items))
	for i, item := range items {
		posts[i] = social.SocialItemToPost(item)
	}
	if len(posts) > 0 {
		posts = social.SortThreadTree(rootRef, posts)
	}
	return result.Ok(posts)
}
