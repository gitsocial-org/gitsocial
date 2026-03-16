// comment.go - PM item comment integration with social extension
package pm

import (
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/result"
	"github.com/gitsocial-org/gitsocial/extensions/social"
)

// CommentOnItem creates a comment on a PM item (issue, milestone, sprint) using the social extension.
func CommentOnItem(workdir, itemRef, content string) Result[social.Post] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	_, err := GetPMItemByRef(itemRef, repoURL)
	if err != nil {
		return result.Err[social.Post]("NOT_FOUND", "item not found: "+itemRef)
	}

	socialResult := social.CreateComment(workdir, itemRef, content, nil)
	if !socialResult.Success {
		return result.Err[social.Post](socialResult.Error.Code, socialResult.Error.Message)
	}

	return result.Ok(socialResult.Data)
}

// GetItemComments retrieves all comments on a PM item (issue, milestone, sprint).
func GetItemComments(itemRef string, workspaceURL string) Result[[]social.Post] {
	item, err := GetPMItemByRef(itemRef, workspaceURL)
	if err != nil {
		return result.Err[[]social.Post]("NOT_FOUND", "item not found: "+itemRef)
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

	// Sort into thread tree for proper nesting
	if len(posts) > 0 {
		posts = social.SortThreadTree(itemRef, posts)
	}

	return result.Ok(posts)
}
