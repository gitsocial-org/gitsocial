// diff.go - Cross-repository diff resolution for pull requests
package review

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/gitsocial-org/gitsocial/library/core/fetch"
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

// ResolveDiffContext returns the workspace for a local pull request, or the fork bare repo it fetched both sides into.
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
	// One side is remote, so a fork bare repo keyed by the base repository holds both.
	forkKey := wsURL
	if !baseLocal {
		forkKey = baseParsed.Repository
	}
	forkDir, err := storage.EnsureForkRepository(cacheDir, forkKey)
	if err != nil {
		return DiffContext{Workdir: workdir, Base: baseBranch, Head: headBranch}
	}
	// A registered fork is fetched from its address; an unregistered one from its identity.
	addresses := gitmsg.ForkAddresses(workdir)
	// Populate and read as a unit, so a repo whose borrowed objects went missing can be retried.
	resolve := func(dir string) (DiffContext, bool) {
		errs := make([]error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if baseLocal {
				errs[0] = lendWorkspaceBranch(dir, workdir, baseBranch)
				if !headLocal {
					// An unreachable upstream leaves the base on the borrowed workspace branch.
					errs[0] = errors.Join(errs[0], fetchFromUpstream(dir, wsURL, fetch.ForkAddress(addresses, wsURL), baseBranch))
				}
			} else {
				errs[0] = fetchFromUpstream(dir, baseParsed.Repository, fetch.ForkAddress(addresses, baseParsed.Repository), baseBranch)
			}
		}()
		go func() {
			defer wg.Done()
			if headLocal {
				errs[1] = lendWorkspaceBranch(dir, workdir, headBranch)
			} else {
				errs[1] = fetchFromUpstream(dir, headParsed.Repository, fetch.ForkAddress(addresses, headParsed.Repository), headBranch)
			}
		}()
		wg.Wait()
		ctx := DiffContext{Workdir: dir}
		broken := storage.IsMissingObjectError(errors.Join(errs...))
		if baseLocal {
			ctx.Base = "refs/workspace/" + baseBranch
			if !headLocal {
				upstreamRef := "refs/fork/" + fetch.URLHash(wsURL) + "/" + baseBranch
				if _, err := git.ReadRef(dir, upstreamRef); err == nil {
					ctx.Base = upstreamRef
				}
			}
		} else {
			ctx.Base = "refs/fork/" + fetch.URLHash(baseParsed.Repository) + "/" + baseBranch
		}
		if headLocal {
			ctx.Head = "refs/workspace/" + headBranch
		} else {
			ctx.Head = "refs/fork/" + fetch.URLHash(headParsed.Repository) + "/" + headBranch
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
			ctx.Error = "cannot fetch " + strings.Join(missing, " and ")
		}
		return ctx, broken
	}
	ctx, broken := resolve(forkDir)
	if !broken {
		return ctx
	}
	// The borrower is a disposable cache, so a missing object rebuilds it and retries once.
	repaired, repairErr := storage.RepairForkRepository(cacheDir, forkKey)
	if repairErr != nil {
		log.Debug("fork repo repair failed", "dir", forkDir, "error", repairErr)
		return ctx
	}
	forgetFetchedRefs(forkDir)
	ctx, _ = resolve(repaired)
	return ctx
}

// refResolves reports whether a ref names an object the repo can read, and which half failed.
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

// forgetFetchedRefs drops a fork repo's memoized fetches, so a rebuilt repo is populated again.
func forgetFetchedRefs(forkDir string) {
	prefix := forkDir + "\x00"
	fetchedRefs.Range(func(key, _ any) bool {
		if name, ok := key.(string); ok && strings.HasPrefix(name, prefix) {
			fetchedRefs.Delete(key)
		}
		return true
	})
}

// resolveLocalRef resolves a branch name locally, falling back to its tracking ref.
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

// fetchFromUpstream fetches a branch into namespaced refs, from the address; the
// remote name hashes the identity, so it is stable across spellings.
func fetchFromUpstream(forkDir, repoURL, address, branch string) error {
	key := forkDir + "\x00" + repoURL + "\x00" + branch
	if _, ok := fetchedRefs.Load(key); ok {
		return nil
	}
	hash := fetch.URLHash(repoURL)
	remoteName := "remote-" + hash
	if err := git.EnsureRemote(forkDir, remoteName, address); err != nil {
		return fmt.Errorf("fork remote: %w", err)
	}
	refspec := fmt.Sprintf("+refs/heads/%s:refs/fork/%s/%s", branch, hash, branch)
	if _, err := git.ExecGit(forkDir, []string{"fetch", remoteName, refspec, "--no-tags"}); err != nil {
		return fmt.Errorf("fetch %s from %s: %w", branch, repoURL, err)
	}
	fetchedRefs.Store(key, true)
	return nil
}

// lendWorkspaceBranch makes a workspace branch resolvable in the fork repo.
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

// workspaceTip resolves a workspace branch to a full sha, or its tracking ref.
func workspaceTip(workdir, branch string) string {
	for _, ref := range []string{"refs/heads/" + branch, "refs/remotes/origin/" + branch} {
		result, err := git.ExecGit(workdir, []string{"rev-parse", "--verify", "--quiet", ref})
		if err == nil && strings.TrimSpace(result.Stdout) != "" {
			return strings.TrimSpace(result.Stdout)
		}
	}
	return ""
}

// fetchFromWorkspace fetches a workspace branch into refs/workspace/, or its tracking ref.
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
	refspec = fmt.Sprintf("+refs/remotes/origin/%s:refs/workspace/%s", branch, branch)
	if _, err := git.ExecGit(forkDir, []string{"fetch", workdir, refspec, "--no-tags"}); err != nil {
		return fmt.Errorf("fetch %s from workspace: %w", branch, err)
	}
	fetchedRefs.Store(key, true)
	return nil
}

// ResolvePRDiff resolves the diff range for a pull request, in the repository that holds it.
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

// applyPinPolicy pins the diff to the pull request's stored tips, so it reads the same everywhere.
func applyPinPolicy(ctx *DiffContext, workdir string, pr *PullRequest, isForkPR bool) {
	if pr.HeadTip == "" {
		return
	}
	// A fork pull request resolves only in the fork bare repo.
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

// qualifyPRRefs qualifies a pull request's relative refs with the repository that holds it.
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
