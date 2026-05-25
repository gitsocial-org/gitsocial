// promote.go - Memo cross-tier promotion (copy semantics)
package memo

import (
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/result"
)

// promoteRank returns the durability rank used for promotion direction:
// session(0) → personal(1) → project(2). Other tiers can't participate in
// promotion (sources of inherited/external are read-only; targets must be
// a writable local tier).
func promoteRank(t Tier) int {
	switch t {
	case TierSession:
		return 0
	case TierPersonal:
		return 1
	case TierProject:
		return 2
	}
	return -1
}

// PromoteMemo copies a memo to the target tier as a fresh commit. The source
// stays put — there is no edit chain or back-reference. The same memo can
// therefore appear at multiple tiers; this is intentional so that `memo list`
// reflects exactly where each version lives.
func PromoteMemo(workdir, memoRef string, target Tier) Result[Memo] {
	if target != TierPersonal && target != TierProject {
		return result.Err[Memo]("INVALID_ARGS", "promote target must be personal or project")
	}
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	inheritedURLs := ListInherits(workdir)
	existing, err := GetMemoItemByRef(memoRef, workspaceURL)
	if err != nil {
		return result.Err[Memo]("NOT_FOUND", "memo not found")
	}
	sourceTier := TierForRepoURL(existing.RepoURL, workspaceURL, inheritedURLs)
	srcRank := promoteRank(sourceTier)
	if srcRank < 0 {
		return result.Err[Memo]("INVALID_ARGS", string(sourceTier)+"-tier memos cannot be promoted")
	}
	if srcRank >= promoteRank(target) {
		return result.Err[Memo]("INVALID_ARGS", "promotion must move to a more durable tier")
	}
	memo := MemoItemToMemo(*existing, workspaceURL, inheritedURLs)
	repoPath, repoURL, branch, terr := ResolveTierTarget(target, workdir, workspaceURL)
	if terr != nil {
		return result.Err[Memo]("TIER_INIT_FAILED", terr.Error())
	}
	content := buildMemoContent(memo.Subject, memo.Body, CreateMemoOptions{
		Tier:   target,
		Labels: memo.Labels,
		Origin: existing.Origin,
	}, "")
	hash, err := git.CreateCommitOnBranch(repoPath, branch, content)
	if err != nil {
		return result.Err[Memo]("COMMIT_FAILED", err.Error())
	}
	if err := cacheMemoFromCommit(repoPath, repoURL, hash, branch); err != nil {
		return result.Err[Memo]("CACHE_FAILED", err.Error())
	}
	item, err := GetMemoItem(repoURL, hash, branch)
	if err != nil {
		return result.Err[Memo]("GET_FAILED", err.Error())
	}
	return result.Ok(MemoItemToMemo(*item, workspaceURL, inheritedURLs))
}
