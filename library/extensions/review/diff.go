// diff.go - Cross-repository diff resolution for pull requests
package review

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/storage"
)

// DiffContext holds resolved parameters for git diff operations.
type DiffContext struct {
	Workdir string // git repo to run operations in (workspace or fork bare repo)
	Base    string // resolved git ref for base
	Head    string // resolved git ref for head
	Error   string // non-empty when diff resolution failed
}

var fetchedRefs sync.Map // "forkDir\x00remote\x00branch" → true

// ResolveDiffContext resolves PR base/head refs for git operations.
// For local-only PRs, returns the workspace. For remote refs, fetches
// both sides into a fork bare repo and returns that path.
func ResolveDiffContext(workdir, cacheDir, baseRef, headRef string) DiffContext {
	baseParsed := protocol.ParseRef(baseRef)
	headParsed := protocol.ParseRef(headRef)
	baseLocal := baseParsed.Repository == ""
	headLocal := headParsed.Repository == ""
	// Refs matching the workspace URL are effectively local
	wsURL := gitmsg.ResolveRepoURL(workdir)
	if !baseLocal && baseParsed.Repository == wsURL {
		baseLocal = true
	}
	if !headLocal && headParsed.Repository == wsURL {
		headLocal = true
	}
	baseBranch := branchValue(baseParsed, baseRef)
	headBranch := branchValue(headParsed, headRef)
	if baseLocal && headLocal {
		return DiffContext{Workdir: workdir, Base: resolveLocalRef(workdir, baseBranch), Head: resolveLocalRef(workdir, headBranch)}
	}
	// At least one side is remote — use a fork bare repo keyed by the base repo
	// (workspace URL when base is local, otherwise the base's repository URL).
	// This isolates each repo's fork data for easy cleanup.
	forkKey := wsURL
	if !baseLocal {
		forkKey = baseParsed.Repository
	}
	forkDir, err := storage.EnsureForkRepository(cacheDir, forkKey)
	if err != nil {
		return DiffContext{Workdir: workdir, Base: baseBranch, Head: headBranch}
	}
	ctx := DiffContext{Workdir: forkDir}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if baseLocal {
			fetchFromWorkspace(forkDir, workdir, baseBranch)
			if !headLocal {
				fetchFromUpstream(forkDir, wsURL, baseBranch)
			}
		} else {
			fetchFromUpstream(forkDir, baseParsed.Repository, baseBranch)
		}
	}()
	go func() {
		defer wg.Done()
		if headLocal {
			fetchFromWorkspace(forkDir, workdir, headBranch)
		} else {
			fetchFromUpstream(forkDir, headParsed.Repository, headBranch)
		}
	}()
	wg.Wait()
	if baseLocal {
		ctx.Base = "refs/workspace/" + baseBranch
		if !headLocal {
			upstreamRef := "refs/fork/" + urlHash(wsURL) + "/" + baseBranch
			if _, err := git.ReadRef(forkDir, upstreamRef); err == nil {
				ctx.Base = upstreamRef
			}
		}
	} else {
		ctx.Base = "refs/fork/" + urlHash(baseParsed.Repository) + "/" + baseBranch
	}
	if headLocal {
		ctx.Head = "refs/workspace/" + headBranch
	} else {
		ctx.Head = "refs/fork/" + urlHash(headParsed.Repository) + "/" + headBranch
	}
	var missing []string
	if _, err := git.ReadRef(forkDir, ctx.Base); err != nil {
		missing = append(missing, fmt.Sprintf("base branch %q", baseBranch))
		ctx.Base = ""
	}
	if _, err := git.ReadRef(forkDir, ctx.Head); err != nil {
		missing = append(missing, fmt.Sprintf("head branch %q", headBranch))
		ctx.Head = ""
	}
	if len(missing) > 0 {
		ctx.Error = "Could not fetch " + strings.Join(missing, " and ")
	}
	return ctx
}

// resolveLocalRef verifies a branch name resolves as a git ref.
// Falls back to remote tracking branch (e.g. origin/feature) when
// the local branch doesn't exist, which is common after git clone.
func resolveLocalRef(workdir, branch string) string {
	if _, err := git.ExecGit(workdir, []string{"rev-parse", "--verify", "--quiet", branch}); err == nil {
		return branch
	}
	result, err := git.ExecGit(workdir, []string{"for-each-ref", "--format=%(refname:short)", "refs/remotes/*/" + branch, "--count=1"})
	if err == nil && strings.TrimSpace(result.Stdout) != "" {
		return strings.TrimSpace(result.Stdout)
	}
	return branch
}

// branchValue extracts the branch name from a parsed ref or raw string.
func branchValue(parsed protocol.ParsedRef, raw string) string {
	if parsed.Type == protocol.RefTypeBranch {
		return parsed.Value
	}
	return raw
}

// fetchFromUpstream fetches a branch from a remote URL into namespaced refs.
func fetchFromUpstream(forkDir, repoURL, branch string) {
	key := forkDir + "\x00" + repoURL + "\x00" + branch
	if _, ok := fetchedRefs.Load(key); ok {
		return
	}
	hash := urlHash(repoURL)
	remoteName := "remote-" + hash
	if _, err := git.ExecGit(forkDir, []string{"remote", "add", remoteName, repoURL}); err != nil {
		log.Debug("add fork remote (may already exist)", "remote", remoteName, "error", err)
	}
	refspec := fmt.Sprintf("+refs/heads/%s:refs/fork/%s/%s", branch, hash, branch)
	if _, err := git.ExecGit(forkDir, []string{"fetch", remoteName, refspec, "--no-tags"}); err == nil {
		fetchedRefs.Store(key, true)
	}
}

// fetchFromWorkspace fetches a branch from the local workspace into refs/workspace/.
// Falls back to remote tracking ref (refs/remotes/origin/<branch>) when the local
// branch doesn't exist, which is common when the branch was never checked out.
func fetchFromWorkspace(forkDir, workdir, branch string) {
	key := forkDir + "\x00" + workdir + "\x00" + branch
	if _, ok := fetchedRefs.Load(key); ok {
		return
	}
	refspec := fmt.Sprintf("+refs/heads/%s:refs/workspace/%s", branch, branch)
	if _, err := git.ExecGit(forkDir, []string{"fetch", workdir, refspec, "--no-tags"}); err == nil {
		fetchedRefs.Store(key, true)
		return
	}
	// Fallback: try remote tracking ref
	refspec = fmt.Sprintf("+refs/remotes/origin/%s:refs/workspace/%s", branch, branch)
	if _, err := git.ExecGit(forkDir, []string{"fetch", workdir, refspec, "--no-tags"}); err == nil {
		fetchedRefs.Store(key, true)
	}
}

// ResolvePRDiff resolves the full diff range for a pull request.
// Handles single-commit mode, fork fetching, merged PR state, SHA pinning, and merge-base.
func ResolvePRDiff(workdir, cacheDir string, pr *PullRequest, commit string) DiffContext {
	baseRef, headRef := qualifyPRRefs(workdir, pr)
	ctx := ResolveDiffContext(workdir, cacheDir, baseRef, headRef)
	if commit != "" {
		dir := ctx.Workdir
		if _, err := git.ReadRef(dir, commit); err != nil {
			dir = workdir
		}
		return DiffContext{Workdir: dir, Base: commit + "^", Head: commit}
	}
	isForkPR := ctx.Workdir != workdir
	if pr.State == PRStateMerged {
		resolveMergedDiff(&ctx, workdir, pr)
	} else if isForkPR {
		// Only pin head — base is resolved via upstream fetch + merge-base
		if pr.HeadTip != "" {
			if _, err := git.ReadRef(ctx.Workdir, pr.HeadTip); err == nil {
				ctx.Head = pr.HeadTip
			}
		}
	} else {
		pinToCommitSHAs(&ctx, workdir, pr.BaseTip, pr.HeadTip)
	}
	if ctx.Base == "" || ctx.Head == "" {
		return ctx
	}
	if mb, err := git.GetMergeBase(ctx.Workdir, ctx.Base, ctx.Head); err == nil {
		ctx.Base = mb
	}
	return ctx
}

// pinToCommitSHAs pins diff refs to platform-provided commit SHAs when resolvable.
func pinToCommitSHAs(ctx *DiffContext, workdir, baseTip, headTip string) {
	if headTip == "" {
		return
	}
	dirs := []string{ctx.Workdir, workdir}
	if baseTip != "" {
		for _, dir := range dirs {
			if _, err := git.ReadRef(dir, baseTip); err == nil {
				if _, err := git.ReadRef(dir, headTip); err == nil {
					ctx.Base = baseTip
					ctx.Head = headTip
					ctx.Workdir = dir
					return
				}
			}
		}
	}
	for _, dir := range dirs {
		if _, err := git.ReadRef(dir, headTip); err == nil {
			ctx.Head = headTip
			ctx.Workdir = dir
			return
		}
	}
}

// resolveMergedDiff resolves diff refs for merged PRs using stored merge-base/merge-head.
func resolveMergedDiff(ctx *DiffContext, workdir string, pr *PullRequest) {
	hash := protocol.ParseRef(pr.ID).Value
	info, err := GetStateChangeInfo(pr.Repository, hash, pr.Branch, PRStateMerged)
	if err != nil {
		log.Debug("GetStateChangeInfo failed for merged PR", "hash", hash, "error", err)
		return
	}
	mBase, mHead := info.MergeBase, info.MergeHead
	if mBase == "" {
		mBase = pr.BaseTip
	}
	if mHead == "" {
		mHead = pr.HeadTip
	}
	if mBase == "" || mHead == "" {
		return
	}
	for _, dir := range []string{workdir, ctx.Workdir} {
		if _, err := git.ReadRef(dir, mBase); err != nil {
			continue
		}
		if _, err := git.ReadRef(dir, mHead); err != nil {
			continue
		}
		ctx.Base = mBase
		ctx.Head = mHead
		ctx.Workdir = dir
		ctx.Error = ""
		return
	}
	log.Debug("could not resolve merged diff refs in any directory", "mergeBase", mBase, "mergeHead", mHead)
}

// qualifyPRRefs resolves relative refs in a PR to absolute refs when the PR
// originates from a different repository than the workspace.
func qualifyPRRefs(workdir string, pr *PullRequest) (baseRef, headRef string) {
	baseRef, headRef = pr.Base, pr.Head
	if pr.Repository == "" {
		return
	}
	wsURL := gitmsg.ResolveRepoURL(workdir)
	prURL := protocol.NormalizeURL(pr.Repository)
	if prURL == "" || prURL == wsURL {
		return
	}
	baseParsed := protocol.ParseRef(baseRef)
	if baseParsed.Repository == "" && baseParsed.Type == protocol.RefTypeBranch {
		baseRef = prURL + baseRef
	}
	headParsed := protocol.ParseRef(headRef)
	if headParsed.Repository == "" && headParsed.Type == protocol.RefTypeBranch {
		headRef = prURL + headRef
	}
	return
}

// urlHash returns a short hash for differentiating remote names.
func urlHash(url string) string {
	h := uint32(0)
	for _, c := range url {
		h = h*31 + uint32(c)
	}
	return fmt.Sprintf("%08x", h)
}
