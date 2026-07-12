// push.go - Push operations for branches and refs to remote
package gitmsg

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

type PushResult struct {
	Commits     int    `json:"commits"`
	CodeCommits int    `json:"codeCommits"`
	Refs        int    `json:"refs"`
	Tags        int    `json:"tags"`
	AllBranches int    `json:"allBranches"`
	Remote      string `json:"remote"`
	RemoteURL   string `json:"remoteUrl"`
}

type PushPreview struct {
	Branches []BranchPushCount `json:"branches"`
	Code     []BranchPushCount `json:"code,omitempty"`
	// All lists local non-gitmsg branches that --all publishes beyond the
	// reason-based set (default/PR-head branches already in Code). Empty unless
	// --all was requested. Surfaced so dry-run and the TUI prompt name them.
	All  []string `json:"all,omitempty"`
	Refs int      `json:"refs"`
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
	return p.Refs == 0 && p.TotalCommits() == 0 && len(p.Code) == 0 && len(p.All) == 0
}

// GetPushPreview enumerates branches and refs that would be pushed without
// touching the remote. Mirrors Push's validation/counting logic so the
// breakdown matches what Push would actually do. codeBranches maps workspace
// code branches referenced by published data (open PR heads) to their
// unpushed-commit counts; callers resolve it (e.g. review.UnpushedHeadBranches)
// because this package can't depend on extensions. remote is the target to
// count against; "" resolves via git.PushRemote.
//
// Note: IsEmpty can be true even when a push would still publish new/moved
// tags — git keeps no remote tag-tracking state, so tags are uncountable
// offline. Callers must therefore still offer a push on an empty preview;
// Push runs the tags push unconditionally (see PushWithProgress).
//
// allBranches expands the set to every local non-gitmsg branch (refs/heads/*),
// beyond the reason-based codeBranches: those extras land in preview.All so the
// dry-run and TUI prompt can name them.
func GetPushPreview(workdir string, codeBranches map[string]int, remote string, allBranches bool) (*PushPreview, error) {
	preview := &PushPreview{}
	if remote == "" {
		remote = git.PushRemote(workdir)
	}

	for _, branch := range GetExtBranches(workdir) {
		err := git.ValidatePushPreconditions(workdir, remote, branch)
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
		remoteRefs, err := getRemoteGitMsgRefs(workdir, remote)
		if err != nil {
			remoteRefs = make(map[string]string)
		}
		for ref, localHash := range localRefs {
			if remoteHash, exists := remoteRefs[ref]; !exists || localHash != remoteHash {
				preview.Refs++
			}
		}
	}

	names := make([]string, 0, len(codeBranches))
	for b := range codeBranches {
		names = append(names, b)
	}
	sort.Strings(names)
	for _, b := range names {
		preview.Code = append(preview.Code, BranchPushCount{Branch: b, Commits: codeBranches[b]})
	}

	if allBranches {
		preview.All = extraAllBranches(workdir, codeBranches)
	}

	return preview, nil
}

// extraAllBranches returns local non-gitmsg branches that --all publishes beyond
// the reason-based code set (already covered by codeBranches). Sorted for a
// stable preview.
func extraAllBranches(workdir string, codeBranches map[string]int) []string {
	locals, err := git.ListLocalBranches(workdir)
	if err != nil {
		return nil
	}
	extra := make([]string, 0, len(locals))
	for _, b := range locals {
		if _, already := codeBranches[b]; already {
			continue
		}
		extra = append(extra, b)
	}
	sort.Strings(extra)
	return extra
}

// Push pushes all extension branches, gitmsg refs, and the given code branches
// to remote (empty = resolve via git.PushRemote: origin, or the configured s3
// remote). Extension branches go through PushBranchWithMergeTo so divergent
// histories auto-resolve (empty-tree append-only branches → conflict-free
// merge) instead of failing non-fast-forward and dropping the user into raw
// git. Code branches (open PR heads, resolved by the caller) are pushed first
// — plain push, no auto-merge — so the gitmsg/review push only publishes PRs
// whose head is reachable on the remote.
func Push(workdir string, dryRun bool, codeBranches map[string]int, remote string, allBranches bool) (*PushResult, error) {
	return PushWithProgress(workdir, dryRun, codeBranches, remote, allBranches, nil)
}

// PushBranchProgress reports which branch is being pushed and its position in
// the batch (done, total), for a coarse per-branch UI indicator (the TUI status
// line). It is called just before each branch's push. nil = no reporting.
// The object/site-shard granularity below the branch push lives in the git
// remote helper's stderr (see core/objstore), which git relays to the terminal;
// callers that go through a subprocess (`gitsocial push`, `git push`) see that
// directly, so this callback stays coarse on purpose.
type PushBranchProgress func(branch string, done, total int)

// PushWithProgress is Push with a coarse per-branch progress callback. See Push.
// remote is the target ("" resolves via git.PushRemote). The tags push runs
// unconditionally even when the preview is empty (tags are uncountable offline),
// so a tags-only change is still published.
func PushWithProgress(workdir string, dryRun bool, codeBranches map[string]int, remote string, allBranches bool, onBranch PushBranchProgress) (*PushResult, error) {
	if remote == "" {
		remote = git.PushRemote(workdir)
	}
	preview, err := GetPushPreview(workdir, codeBranches, remote, allBranches)
	if err != nil {
		return nil, err
	}
	result := &PushResult{Refs: preview.Refs, Remote: remote, RemoteURL: git.RemoteURL(workdir, remote)}

	// One coarse step per branch push plus the tags push and, when state refs
	// have moved, the refs/gitmsg/* push.
	total := len(preview.Code) + len(preview.All) + len(preview.Branches) + 1
	if preview.Refs > 0 {
		total++
	}
	done := 0
	step := func(branch string) {
		done++
		if onBranch != nil {
			onBranch(branch, done, total)
		}
	}

	for _, bp := range preview.Code {
		result.CodeCommits += bp.Commits
		step(bp.Branch)
		if !dryRun {
			if _, err := git.ExecGit(workdir, []string{"push", remote, bp.Branch}); err != nil {
				return nil, wrapCodePushError(remote, bp.Branch, err)
			}
		}
	}

	// --all: publish every remaining local branch. Plain push, no auto-merge —
	// a non-FF here is reported per-branch (rebased/diverged branch), same rule
	// as code branches; the user reconciles explicitly.
	for _, branch := range preview.All {
		result.AllBranches++
		step(branch)
		if !dryRun {
			if _, err := git.ExecGit(workdir, []string{"push", remote, branch}); err != nil {
				return nil, wrapCodePushError(remote, branch, err)
			}
		}
	}

	step("tags")
	tags, err := pushTags(workdir, remote, dryRun)
	if err != nil {
		return nil, err
	}
	result.Tags = tags

	for _, bp := range preview.Branches {
		result.Commits += bp.Commits
		step(bp.Branch)
		if !dryRun {
			if err := PushBranchWithMergeTo(workdir, remote, bp.Branch); err != nil {
				return nil, fmt.Errorf("push %s: %w", bp.Branch, err)
			}
		}
	}

	if preview.Refs > 0 {
		step("refs/gitmsg/*")
		if !dryRun {
			if _, err := git.ExecGit(workdir, []string{
				"push", remote, "refs/gitmsg/*:refs/gitmsg/*",
			}); err != nil {
				return nil, wrapStateRefPushError(err)
			}
			// The just-pushed state refs now match the remote; mirror them into the
			// remote-tracking namespace so the next offline push preview doesn't
			// re-report them as unpushed before the following fetch.
			mirrorGitMsgRefsToTracking(workdir, remote)
		}
	}

	return result, nil
}

// RemoteIsEmpty reports whether the resolved remote advertises zero refs — the
// first-publish (bootstrap) case. One `git ls-remote <remote>` round trip (for
// s3 remotes this is a single refs listing through the helper). remote ""
// resolves via git.PushRemote. A remote that isn't configured, or a listing
// error, is treated as non-empty (false) so bootstrap never triggers on a bad
// probe; the push itself will surface any real remote error.
func RemoteIsEmpty(workdir, remote string) bool {
	if remote == "" {
		remote = git.PushRemote(workdir)
	}
	if _, err := git.ExecGit(workdir, []string{"remote", "get-url", remote}); err != nil {
		return false
	}
	out, err := git.ExecGit(workdir, []string{"ls-remote", remote})
	if err != nil {
		return false
	}
	return strings.TrimSpace(out.Stdout) == ""
}

// pushTags pushes all local tags to the remote and returns how many were
// accepted as new or moved. Runs with --porcelain so the count parses stably:
// one ref per line, "=" marking up-to-date tags (not counted); a rejected tag
// surfaces as a git error. --dry-run contacts the remote but updates nothing.
// A workspace without the remote configured is a no-op, like code branches.
func pushTags(workdir, remote string, dryRun bool) (int, error) {
	if _, err := git.ExecGit(workdir, []string{"remote", "get-url", remote}); err != nil {
		return 0, nil
	}
	args := []string{"push", remote, "--tags", "--porcelain"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	out, err := git.ExecGit(workdir, args)
	if err != nil {
		return 0, fmt.Errorf("push tags: %w", err)
	}
	count := 0
	for _, line := range strings.Split(out.Stdout, "\n") {
		if strings.HasPrefix(line, "=") || strings.HasPrefix(line, "!") {
			continue
		}
		if strings.Contains(line, "refs/tags/") {
			count++
		}
	}
	return count, nil
}

// mirrorGitMsgRefsToTracking points each local refs/gitmsg/* ref's remote-tracking
// counterpart (refs/remotes/<remote>/gitmsg/*) at the local hash. Called after a
// push so the push preview reflects the new remote state without a fetch.
func mirrorGitMsgRefsToTracking(workdir, remote string) {
	localRefs, err := getLocalGitMsgRefs(workdir)
	if err != nil {
		return
	}
	for ref, hash := range localRefs {
		tracking := "refs/remotes/" + remote + "/" + strings.TrimPrefix(ref, "refs/")
		if err := git.WriteRef(workdir, tracking, hash); err != nil {
			// Best-effort: a stale preview is harmless, so don't fail the push.
			continue
		}
	}
}

// wrapCodePushError contextualizes a failed code-branch push. Non-FF here
// usually means the PR head was rebased; code branches must never auto-merge,
// so point the user at an explicit force-with-lease instead (honored by s3
// remotes too via the helper's cas option). --force is the blunt fallback for
// the lease-can't-work case: a bare --force-with-lease needs a remote-tracking
// base, which a never-fetched branch on that remote doesn't have.
func wrapCodePushError(remote, branch string, err error) error {
	if isNonFastForward(err) {
		return fmt.Errorf("push %s: remote has diverged (rebased head?) — review and push manually with `git push --force-with-lease %s %s` (or --force as a last resort): %w", branch, remote, branch, err)
	}
	return fmt.Errorf("push %s: %w", branch, err)
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
