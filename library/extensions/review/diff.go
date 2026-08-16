// diff.go - Cross-repository diff resolution for pull requests
package review

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/storage"
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
	// Populating and reading the fork repo, as a unit: a fork repo whose borrowed
	// objects went missing is rebuilt and run through this a second time.
	resolve := func(dir string) (DiffContext, bool) {
		errs := make([]error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if baseLocal {
				errs[0] = lendWorkspaceBranch(dir, workdir, baseBranch)
				if !headLocal {
					// Best effort: an unreachable upstream just leaves the base
					// resolving against the borrowed workspace branch.
					errs[0] = errors.Join(errs[0], fetchFromUpstream(dir, wsURL, baseBranch))
				}
			} else {
				errs[0] = fetchFromUpstream(dir, baseParsed.Repository, baseBranch)
			}
		}()
		go func() {
			defer wg.Done()
			if headLocal {
				errs[1] = lendWorkspaceBranch(dir, workdir, headBranch)
			} else {
				errs[1] = fetchFromUpstream(dir, headParsed.Repository, headBranch)
			}
		}()
		wg.Wait()
		ctx := DiffContext{Workdir: dir}
		broken := storage.IsMissingObjectError(errors.Join(errs...))
		if baseLocal {
			ctx.Base = "refs/workspace/" + baseBranch
			if !headLocal {
				upstreamRef := "refs/fork/" + urlHash(wsURL) + "/" + baseBranch
				if _, err := git.ReadRef(dir, upstreamRef); err == nil {
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
		if ok, objectMissing := refResolves(dir, ctx.Base); !ok {
			missing = append(missing, fmt.Sprintf("base branch %q", baseBranch))
			ctx.Base = ""
			broken = broken || objectMissing
		}
		if ok, objectMissing := refResolves(dir, ctx.Head); !ok {
			missing = append(missing, fmt.Sprintf("head branch %q", headBranch))
			ctx.Head = ""
			broken = broken || objectMissing
		}
		if len(missing) > 0 {
			ctx.Error = "Could not fetch " + strings.Join(missing, " and ")
		}
		return ctx, broken
	}
	ctx, broken := resolve(forkDir)
	if !broken {
		return ctx
	}
	// The borrowed workspace ODB moved or was gc'd, or the fork repo's own objects
	// were pruned. The borrower is a disposable cache, so rebuild it and retry once.
	repaired, repairErr := storage.RepairForkRepository(cacheDir, forkKey)
	if repairErr != nil {
		log.Debug("fork repo repair failed", "dir", forkDir, "error", repairErr)
		return ctx
	}
	forgetFetchedRefs(forkDir)
	ctx, _ = resolve(repaired)
	return ctx
}

// refResolves reports whether a ref names an object the repo can read. git
// resolves a ref from its ref file alone, so an object a donor gc'd out from
// under a borrowed alternate only surfaces on the object read — reported
// separately so the caller can rebuild instead of blaming the branch.
func refResolves(dir, ref string) (ok bool, objectMissing bool) {
	sha, err := git.ReadRef(dir, ref)
	if err != nil || sha == "" {
		return false, false
	}
	if _, err := git.ExecGit(dir, []string{"cat-file", "-e", sha}); err != nil {
		return false, true
	}
	return true, false
}

// forgetFetchedRefs drops the memoized fetches for a fork repo, so a rebuilt repo
// is populated again instead of being considered already fetched.
func forgetFetchedRefs(forkDir string) {
	prefix := forkDir + "\x00"
	fetchedRefs.Range(func(key, _ any) bool {
		if name, ok := key.(string); ok && strings.HasPrefix(name, prefix) {
			fetchedRefs.Delete(key)
		}
		return true
	})
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
func fetchFromUpstream(forkDir, repoURL, branch string) error {
	key := forkDir + "\x00" + repoURL + "\x00" + branch
	if _, ok := fetchedRefs.Load(key); ok {
		return nil
	}
	hash := urlHash(repoURL)
	remoteName := "remote-" + hash
	if _, err := git.ExecGit(forkDir, []string{"remote", "add", remoteName, repoURL}); err != nil {
		log.Debug("add fork remote (may already exist)", "remote", remoteName, "error", err)
	}
	refspec := fmt.Sprintf("+refs/heads/%s:refs/fork/%s/%s", branch, hash, branch)
	if _, err := git.ExecGit(forkDir, []string{"fetch", remoteName, refspec, "--no-tags"}); err != nil {
		return fmt.Errorf("fetch %s from %s: %w", branch, repoURL, err)
	}
	fetchedRefs.Store(key, true)
	return nil
}

// lendWorkspaceBranch makes a workspace branch resolvable in the fork repo. A
// full-clone workspace lends its whole object database through an alternate and
// only the branch tip is written as a ref, so no objects are copied; a blobless
// workspace cannot lend objects, so its branch is fetched as before.
func lendWorkspaceBranch(forkDir, workdir, branch string) error {
	if storage.IsPartialClone(workdir) {
		return fetchFromWorkspace(forkDir, workdir, branch)
	}
	if err := storage.SetAlternate(forkDir, workdir); err != nil {
		log.Debug("borrowing workspace objects failed, fetching instead", "workdir", workdir, "error", err)
		return fetchFromWorkspace(forkDir, workdir, branch)
	}
	tip := workspaceTip(workdir, branch)
	if tip == "" {
		return fetchFromWorkspace(forkDir, workdir, branch)
	}
	if _, err := git.ExecGit(forkDir, []string{"update-ref", "refs/workspace/" + branch, tip}); err != nil {
		return fmt.Errorf("write workspace ref %s: %w", branch, err)
	}
	return nil
}

// workspaceTip resolves a branch to a full sha in the workspace, falling back to
// the origin tracking ref when the branch was never checked out locally.
func workspaceTip(workdir, branch string) string {
	for _, ref := range []string{"refs/heads/" + branch, "refs/remotes/origin/" + branch} {
		result, err := git.ExecGit(workdir, []string{"rev-parse", "--verify", "--quiet", ref})
		if err == nil && strings.TrimSpace(result.Stdout) != "" {
			return strings.TrimSpace(result.Stdout)
		}
	}
	return ""
}

// fetchFromWorkspace fetches a branch from the local workspace into refs/workspace/.
// Falls back to remote tracking ref (refs/remotes/origin/<branch>) when the local
// branch doesn't exist, which is common when the branch was never checked out.
func fetchFromWorkspace(forkDir, workdir, branch string) error {
	key := forkDir + "\x00" + workdir + "\x00" + branch
	if _, ok := fetchedRefs.Load(key); ok {
		return nil
	}
	refspec := fmt.Sprintf("+refs/heads/%s:refs/workspace/%s", branch, branch)
	if _, err := git.ExecGit(forkDir, []string{"fetch", workdir, refspec, "--no-tags"}); err == nil {
		fetchedRefs.Store(key, true)
		return nil
	}
	// Fallback: try remote tracking ref
	refspec = fmt.Sprintf("+refs/remotes/origin/%s:refs/workspace/%s", branch, branch)
	if _, err := git.ExecGit(forkDir, []string{"fetch", workdir, refspec, "--no-tags"}); err != nil {
		return fmt.Errorf("fetch %s from workspace: %w", branch, err)
	}
	fetchedRefs.Store(key, true)
	return nil
}

// ResolvePRDiff resolves the full diff range for a pull request.
// Handles single-commit mode, fork fetching, merged PR state, SHA pinning,
// and merge-base. Cross-fork PRs pin only the head (base resolves through
// upstream fetch + merge-base); workspace PRs pin both sides when the
// stored tips are reachable. The single applyPinPolicy helper covers both.
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
	if pr.State == PRStateMerged {
		resolveMergedDiff(&ctx, workdir, pr)
	} else {
		isForkPR := ctx.Workdir != workdir
		applyPinPolicy(&ctx, workdir, pr, isForkPR)
	}
	if ctx.Base == "" || ctx.Head == "" {
		return ctx
	}
	if mb, err := git.GetMergeBase(ctx.Workdir, ctx.Base, ctx.Head); err == nil {
		ctx.Base = mb
	}
	return ctx
}

// applyPinPolicy pins diff refs to the PR's stored tips when reachable. Both
// tips are pinned together whenever they resolve in the same directory, so the
// diff is exactly base-tip..head-tip (collapsed to their merge-base by the
// caller) and stays identical across workspaces (base repo and fork) rather
// than re-diffing against each workspace's live branch heads. Cross-fork PRs
// resolve their tips in the fork bare repo; workspace PRs may also resolve in
// the workspace checkout. If the base tip is missing but the head tip resolves,
// pin the head only and let the base fall back to the fetched branch.
func applyPinPolicy(ctx *DiffContext, workdir string, pr *PullRequest, isForkPR bool) {
	if pr.HeadTip == "" {
		return
	}
	// Fork PRs live only in the fork bare repo; workspace PRs may also resolve
	// in the workspace checkout.
	dirs := []string{ctx.Workdir}
	if !isForkPR {
		dirs = append(dirs, workdir)
	}
	if pr.BaseTip != "" {
		for _, dir := range dirs {
			if _, err := git.ReadRef(dir, pr.BaseTip); err != nil {
				continue
			}
			if _, err := git.ReadRef(dir, pr.HeadTip); err != nil {
				continue
			}
			ctx.Base = pr.BaseTip
			ctx.Head = pr.HeadTip
			ctx.Workdir = dir
			return
		}
	}
	for _, dir := range dirs {
		if _, err := git.ReadRef(dir, pr.HeadTip); err == nil {
			ctx.Head = pr.HeadTip
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
