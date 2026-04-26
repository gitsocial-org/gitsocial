// workspace.go - Unified workspace sync: single git log, combined tip check, parallel extension processing
package fetch

import (
	"sort"
	"strings"
	"sync"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
)

// quickPassLimit is how many of the most-recent commits the quick sync
// pass loads before returning. Sized to be enough for an immediately useful
// timeline / extension view (~screenful x N) without making the first launch
// slow on large repos like the Linux kernel (1.4M commits). The remaining
// history is processed in a background goroutine afterwards.
const quickPassLimit = 10000

// WorkspaceSyncFunc processes pre-fetched commits for an extension.
// workdir is the git working directory, extBranch is the extension-specific
// branch (e.g. "gitmsg/pm"), defaultBranch is the repo default (e.g. "main").
type WorkspaceSyncFunc func(commits []git.Commit, workdir, repoURL, extBranch, defaultBranch string)

var (
	processorMu sync.RWMutex
	processors  = map[string]WorkspaceSyncFunc{}
)

// RegisterProcessor registers an extension's workspace sync processor.
// Extensions call this in init() to participate in unified workspace sync.
func RegisterProcessor(ext string, fn WorkspaceSyncFunc) {
	processorMu.Lock()
	defer processorMu.Unlock()
	processors[ext] = fn
}

// workspaceSyncContext bundles the resolved state shared between the quick
// pass and the background continuation.
type workspaceSyncContext struct {
	workdir       string
	repoURL       string
	defaultBranch string
	branches      map[string]string
	procs         map[string]WorkspaceSyncFunc
	combinedTip   string
	tipKey        string
}

// resolveWorkspaceSyncContext gathers the per-sync state once. Returns nil
// when no work is needed (tips unchanged since last full sync).
func resolveWorkspaceSyncContext(workdir string) *workspaceSyncContext {
	processorMu.RLock()
	procs := make(map[string]WorkspaceSyncFunc, len(processors))
	for k, v := range processors {
		procs[k] = v
	}
	processorMu.RUnlock()

	repoURL := gitmsg.ResolveRepoURL(workdir)
	defaultBranch, _ := git.GetDefaultBranch(workdir)
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	branches := map[string]string{"default": defaultBranch}
	for ext := range procs {
		branches[ext] = gitmsg.GetExtBranch(workdir, ext)
	}
	names := make([]string, 0, len(branches))
	for name := range branches {
		names = append(names, name)
	}
	sort.Strings(names)
	tipParts := make([]string, 0, len(branches)*2)
	for _, name := range names {
		branch := branches[name]
		tip, _ := git.ReadRef(workdir, branch)
		tipParts = append(tipParts, tip)
		remoteTip, _ := git.ReadRef(workdir, "origin/"+branch)
		tipParts = append(tipParts, remoteTip)
	}
	combinedTip := strings.Join(tipParts, "\x00")
	tipKey := "workspace:" + repoURL

	if combinedTip != "" {
		if persisted, err := cache.GetSyncTip(tipKey); err == nil && persisted == combinedTip {
			return nil
		}
	}

	_ = cache.InsertRepository(cache.Repository{URL: repoURL, Branch: "*", StoragePath: workdir})

	return &workspaceSyncContext{
		workdir:       workdir,
		repoURL:       repoURL,
		defaultBranch: defaultBranch,
		branches:      branches,
		procs:         procs,
		combinedTip:   combinedTip,
		tipKey:        tipKey,
	}
}

// processCommitBatch inserts a batch of git commits into the cache and runs
// each registered extension processor against them.
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
	var wg sync.WaitGroup
	for ext, proc := range ctx.procs {
		ext := ext
		proc := proc
		wg.Add(1)
		go func() {
			defer wg.Done()
			proc(commits, ctx.workdir, ctx.repoURL, ctx.branches[ext], ctx.defaultBranch)
		}()
	}
	wg.Wait()
	return nil
}

// SyncWorkspace runs the full sync — quick pass plus background continuation
// inline. Use this from non-interactive callers (CLI, tests) that should
// only return when the cache is fully populated. On a repo where there's
// nothing new since the last full sync, returns immediately.
func SyncWorkspace(workdir string) error {
	if err := SyncWorkspaceQuick(workdir); err != nil {
		return err
	}
	return SyncWorkspaceContinue(workdir, nil)
}

// SyncWorkspaceQuick processes the most recent quickPassLimit commits and
// returns. Use from interactive callers (TUI startup) that need the cache
// populated enough to render an immediately useful view; pair with a
// background SyncWorkspaceContinue for the rest of history.
//
// Returns nil without touching the cache when nothing has changed since
// the last full sync.
func SyncWorkspaceQuick(workdir string) error {
	ctx := resolveWorkspaceSyncContext(workdir)
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

// SyncWorkspaceContinue processes commits older than what SyncWorkspace
// loaded. Runs in chunks so the cache write lock releases between batches and
// the UI stays responsive. Calls onProgress (non-blocking) after each chunk.
// Records the sync tip only after the full pass completes, so an interrupted
// background sync resumes correctly on next launch.
func SyncWorkspaceContinue(workdir string, onProgress func(SyncProgress)) error {
	ctx := resolveWorkspaceSyncContext(workdir)
	if ctx == nil {
		return nil // tips unchanged — quick pass already covered everything
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

// finalizeWorkspaceSync runs the per-sync wrap-up: stale-marking against the
// full live hash set, plus tip recording so subsequent syncs short-circuit.
func finalizeWorkspaceSync(ctx *workspaceSyncContext, allCommits []git.Commit) error {
	liveHashes := make(map[string]bool, len(allCommits))
	for _, c := range allCommits {
		liveHashes[c.Hash] = true
	}
	_, _ = cache.MarkCommitsStaleByRepo(ctx.repoURL, liveHashes)
	if ctx.combinedTip != "" {
		_ = cache.SetSyncTip(ctx.tipKey, ctx.combinedTip)
	}
	return nil
}
