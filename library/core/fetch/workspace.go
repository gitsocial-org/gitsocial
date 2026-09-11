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
}

// resolveWorkspaceSyncContext gathers the per-sync state once. Returns nil
// when no work is needed (tips unchanged since last full sync).
func resolveWorkspaceSyncContext(workdir string, procs []WorkspaceSyncFunc) *workspaceSyncContext {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	defaultBranch, _ := git.GetDefaultBranch(workdir)
	if defaultBranch == "" {
		defaultBranch = "main"
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

	if persisted, err := cache.GetSyncTip(tipKey); err == nil && persisted == combinedTip {
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
	}
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

	commits, err := git.GetCommits(workdir, &git.GetCommitsOptions{All: true, Limit: quickPassLimit})
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

	commits, err := git.GetCommits(workdir, &git.GetCommitsOptions{All: true})
	if err != nil {
		return err
	}

	// Skip the head of the list (already processed by SyncWorkspace) and
	// process the tail in chunks.
	if len(commits) <= quickPassLimit {
		return finalizeWorkspaceSync(ctx, commits)
	}
	rest := commits[quickPassLimit:]
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

	return finalizeWorkspaceSync(ctx, commits)
}

// finalizeWorkspaceSync marks commits that left the repo stale and records the sync tip.
func finalizeWorkspaceSync(ctx *workspaceSyncContext, allCommits []git.Commit) error {
	liveHashes := make(map[string]bool, len(allCommits))
	for _, c := range allCommits {
		liveHashes[c.Hash] = true
	}
	_, _ = cache.MarkCommitsStaleByRepo(ctx.repoURL, liveHashes)
	_ = cache.SetSyncTip(ctx.tipKey, ctx.combinedTip)
	return nil
}
