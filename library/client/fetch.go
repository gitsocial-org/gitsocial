// fetch.go - The fetch sequence every thin client runs: processor lists, forks, the workspace sync and the memo tiers.
package client

import (
	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/extensions/memo"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/extensions/release"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

// FetchOptions controls one run of Fetch.
type FetchOptions struct {
	ListID           string
	Parallel         int
	FetchAllBranches bool
	OnProgress       func(repoURL string, processed, total int)
}

// Fetch runs the one sequence: subscribed repositories, registered forks, the workspace, then the memo tiers.
func Fetch(workdir, cacheDir string, opts FetchOptions) (fetch.Result, fetch.Stats) {
	result := social.Fetch(workdir, cacheDir, &social.FetchOptions{
		ListID:           opts.ListID,
		Parallel:         opts.Parallel,
		FetchAllBranches: opts.FetchAllBranches,
		ExtraProcessors:  extraProcessors(),
		ExtraHooks:       review.PostFetchHooks(),
		OnProgress:       opts.OnProgress,
	})
	forkStats := FetchForks(workdir, cacheDir)
	if err := SyncWorkspace(workdir); err != nil {
		log.Debug("workspace sync failed", "error", err)
	}
	if err := memo.SyncAllTierReposToCache(workdir); err != nil {
		log.Debug("memo tier sync failed", "error", err)
	}
	return result, forkStats
}

// FetchRepository fetches one repository's history with every processor and hook.
func FetchRepository(cacheDir, repoURL, branch, workspaceURL string) fetch.Result {
	return fetch.FetchRepository(cacheDir, repoURL, branch, workspaceURL, processors(), hooks())
}

// FetchRepositoryRange fetches one repository inside a date window, for pagination.
func FetchRepositoryRange(cacheDir, repoURL, branch, since, before, workspaceURL string) fetch.Result {
	return fetch.FetchRepositoryRange(cacheDir, repoURL, branch, since, before, workspaceURL, processors(), hooks())
}

// FetchForks fetches every registered fork with the full processor set, then
// backfills any extension items row missed by dedup. Fetch and backfill are
// paired in one call so a fork commit can't linger in core_commits without its
// extension row, which the dedup would then skip forever.
func FetchForks(workdir, cacheDir string) fetch.Stats {
	procs := processors()
	stats := fetch.FetchForks(workdir, cacheDir, procs)
	fetch.BackfillExtensionItems(backfillRepos(workdir), backfillSpecs(), procs)
	// Link any just-fetched fork edits to their canonicals so proposals attach.
	_, _ = cache.ReconcileVersions()
	return stats
}

// SyncWorkspace ingests the workspace into the cache and returns when it is current.
func SyncWorkspace(workdir string) error {
	return fetch.SyncWorkspace(workdir, workspaceSyncs())
}

// SyncWorkspaceLocal ingests the workspace without the network identity backfill.
func SyncWorkspaceLocal(workdir string) (bool, error) {
	return fetch.SyncWorkspaceLocal(workdir, workspaceSyncs())
}

// SyncWorkspaceQuick ingests the most recent workspace commits and returns.
func SyncWorkspaceQuick(workdir string) error {
	return fetch.SyncWorkspaceQuick(workdir, workspaceSyncs())
}

// SyncWorkspaceContinue ingests the commits older than the quick pass, reporting progress per chunk.
func SyncWorkspaceContinue(workdir string, onProgress func(fetch.SyncProgress)) error {
	return fetch.SyncWorkspaceContinue(workdir, workspaceSyncs(), onProgress)
}

// SyncWorkspaceOrigin refreshes the workspace from its own origin and ingests its commits.
func SyncWorkspaceOrigin(workdir string, opts *fetch.Options) (string, fetch.Stats) {
	return fetch.SyncWorkspaceOrigin(workdir, opts, processors(), hooks())
}

// BackfillWorkspaceIdentity extracts signer keys for the workspace and verifies their bindings.
func BackfillWorkspaceIdentity(workdir string) {
	fetch.BackfillWorkspaceIdentity(workdir)
}

// processors returns every extension's commit processors plus the notification ones.
func processors() []fetch.CommitProcessor {
	return append(extraProcessors(), social.Processors()...)
}

// extraProcessors returns the processor set without social, which social.Fetch adds itself.
func extraProcessors() []fetch.CommitProcessor {
	procs := append(pm.Processors(), review.Processors()...)
	procs = append(procs, memo.Processors()...)
	procs = append(procs, release.Processors()...)
	return append(procs, notifications.MentionProcessor(), notifications.TrailerProcessor())
}

// hooks returns the post-fetch hooks every fetch runs.
func hooks() []fetch.PostFetchHook {
	return append(social.Hooks(), review.PostFetchHooks()...)
}

// workspaceSyncs returns the workspace sync of every extension, social first.
func workspaceSyncs() []fetch.WorkspaceSyncFunc {
	return []fetch.WorkspaceSyncFunc{
		social.SyncWorkspaceBatch,
		pm.SyncWorkspaceBatch,
		review.SyncWorkspaceBatch,
		release.SyncWorkspaceBatch,
		memo.SyncWorkspaceBatch,
	}
}

// backfillSpecs enumerates the extension items tables the post-fetch backfill scans.
func backfillSpecs() []fetch.ExtBackfillSpec {
	return []fetch.ExtBackfillSpec{
		social.BackfillSpec(),
		pm.BackfillSpec(),
		release.BackfillSpec(),
		review.BackfillSpec(),
		memo.BackfillSpec(),
	}
}

// backfillRepos returns the workspace URL plus every registered fork URL, the
// set whose cached commits may carry an orphaned extension row.
func backfillRepos(workdir string) []string {
	repos := make([]string, 0, 8)
	if ws := gitmsg.ResolveRepoURL(workdir); ws != "" {
		repos = append(repos, ws)
	}
	return append(repos, gitmsg.GetForks(workdir)...)
}
