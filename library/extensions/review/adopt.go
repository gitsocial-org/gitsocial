// adopt.go - Adopting a registered fork's pull request as a workspace copy (GITMSG.md 1.5)
package review

import (
	"database/sql"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
)

// adoptedPRCopy returns the workspace pull request adopting the given original, or nil.
func adoptedPRCopy(workspaceURL, repoURL, hash string) *ReviewItem {
	items, err := cache.QueryLocked(func(db *sql.DB) ([]ReviewItem, error) {
		rows, err := db.Query(baseSelectFromView+`
			WHERE v.repo_url = ? AND v.type = ? AND v.raw_message LIKE ? ESCAPE '\'
			  AND NOT v.is_edit_commit AND NOT v.is_retracted`,
			workspaceURL, string(ItemTypePullRequest), "%"+cache.EscapeLike(`adopts="`)+"%")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []ReviewItem
		for rows.Next() {
			item, err := scanResolvedRow(rows)
			if err != nil {
				return nil, err
			}
			out = append(out, *item)
		}
		return out, rows.Err()
	})
	if err != nil {
		return nil
	}
	want := adoptedPRKey(repoURL, hash)
	for i := range items {
		ref := protocol.ParseRef(items[i].Adopts)
		if ref.Type == protocol.RefTypeCommit && adoptedPRKey(ref.Repository, ref.Value) == want {
			return &items[i]
		}
	}
	return nil
}

// adoptedPRKey names an adopted original by repository identity and short hash.
func adoptedPRKey(repoURL, hash string) string {
	if len(hash) > 12 {
		hash = hash[:12]
	}
	return protocol.NormalizeURL(repoURL) + "#" + hash
}

// throughAdoptedCopy resolves a fork pull request the workspace has adopted to its copy, which carries the state a writer checks.
func throughAdoptedCopy(workspaceURL string, existing *ReviewItem, prRef string) (*ReviewItem, string) {
	if existing.RepoURL == workspaceURL {
		return existing, prRef
	}
	if copied := adoptedPRCopy(workspaceURL, existing.RepoURL, existing.Hash); copied != nil {
		return copied, protocol.CreateRef(protocol.RefTypeCommit, copied.Hash, copied.RepoURL, copied.Branch)
	}
	return existing, prRef
}

// adoptablePR reports whether a change from the workspace adopts this pull request: a registered fork's, targeting the workspace.
func adoptablePR(workdir, workspaceURL string, existing *ReviewItem) bool {
	return existing.RepoURL != workspaceURL && pm.IsRegisteredFork(workdir, existing.RepoURL) && forkPRTargetsWorkspace(*existing, workspaceURL)
}

// AdoptPR adopts a registered fork's pull request as a workspace copy without changing it; a pull request adopted before returns its copy.
func AdoptPR(workdir, prRef string) Result[PullRequest] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetReviewItemByRef(prRef, repoURL)
	if err != nil {
		return result.Err[PullRequest]("NOT_FOUND", "pull request not found")
	}
	if existing.RepoURL == repoURL {
		return result.Err[PullRequest]("NOT_FOREIGN", "the pull request is already in this repository")
	}
	if copied := adoptedPRCopy(repoURL, existing.RepoURL, existing.Hash); copied != nil {
		return result.Ok(ReviewItemToPullRequest(*copied))
	}
	if !adoptablePR(workdir, repoURL, existing) {
		return result.Err[PullRequest]("NOT_A_FORK", "only a registered fork's pull request to this repository can be adopted")
	}
	hash, err := homeForkPR(workdir, repoURL, prRef, existing, ReviewItemToPullRequest(*existing))
	if err != nil {
		return result.Err[PullRequest]("COMMIT_FAILED", err.Error())
	}
	copied, err := GetReviewItem(repoURL, hash, gitmsg.GetExtBranch(workdir, "review"))
	if err != nil {
		return result.Err[PullRequest]("GET_FAILED", err.Error())
	}
	return result.Ok(ReviewItemToPullRequest(*copied))
}
