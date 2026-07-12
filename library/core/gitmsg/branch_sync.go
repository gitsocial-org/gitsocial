// branch_sync.go - Push/fetch gitmsg/* branches with auto-merge on divergence.
//
// Every gitmsg extension branch (gitmsg/social, gitmsg/pm, gitmsg/review,
// gitmsg/release, gitmsg/memo, ...) is empty-tree and append-only by protocol
// convention — see core/git CreateCommitOnBranch / CreateCommitTree. That
// means divergent histories on a `gitmsg/*` branch have no conflict surface:
// a merge commit whose tree is the empty tree and whose parents are both tips
// preserves every commit from both sides, and the merge commit itself has no
// extension header so extension processors ignore it.
//
// FetchAndMergeBranch and PushBranchWithMerge automate that merge so naive
// `git push origin <branch>` no longer fails non-fast-forward when two
// clones write between syncs. They're written to be reusable across every
// gitmsg/* caller: memo personal/session sync today; gitmsg.Push and
// `gitsocial personal sync` as follow-ups.
package gitmsg

import (
	"errors"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// FetchAndMergeBranch fetches branch from the push remote (origin, or the
// configured s3 remote) into the local branch ref, creating an empty-tree
// merge commit when local and remote have diverged. Idempotent and safe to
// call on a fresh local branch (the remote tip is adopted directly). Suitable
// for any gitmsg/* branch.
func FetchAndMergeBranch(repoPath, branch string) error {
	return FetchAndMergeBranchTo(repoPath, git.PushRemote(repoPath), branch)
}

// FetchAndMergeBranchTo is FetchAndMergeBranch against an explicit remote. The
// gitsocial-push path threads its resolved remote through here so a
// non-fast-forward retry merges against the same target it pushed to.
func FetchAndMergeBranchTo(repoPath, remote, branch string) error {
	if _, err := git.ExecGit(repoPath, []string{"fetch", remote, branch}); err != nil {
		return err
	}
	remoteTip := fullHash(repoPath, "FETCH_HEAD")
	if remoteTip == "" {
		return nil
	}
	localRef := "refs/heads/" + branch
	localTip := fullHash(repoPath, localRef)

	switch localTip {
	case "":
		return git.WriteRef(repoPath, localRef, remoteTip)
	case remoteTip:
		return nil
	}

	base, err := git.GetMergeBase(repoPath, localTip, remoteTip)
	switch {
	case err != nil:
		// No common ancestor — independent histories. Merge them with both as parents.
		return writeMergeCommit(repoPath, localRef, branch, localTip, remoteTip)
	case base == localTip:
		// Fast-forward.
		return git.WriteRef(repoPath, localRef, remoteTip)
	case base == remoteTip:
		// Local is ahead; nothing to pull.
		return nil
	default:
		// Divergence.
		return writeMergeCommit(repoPath, localRef, branch, localTip, remoteTip)
	}
}

// PushBranchWithMerge pushes branch to the push remote. On a non-fast-forward
// rejection, runs FetchAndMergeBranch and retries once. Any non-FF failure on
// the retry surfaces as an error so the caller can report it.
func PushBranchWithMerge(repoPath, branch string) error {
	return PushBranchWithMergeTo(repoPath, git.PushRemote(repoPath), branch)
}

// PushBranchWithMergeTo is PushBranchWithMerge against an explicit remote. The
// gitsocial-push path threads its resolved remote through here so the push and
// its non-FF merge-retry target the same remote.
func PushBranchWithMergeTo(repoPath, remote, branch string) error {
	_, err := git.ExecGit(repoPath, []string{"push", remote, branch})
	if err == nil {
		return nil
	}
	if !isNonFastForward(err) {
		return err
	}
	if mergeErr := FetchAndMergeBranchTo(repoPath, remote, branch); mergeErr != nil {
		return mergeErr
	}
	_, err = git.ExecGit(repoPath, []string{"push", remote, branch})
	return err
}

// writeMergeCommit creates an empty-tree merge commit with the given parents
// and points branchRef at it.
func writeMergeCommit(repoPath, branchRef, branchLabel, localTip, remoteTip string) error {
	args := []string{
		"commit-tree", git.EmptyTreeHash,
		"-m", "merge " + branchLabel + " (auto-merged on append-only sync)",
		"-p", localTip,
		"-p", remoteTip,
	}
	out, err := git.ExecGit(repoPath, args)
	if err != nil {
		return err
	}
	mergeHash := strings.TrimSpace(out.Stdout)
	if mergeHash == "" {
		return errors.New("commit-tree returned empty hash")
	}
	return git.WriteRef(repoPath, branchRef, mergeHash)
}

// fullHash returns the full (un-abbreviated) commit hash a ref resolves to, or
// "" if the ref doesn't exist. Needed so hash comparisons inside
// FetchAndMergeBranch don't mix `--short` and full forms (git.ReadRef
// abbreviates to 12 chars).
func fullHash(repoPath, ref string) string {
	out, err := git.ExecGit(repoPath, []string{"rev-parse", "--verify", "--quiet", ref})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out.Stdout)
}

// isNonFastForward detects git's non-FF push rejection. Git wording varies
// across versions but always includes one of these phrases on stderr.
func isNonFastForward(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "non-fast-forward") ||
		strings.Contains(msg, "fetch first") ||
		strings.Contains(msg, "rejected")
}
