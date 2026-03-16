// push.go - Push operations for branches and refs to remote
package gitmsg

import (
	"errors"
	"fmt"

	"github.com/gitsocial-org/gitsocial/core/git"
)

type PushResult struct {
	Commits int `json:"commits"`
	Refs    int `json:"refs"`
}

// Push pushes all extension branches and gitmsg refs to remote.
func Push(workdir string, dryRun bool) (*PushResult, error) {
	branches := GetExtBranches(workdir)
	result := &PushResult{}

	for _, branch := range branches {
		err := git.ValidatePushPreconditions(workdir, "origin", branch)
		if err != nil {
			if errors.Is(err, git.ErrDetachedHead) || errors.Is(err, git.ErrGitRemote) {
				return nil, err
			}
			continue
		}
		counts, err := GetUnpushedCounts(workdir, branch)
		if err != nil {
			continue
		}
		if counts.Posts > 0 {
			result.Commits += counts.Posts
			if !dryRun {
				if _, err := git.ExecGit(workdir, []string{"push", "origin", branch}); err != nil {
					return nil, fmt.Errorf("push %s: %w", branch, err)
				}
			}
		}
	}

	localRefs, err := getLocalGitMsgRefs(workdir)
	if err == nil && len(localRefs) > 0 {
		remoteRefs, err := getRemoteGitMsgRefs(workdir)
		if err != nil {
			remoteRefs = make(map[string]string)
		}
		for ref, localHash := range localRefs {
			if remoteHash, exists := remoteRefs[ref]; !exists || localHash != remoteHash {
				result.Refs++
			}
		}
		if result.Refs > 0 && !dryRun {
			if _, err := git.ExecGit(workdir, []string{
				"push", "origin", "refs/gitmsg/*:refs/gitmsg/*",
			}); err != nil {
				return nil, fmt.Errorf("push refs: %w", err)
			}
		}
	}

	return result, nil
}
