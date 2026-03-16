// comment.go - Release comment integration with social extension
package release

import (
	"github.com/gitsocial-org/gitsocial/core/result"
	"github.com/gitsocial-org/gitsocial/extensions/social"
)

// GetReleaseComments retrieves all social comments on a release.
func GetReleaseComments(releaseRef string, workspaceURL string) Result[[]social.Post] {
	item, err := GetReleaseItemByRef(releaseRef, workspaceURL)
	if err != nil {
		return result.Err[[]social.Post]("NOT_FOUND", "item not found: "+releaseRef)
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
	for i, item := range items {
		posts[i] = social.SocialItemToPost(item)
	}

	if len(posts) > 0 {
		posts = social.SortThreadTree(releaseRef, posts)
	}

	return result.Ok(posts)
}
