// workspace.go - Workspace sync: the cache follows the home branch of each commit, with a ref gate and parallel extension processing
package fetch

import (
	"strings"
	"sync"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
)

// quickPassLimit is how many recent commits per branch the quick pass loads before the background pass takes the rest.
const quickPassLimit = 10000

// noWalkLimit is how many missing commits of a branch are read by hash, and how many hashes one read takes; above it the branch is walked as a range.
const noWalkLimit = 500

// homeMarker prefixes a sync tip written by the home sync, so a tip from older code earns one sync that repairs its rows.
const homeMarker = "home\x00"

// WorkspaceSyncFunc processes pre-fetched workspace commits for one extension, which resolves its own branch.
type WorkspaceSyncFunc func(commits []git.Commit, workdir, repoURL, defaultBranch string)

// workspaceSyncContext bundles the resolved state of one sync: the gate string, the branches, and the last tips of the stable branches on the fast path.
type workspaceSyncContext struct {
	workdir       string
	repoURL       string
	defaultBranch string
	procs         []WorkspaceSyncFunc
	combinedTip   string
	tipKey        string
	stable        []homeBranch
	code          []homeBranch
	since         map[string][]string
}

// resolveWorkspaceSyncContext gathers the per-sync state, or returns nil when the walked refs match the last full sync.
func resolveWorkspaceSyncContext(workdir string, procs []WorkspaceSyncFunc) (*workspaceSyncContext, error) {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	// A failed listing stops the sync: with no branches, the finalize would mark each row stale.
	combinedTip, err := listHomeRefs(workdir)
	if err != nil {
		return nil, err
	}
	tipKey := "workspace:" + repoURL
	persisted, err := cache.GetSyncTip(tipKey)
	if err == nil && persisted == homeMarker+combinedTip {
		return nil, nil
	}

	_ = cache.InsertRepository(cache.Repository{URL: repoURL, Branch: "*", StoragePath: workdir})

	stable, code := parseHomeRefs(combinedTip)
	ctx := &workspaceSyncContext{
		workdir:       workdir,
		repoURL:       repoURL,
		defaultBranch: "main",
		procs:         procs,
		combinedTip:   combinedTip,
		tipKey:        tipKey,
		stable:        stable,
		code:          code,
	}
	if len(stable) > 0 && !strings.HasPrefix(stable[0].name, contentPrefix) {
		ctx.defaultBranch = stable[0].name
	}
	// The fast path needs a tip that this sync wrote and a cache that still has its rows.
	if lastGate, ok := strings.CutPrefix(persisted, homeMarker); ok {
		if meta, merr := cache.GetRepositoryFetchMeta(repoURL); merr == nil && meta.HasCommits {
			ctx.since = stableSince(workdir, lastGate, stable)
		}
	}
	return ctx, nil
}

// processCommitBatch inserts a batch of git commits into the cache and runs each extension sync against them.
func processCommitBatch(ctx *workspaceSyncContext, commits []git.Commit) {
	if len(commits) == 0 {
		return
	}
	cacheCommits := make([]cache.Commit, 0, len(commits))
	for _, gc := range commits {
		branch := CleanRefname(gc.Refname)
		if branch == "" {
			branch = ctx.defaultBranch
		}
		cacheCommits = append(cacheCommits, cache.Commit{
			Hash:        gc.Hash,
			RepoURL:     ctx.repoURL,
			Branch:      branch,
			AuthorName:  gc.Author,
			AuthorEmail: gc.Email,
			Message:     gc.Message,
			Timestamp:   gc.Timestamp,
		})
	}
	if err := cache.InsertCommits(cacheCommits); err != nil {
		log.Debug("workspace sync insert failed", "error", err)
	}
	if _, err := cache.ReconcileVersions(); err != nil {
		log.Debug("workspace sync reconcile failed", "error", err)
	}
	// Identity backfill runs in BackfillWorkspaceIdentity, off this path: it waits on forge round-trips.
	var wg sync.WaitGroup
	for _, proc := range ctx.procs {
		proc := proc
		wg.Add(1)
		go func() {
			defer wg.Done()
			proc(commits, ctx.workdir, ctx.repoURL, ctx.defaultBranch)
		}()
	}
	wg.Wait()
}

// SyncWorkspace brings the cache to the state of the workspace refs, then runs the identity backfill.
func SyncWorkspace(workdir string, procs []WorkspaceSyncFunc) error {
	if err := SyncWorkspaceContinue(workdir, procs, nil); err != nil {
		return err
	}
	BackfillWorkspaceIdentity(workdir)
	return nil
}

// SyncWorkspaceLocal runs the sync without the network identity backfill and reports whether the refs had changed.
func SyncWorkspaceLocal(workdir string, procs []WorkspaceSyncFunc) (bool, error) {
	ran, _, err := syncWorkspaceHomes(workdir, procs, 0, nil)
	return ran, err
}

// BackfillWorkspaceIdentity extracts signer keys for the workspace and verifies their bindings.
func BackfillWorkspaceIdentity(workdir string) {
	backfillRepoSignerKeys(workdir, gitmsg.ResolveRepoURL(workdir))
}

// SyncWorkspaceQuick processes the most recent quickPassLimit missing commits of each branch and returns, for callers that need a view to render.
func SyncWorkspaceQuick(workdir string, procs []WorkspaceSyncFunc) error {
	_, _, err := syncWorkspaceHomes(workdir, procs, quickPassLimit, nil)
	return err
}

// SyncProgress is reported by SyncWorkspaceContinue after each chunk.
type SyncProgress struct {
	Processed int
	Total     int
}

// SyncWorkspaceContinue processes every missing commit in chunks, calling onProgress after each one, and finalizes the sync.
func SyncWorkspaceContinue(workdir string, procs []WorkspaceSyncFunc, onProgress func(SyncProgress)) error {
	_, _, err := syncWorkspaceHomes(workdir, procs, 0, onProgress)
	return err
}

// syncWorkspaceHomes inserts each commit that has no row under its home branch and reports whether it ran and how many it inserted; a limit makes it the quick pass, with no finalize.
func syncWorkspaceHomes(workdir string, procs []WorkspaceSyncFunc, limit int, onProgress func(SyncProgress)) (bool, int, error) {
	ctx, err := resolveWorkspaceSyncContext(workdir, procs)
	if err != nil || ctx == nil {
		return false, 0, err
	}
	lists, err := stableHashes(ctx.workdir, ctx.stable, ctx.since, limit)
	if err != nil {
		return true, 0, err
	}
	ordered, codeLists, err := codeHomes(ctx.workdir, ctx.stable, ctx.code)
	if err != nil {
		return true, 0, err
	}
	branches := append(append([]homeBranch(nil), ctx.stable...), ordered...)
	lists = append(lists, codeLists...)
	missing := make([][]string, len(lists))
	var commits []git.Commit
	// A long list is read as a range walk of its branch; the short lists of all branches share the reads by hash.
	var byHash []string
	homeOf := make(map[string]string)
	for i, hashes := range lists {
		if missing[i], err = cache.FilterUnfetchedCommits(ctx.repoURL, branches[i].name, hashes); err != nil {
			return true, 0, err
		}
		if len(missing[i]) > noWalkLimit {
			commits = append(commits, walkHomeCommits(ctx, branches, i, missing[i], limit)...)
			continue
		}
		for _, hash := range missing[i] {
			byHash = append(byHash, hash)
			homeOf[hash] = branches[i].name
		}
	}
	for start := 0; start < len(byHash); start += noWalkLimit {
		read, rerr := git.GetCommits(ctx.workdir, &git.GetCommitsOptions{Hashes: byHash[start:min(start+noWalkLimit, len(byHash))]})
		if rerr != nil {
			log.Debug("workspace sync read failed", "error", rerr)
		}
		for k := range read {
			read[k].Refname = localRefPrefix + homeOf[read[k].Hash]
		}
		commits = append(commits, read...)
	}
	for start := 0; start < len(commits); start += quickPassLimit {
		end := min(start+quickPassLimit, len(commits))
		processCommitBatch(ctx, commits[start:end])
		if onProgress != nil {
			onProgress(SyncProgress{Processed: end, Total: len(commits)})
		}
	}
	if limit == 0 {
		finalizeWorkspaceSync(ctx, branches, lists, missing)
	}
	return true, len(commits), nil
}

// walkHomeCommits reads the missing commits of one branch as a range walk and gives each the branch as its refname.
func walkHomeCommits(ctx *workspaceSyncContext, branches []homeBranch, i int, missing []string, limit int) []git.Commit {
	branch := branches[i]
	// A stable branch excludes the stable branches before it; a code branch excludes each stable branch.
	exclude := branchRefs(ctx.stable)
	if i < len(ctx.stable) {
		exclude = append(branchRefs(ctx.stable[:i]), ctx.since[branch.name]...)
	} else {
		limit = 0
	}
	walked, err := git.GetCommits(ctx.workdir, &git.GetCommitsOptions{
		Branch: branch.refs[0], IncludeRefs: branch.refs[1:], Exclude: exclude, Limit: limit,
	})
	if err != nil {
		log.Debug("workspace sync walk failed", "error", err, "branch", branch.name)
	}
	commits := filterCommitsByHash(walked, missing)
	for k := range commits {
		commits[k].Refname = localRefPrefix + branch.name
	}
	return commits
}

// filterCommitsByHash keeps the commits whose hash is in the given set.
func filterCommitsByHash(commits []git.Commit, keep []string) []git.Commit {
	set := make(map[string]bool, len(keep))
	for _, h := range keep {
		set[h] = true
	}
	kept := make([]git.Commit, 0, len(keep))
	for _, c := range commits {
		if set[c.Hash] {
			kept = append(kept, c)
		}
	}
	return kept
}

// finalizeWorkspaceSync marks each row stale whose branch is not the home of its commit, and records the sync tip.
func finalizeWorkspaceSync(ctx *workspaceSyncContext, branches []homeBranch, lists, missing [][]string) {
	// The tip advances only when every commit has its home row, so a failed read retries on the next sync.
	homes := make(map[string]string)
	for i, hashes := range lists {
		name := branches[i].name
		if len(missing[i]) > 0 {
			left, err := cache.FilterUnfetchedCommits(ctx.repoURL, name, missing[i])
			if err != nil || len(left) > 0 {
				log.Warn("sync finalize skipped, rows missing", "repo", ctx.repoURL, "branch", name, "missing", len(left), "error", err)
				return
			}
		}
		for _, hash := range hashes {
			homes[hash] = name
		}
	}
	// A ref that moved during the sync has rows this home map does not know; the next sync finalizes.
	if now, err := listHomeRefs(ctx.workdir); err != nil || now != ctx.combinedTip {
		return
	}
	// On the fast path a stable branch only grew, so its rows stay live and are not read.
	var settled []string
	if ctx.since != nil {
		for _, branch := range ctx.stable {
			settled = append(settled, branch.name)
		}
	}
	if _, err := cache.MarkCommitsStaleByHome(ctx.repoURL, homes, settled); err != nil {
		log.Warn("mark stale commits", "error", err, "repo", ctx.repoURL)
	}
	_ = cache.SetSyncTip(ctx.tipKey, homeMarker+ctx.combinedTip)
}
