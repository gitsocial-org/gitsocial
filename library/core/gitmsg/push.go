// push.go - Push operations for branches and refs to remote
package gitmsg

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

type PushResult struct {
	Commits int `json:"commits"`
	Refs    int `json:"refs"`
}

// Push pushes all extension branches and gitmsg refs to remote. Extension
// branches go through PushBranchWithMerge so divergent histories auto-resolve
// (empty-tree append-only branches → conflict-free merge) instead of failing
// non-fast-forward and dropping the user into raw git.
func Push(workdir string, dryRun bool) (*PushResult, error) {
	branches := GetExtBranches(workdir)
	result := &PushResult{}

	for _, branch := range branches {
		err := git.ValidatePushPreconditions(workdir, "origin", branch)
		if err != nil {
			if errors.Is(err, git.ErrDetachedHead) || errors.Is(err, git.ErrGitRemote) {
				return nil, err
			}
			// Divergence is no longer a block — PushBranchWithMerge handles it.
			// Other recoverable errors (e.g., missing remote-tracking ref) still skip.
			if !errors.Is(err, git.ErrDiverged) {
				continue
			}
		}
		counts, err := GetUnpushedCounts(workdir, branch)
		if err != nil {
			continue
		}
		if counts.Posts > 0 {
			result.Commits += counts.Posts
			if !dryRun {
				if err := PushBranchWithMerge(workdir, branch); err != nil {
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
				return nil, wrapStateRefPushError(err)
			}
		}
	}

	return result, nil
}

// wrapStateRefPushError contextualizes a non-fast-forward rejection on the
// bulk refs/gitmsg/* push so users know it's a state-ref conflict (two clones
// wrote different content to the same per-element key) and not the kind of
// divergence PushBranchWithMerge handles.
func wrapStateRefPushError(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "non-fast-forward") || strings.Contains(msg, "fetch first") || strings.Contains(msg, "rejected") {
		return fmt.Errorf("push refs: state-ref conflict on refs/gitmsg/* (two clones wrote different content under the same key — fetch and reconcile manually): %w", err)
	}
	return fmt.Errorf("push refs: %w", err)
}
