// workspace.go - Unified workspace sync: single git log, combined tip check, parallel extension processing
package fetch

import (
	"strings"
	"sync"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
)

// quickPassLimit is how many recent commits the quick pass loads before the background pass takes the rest.
const quickPassLimit = 10000

// WorkspaceSyncFunc processes pre-fetched workspace commits for one extension, which resolves its own branch.
type WorkspaceSyncFunc func(commits []git.Commit, workdir, repoURL, defaultBranch string)

// workspaceSyncContext bundles the resolved state shared between the quick
// pass and the background continuation.
type workspaceSyncContext struct {
	workdir       string
	repoURL       string
	defaultBranch string
	procs         []WorkspaceSyncFunc
	combinedTip   string
	tipKey        string
	// excludes are the previous walk's tip hashes; the incremental walk is
	// everything they do not reach. Empty means a full walk.
	excludes []string
}

// resolveWorkspaceSyncContext gathers the per-sync state once. Returns nil
// when no work is needed (tips unchanged since last full sync).
func resolveWorkspaceSyncContext(workdir string, procs []WorkspaceSyncFunc) *workspaceSyncContext {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	defaultBranch, _ := git.GetDefaultBranch(workdir)
	if defaultBranch == "" {
		// A label fallback only, so the checked-out branch is good enough here.
		if current, err := git.GetCurrentBranch(workdir); err == nil && current != "" && current != "HEAD" {
			defaultBranch = current
		} else {
			defaultBranch = "main"
		}
	}

	// Every tip the gate watches, local and remote tracking: the timeline shows
	// commits from all branches, so a push or a local commit on any of them has
	// to invalidate the sync cache. for-each-ref output is sorted.
	tipParts := make([]string, 2)
	if result, err := git.ExecGit(workdir, []string{
		"for-each-ref", "--format=%(objectname)", "refs/remotes/origin/",
	}); err == nil {
		tipParts[0] = strings.TrimSpace(result.Stdout)
	}
	if result, err := git.ExecGit(workdir, []string{
		"for-each-ref", "--format=%(objectname)", "refs/heads/",
	}); err == nil {
		tipParts[1] = strings.TrimSpace(result.Stdout)
	}
	combinedTip := strings.Join(tipParts, "\x00")
	tipKey := "workspace:" + repoURL

	persisted, err := cache.GetSyncTip(tipKey)
	if err == nil && persisted == ancestryMarker+combinedTip {
		return nil
	}

	_ = cache.InsertRepository(cache.Repository{URL: repoURL, Branch: "*", StoragePath: workdir})

	return &workspaceSyncContext{
		workdir:       workdir,
		repoURL:       repoURL,
		defaultBranch: defaultBranch,
		procs:         procs,
		combinedTip:   combinedTip,
		tipKey:        tipKey,
		excludes:      workspaceSyncExcludes(repoURL, persisted),
	}
}

// workspaceSyncExcludes returns the previous walk's tip hashes, or nil when the
// next walk must be full. Ancestry, never a time window: an item creation
// inserts its commit at wall-clock now, and a timestamp-derived window moves
// past commits the walk has not seen; a late-pushed commit carries a commit
// date older than any clock bound.
func workspaceSyncExcludes(repoURL, persistedTip string) []string {
	// Only a tip this ancestry walk wrote proves the cache reaches its hashes;
	// a row from the old time-window code may sit past starved commits, so it
	// earns one full walk, which backfills them.
	marked, ok := strings.CutPrefix(persistedTip, ancestryMarker)
	if !ok {
		return nil
	}
	// An emptied repository (ResetRepositoryData, DeleteRepository) can leave the
	// tip row behind; with nothing cached, the walk must rebuild in full.
	meta, err := cache.GetRepositoryFetchMeta(repoURL)
	if err != nil || !meta.HasCommits {
		return nil
	}
	return tipHashes(marked)
}

// ancestryMarker prefixes a sync tip written by the ancestry walk; ref hashes never contain it.
const ancestryMarker = "ancestry\x00"

// nonMergeTips returns the tips in the cache's abbreviated form, dropping the
// merges, which --no-merges keeps out of every walk. On a listing error it
// returns the full-length input, which the guard then refuses: the safe side.
func nonMergeTips(workdir string, tips []string) []string {
	if len(tips) == 0 {
		return nil
	}
	short, err := git.ExecGit(workdir, append([]string{"rev-list", "--no-walk", "--abbrev-commit", "--abbrev=12"}, tips...))
	if err != nil {
		return tips
	}
	mergeList, err := git.ExecGit(workdir, append([]string{"rev-list", "--no-walk", "--min-parents=2", "--abbrev-commit", "--abbrev=12"}, tips...))
	if err != nil {
		return tips
	}
	merges := make(map[string]bool)
	for _, h := range strings.Fields(mergeList.Stdout) {
		merges[h] = true
	}
	kept := make([]string, 0, len(tips))
	for _, h := range strings.Fields(short.Stdout) {
		if !merges[h] {
			kept = append(kept, h)
		}
	}
	return kept
}

// tipHashes extracts the ref hashes from a stored combined tip.
func tipHashes(combinedTip string) []string {
	var hashes []string
	for _, part := range strings.Split(combinedTip, "\x00") {
		for _, line := range strings.Split(part, "\n") {
			if h := strings.TrimSpace(line); len(h) == 40 {
				hashes = append(hashes, h)
			}
		}
	}
	return hashes
}

// processCommitBatch inserts a batch of git commits into the cache and runs
// each extension sync against them.
func processCommitBatch(ctx *workspaceSyncContext, commits []git.Commit) error {
	if len(commits) == 0 {
		return nil
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
	return nil
}

// SyncWorkspace runs the quick pass, the background continuation and the identity backfill, and returns when the cache is current.
func SyncWorkspace(workdir string, procs []WorkspaceSyncFunc) error {
	if err := SyncWorkspaceQuick(workdir, procs); err != nil {
		return err
	}
	if err := SyncWorkspaceContinue(workdir, procs, nil); err != nil {
		return err
	}
	BackfillWorkspaceIdentity(workdir)
	return nil
}

// SyncWorkspaceLocal runs the sync without the network identity backfill and reports whether it ingested anything.
func SyncWorkspaceLocal(workdir string, procs []WorkspaceSyncFunc) (bool, error) {
	if resolveWorkspaceSyncContext(workdir, procs) == nil {
		return false, nil
	}
	if err := SyncWorkspaceQuick(workdir, procs); err != nil {
		return true, err
	}
	return true, SyncWorkspaceContinue(workdir, procs, nil)
}

// BackfillWorkspaceIdentity extracts signer keys for the workspace and verifies their bindings.
func BackfillWorkspaceIdentity(workdir string) {
	backfillRepoSignerKeys(workdir, gitmsg.ResolveRepoURL(workdir))
}

// SyncWorkspaceQuick processes the most recent quickPassLimit commits and returns, for callers that need a view to render.
func SyncWorkspaceQuick(workdir string, procs []WorkspaceSyncFunc) error {
	ctx := resolveWorkspaceSyncContext(workdir, procs)
	if ctx == nil {
		return nil // tips unchanged
	}

	// Bounded in both modes: this is the render-path pass, and the continuation
	// picks up whatever a large delta leaves beyond the cap.
	commits, err := git.GetCommits(workdir, &git.GetCommitsOptions{All: true, Exclude: ctx.excludes, Limit: quickPassLimit})
	if err != nil {
		return err
	}
	return processCommitBatch(ctx, commits)
}

// SyncProgress is reported by SyncWorkspaceContinue after each chunk.
type SyncProgress struct {
	Processed int
	Total     int
}

// SyncWorkspaceContinue processes the commits older than the quick pass in chunks, calling onProgress after each one.
func SyncWorkspaceContinue(workdir string, procs []WorkspaceSyncFunc, onProgress func(SyncProgress)) error {
	ctx := resolveWorkspaceSyncContext(workdir, procs)
	if ctx == nil {
		return nil // tips unchanged, the quick pass covered everything
	}

	commits, err := git.GetCommits(workdir, &git.GetCommitsOptions{All: true, Exclude: ctx.excludes})
	if err != nil {
		return err
	}

	// An incremental continuation re-resolves its own delta, so a commit that
	// landed between the quick pass's walk and here is in it; only the commits
	// the cache lacks are processed, never a positional skip against a list the
	// quick pass may not have produced.
	rest := commits
	if len(ctx.excludes) > 0 {
		if unfetched, ferr := cache.FilterUnfetchedCommitsByRepo(ctx.repoURL, commitHashes(commits)); ferr == nil {
			rest = filterCommitsByHash(commits, unfetched)
		}
	} else {
		// A full walk: skip the head the quick pass processed and chunk the tail.
		if len(commits) <= quickPassLimit {
			return finalizeWorkspaceSync(ctx)
		}
		rest = commits[quickPassLimit:]
	}
	total := len(rest)
	for start := 0; start < len(rest); start += quickPassLimit {
		end := start + quickPassLimit
		if end > len(rest) {
			end = len(rest)
		}
		if err := processCommitBatch(ctx, rest[start:end]); err != nil {
			log.Debug("workspace background sync chunk failed", "error", err, "start", start)
		}
		if onProgress != nil {
			onProgress(SyncProgress{Processed: end, Total: total})
		}
	}

	return finalizeWorkspaceSync(ctx)
}

// commitHashes lists the hashes of a commit batch.
func commitHashes(commits []git.Commit) []string {
	hashes := make([]string, 0, len(commits))
	for _, c := range commits {
		hashes = append(hashes, c.Hash)
	}
	return hashes
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

// finalizeWorkspaceSync marks commits that left the repo stale and records the sync tip.
func finalizeWorkspaceSync(ctx *workspaceSyncContext) error {
	// The tip advances only when every current tip commit is cached: a walk
	// that failed or was cut short leaves the old tip, and the next sync
	// retries the same delta instead of sealing the hole behind a new tip.
	// Merge tips are exempt, since the walk itself runs --no-merges.
	missing, err := cache.FilterUnfetchedCommitsByRepo(ctx.repoURL, nonMergeTips(ctx.workdir, tipHashes(ctx.combinedTip)))
	if err != nil || len(missing) > 0 {
		log.Warn("sync finalize skipped, walk incomplete", "repo", ctx.repoURL, "missing", len(missing), "error", err)
		return nil
	}
	// The stale check reads every live commit, not the window the walk used.
	liveHashes, err := git.GetAllCommitHashes(ctx.workdir)
	if err != nil {
		log.Warn("list live commits", "error", err, "repo", ctx.repoURL)
	} else if _, err := cache.MarkCommitsStaleByRepo(ctx.repoURL, liveHashes); err != nil {
		log.Warn("mark stale commits", "error", err, "repo", ctx.repoURL)
	}
	_ = cache.SetSyncTip(ctx.tipKey, ancestryMarker+ctx.combinedTip)
	return nil
}
