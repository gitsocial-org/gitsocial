// publish.go - Push code branches referenced by published review data
package review

import (
	"fmt"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// CodeBranchesToPush returns the workspace code branches `gitsocial push`
// publishes alongside gitmsg data: heads of open PRs with unpushed commits,
// plus the default branch when it's ahead of remote ("" resolves via
// git.PushRemote). The default branch is the published face of the repository,
// so it travels with pushes even when no PR references it. Returns nil when
// the workspace has no remote.
func CodeBranchesToPush(workdir, remote string) (map[string]int, error) {
	if git.GetOriginURL(workdir) == "" {
		return nil, nil
	}
	branches, err := UnpushedHeadBranches(workdir, remote)
	if branches == nil {
		branches = make(map[string]int)
	}
	if def := defaultBranch(workdir); def != "" {
		if _, ok := branches[def]; !ok {
			if unpushed, uerr := git.UnpushedOnBranch(workdir, def, remote); uerr == nil && len(unpushed) > 0 {
				branches[def] = len(unpushed)
			}
		}
	}
	if len(branches) == 0 {
		return nil, err
	}
	return branches, err
}

// defaultBranch resolves the repository's default branch: origin's HEAD when
// known, else main/master if one exists locally. Deliberately not HEAD — the
// checked-out branch may be unannounced feature work.
func defaultBranch(workdir string) string {
	if out, err := git.ExecGit(workdir, []string{"symbolic-ref", "--short", "refs/remotes/origin/HEAD"}); err == nil {
		return strings.TrimPrefix(strings.TrimSpace(out.Stdout), "origin/")
	}
	for _, name := range []string{"main", "master"} {
		if git.BranchExists(workdir, name) {
			return name
		}
	}
	return ""
}

// PushMergedBase pushes a merged PR's base branch to the push remote so the
// remote code catches up with the merged state published on gitmsg/review.
// No-op when the workspace has no remote. Merge callers treat a failure as a
// warning: the merge itself already succeeded locally.
func PushMergedBase(workdir string, pr PullRequest) error {
	if git.GetOriginURL(workdir) == "" {
		return nil
	}
	parsed := protocol.ParseRef(pr.Base)
	if parsed.Type != protocol.RefTypeBranch || parsed.Value == "" {
		return fmt.Errorf("base ref %q is not a branch", pr.Base)
	}
	if _, err := git.ExecGit(workdir, []string{"push", git.PushRemote(workdir), parsed.Value}); err != nil {
		return fmt.Errorf("push %s: %w", parsed.Value, err)
	}
	return nil
}
