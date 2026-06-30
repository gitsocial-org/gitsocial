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

type PushPreview struct {
	Branches []BranchPushCount `json:"branches"`
	Refs     int               `json:"refs"`
}

type BranchPushCount struct {
	Branch  string `json:"branch"`
	Commits int    `json:"commits"`
}

// TotalCommits returns the total number of unpushed commits across all branches.
func (p *PushPreview) TotalCommits() int {
	n := 0
	for _, b := range p.Branches {
		n += b.Commits
	}
	return n
}

// IsEmpty reports whether the preview has nothing to push.
func (p *PushPreview) IsEmpty() bool {
	return p.Refs == 0 && p.TotalCommits() == 0
}

// GetPushPreview enumerates branches and refs that would be pushed without
// touching the remote. Mirrors Push's validation/counting logic so the
// breakdown matches what Push would actually do.
func GetPushPreview(workdir string) (*PushPreview, error) {
	preview := &PushPreview{}

	for _, branch := range GetExtBranches(workdir) {
		err := git.ValidatePushPreconditions(workdir, "origin", branch)
		if err != nil {
			if errors.Is(err, git.ErrDetachedHead) || errors.Is(err, git.ErrGitRemote) {
				return nil, err
			}
			if !errors.Is(err, git.ErrDiverged) {
				continue
			}
		}
		counts, err := GetUnpushedCounts(workdir, branch)
		if err != nil || counts.Posts == 0 {
			continue
		}
		preview.Branches = append(preview.Branches, BranchPushCount{Branch: branch, Commits: counts.Posts})
	}

	localRefs, err := getLocalGitMsgRefs(workdir)
	if err == nil && len(localRefs) > 0 {
		remoteRefs, err := getRemoteGitMsgRefs(workdir)
		if err != nil {
			remoteRefs = make(map[string]string)
		}
		for ref, localHash := range localRefs {
			if remoteHash, exists := remoteRefs[ref]; !exists || localHash != remoteHash {
				preview.Refs++
			}
		}
	}

	return preview, nil
}

// Push pushes all extension branches and gitmsg refs to remote. Extension
// branches go through PushBranchWithMerge so divergent histories auto-resolve
// (empty-tree append-only branches → conflict-free merge) instead of failing
// non-fast-forward and dropping the user into raw git.
func Push(workdir string, dryRun bool) (*PushResult, error) {
	preview, err := GetPushPreview(workdir)
	if err != nil {
		return nil, err
	}
	result := &PushResult{Refs: preview.Refs}

	for _, bp := range preview.Branches {
		result.Commits += bp.Commits
		if !dryRun {
			if err := PushBranchWithMerge(workdir, bp.Branch); err != nil {
				return nil, fmt.Errorf("push %s: %w", bp.Branch, err)
			}
		}
	}

	if preview.Refs > 0 && !dryRun {
		if _, err := git.ExecGit(workdir, []string{
			"push", "origin", "refs/gitmsg/*:refs/gitmsg/*",
		}); err != nil {
			return nil, wrapStateRefPushError(err)
		}
		// The just-pushed state refs now match the remote; mirror them into the
		// remote-tracking namespace so the next offline push preview doesn't
		// re-report them as unpushed before the following fetch.
		mirrorGitMsgRefsToTracking(workdir)
	}

	return result, nil
}

// mirrorGitMsgRefsToTracking points each local refs/gitmsg/* ref's remote-tracking
// counterpart (refs/remotes/origin/gitmsg/*) at the local hash. Called after a
// push so the push preview reflects the new remote state without a fetch.
func mirrorGitMsgRefsToTracking(workdir string) {
	localRefs, err := getLocalGitMsgRefs(workdir)
	if err != nil {
		return
	}
	for ref, hash := range localRefs {
		tracking := "refs/remotes/origin/" + strings.TrimPrefix(ref, "refs/")
		if err := git.WriteRef(workdir, tracking, hash); err != nil {
			// Best-effort: a stale preview is harmless, so don't fail the push.
			continue
		}
	}
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
