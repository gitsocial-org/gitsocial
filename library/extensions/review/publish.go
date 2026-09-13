// publish.go - Push code branches referenced by published review data
package review

import (
	"fmt"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// CodeBranchesToPush lists the code branches a push publishes: open pull request heads and the default branch.
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

// defaultBranch resolves origin's HEAD, else a local main or master. Not HEAD, which may be feature work.
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

// PushMergedBase pushes a merged pull request's base branch, so the remote code catches up.
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
