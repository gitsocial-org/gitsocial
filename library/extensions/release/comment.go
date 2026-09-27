// comment.go - Release comment integration with social extension
package release

import (
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

// GetReleaseComments retrieves all social comments on a release.
func GetReleaseComments(releaseRef string, workspaceURL string) Result[[]social.Post] {
	item, err := GetReleaseItemByRef(releaseRef, workspaceURL)
	if err != nil {
		return result.Err[[]social.Post]("NOT_FOUND", notFoundMessage(releaseRef, err))
	}

	itemID := protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch)
	posts, err := social.GetComments(item.RepoURL, item.Hash, item.Branch, itemID)
	if err != nil {
		return result.Err[[]social.Post]("QUERY_FAILED", err.Error())
	}
	return result.Ok(posts)
}
