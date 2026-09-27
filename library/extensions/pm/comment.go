// comment.go - PM item comment integration with social extension
package pm

import (
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

// CommentOnItem creates a comment on a PM item (issue, milestone, sprint) using the social extension.
func CommentOnItem(workdir, itemRef, content string) Result[social.Post] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	item, err := GetPMItemByRef(itemRef, repoURL)
	if err != nil {
		return result.Err[social.Post]("NOT_FOUND", notFoundMessage("item", itemRef, err))
	}

	itemID := protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch)
	socialResult := social.CreateComment(workdir, itemID, content, nil)
	if !socialResult.Success {
		return result.Err[social.Post](socialResult.Error.Code, socialResult.Error.Message)
	}

	return result.Ok(socialResult.Data)
}

// GetItemComments retrieves all comments on a PM item (issue, milestone, sprint).
func GetItemComments(itemRef string, workspaceURL string) Result[[]social.Post] {
	item, err := GetPMItemByRef(itemRef, workspaceURL)
	if err != nil {
		return result.Err[[]social.Post]("NOT_FOUND", notFoundMessage("item", itemRef, err))
	}

	itemID := protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch)
	posts, err := social.GetComments(item.RepoURL, item.Hash, item.Branch, itemID)
	if err != nil {
		return result.Err[[]social.Post]("QUERY_FAILED", err.Error())
	}
	return result.Ok(posts)
}
