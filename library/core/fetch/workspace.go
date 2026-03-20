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

// SyncWorkspace performs a unified workspace sync with combined tip check,
// single git log --all, and parallel extension processing.
func SyncWorkspace(workdir string) error {
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

	// Resolve branches and read tips
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

	// Skip if nothing changed since last sync
	if combinedTip != "" {
		if persisted, err := cache.GetSyncTip(tipKey); err == nil && persisted == combinedTip {
			return nil
		}
	}

	_ = cache.InsertRepository(cache.Repository{URL: repoURL, Branch: "*", StoragePath: workdir})

	// Single git log --all instead of separate subprocess per extension
	commits, err := git.GetCommits(workdir, &git.GetCommitsOptions{All: true})
	if err != nil {
		return err
	}

	cacheCommits := make([]cache.Commit, 0, len(commits))
	for _, gc := range commits {
		branch := CleanRefname(gc.Refname)
		if branch == "" {
			branch = defaultBranch
		}
		cacheCommits = append(cacheCommits, cache.Commit{
			Hash:        gc.Hash,
			RepoURL:     repoURL,
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

	// Dispatch to extension processors in parallel
	var wg sync.WaitGroup
	for ext, proc := range procs {
		ext := ext
		proc := proc
		wg.Add(1)
		go func() {
			defer wg.Done()
			proc(commits, workdir, repoURL, branches[ext], defaultBranch)
		}()
	}
	wg.Wait()

	// Mark stale across all branches
	liveHashes := make(map[string]bool, len(commits))
	for _, c := range commits {
		liveHashes[c.Hash] = true
	}
	_, _ = cache.MarkCommitsStaleByRepo(repoURL, liveHashes)

	if combinedTip != "" {
		_ = cache.SetSyncTip(tipKey, combinedTip)
	}
	return nil
}
