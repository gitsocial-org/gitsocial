// fetch.go - Generic repository fetch orchestration
package fetch

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/result"
	"github.com/gitsocial-org/gitsocial/core/storage"
)

// Stats contains fetch operation statistics.
type Stats struct {
	Repositories int
	Items        int
	Errors       []Error
}

// Error records a per-repository fetch failure.
type Error struct {
	Repository string
	Error      string
}

// Options controls fetch behavior.
type Options struct {
	WorkspaceBranch  string
	Since            string
	Before           string
	Parallel         int
	FetchAllBranches bool
	OnProgress       func(repoURL string, processed, total int)
}

// RepoInfo identifies a repository to fetch.
type RepoInfo struct {
	URL    string
	Branch string
	ListID string
}

// Result is the return type for fetch operations.
type Result = result.Result[Stats]

// PostFetchHook is called after a repository is fetched and processed.
// Extensions use this for post-fetch operations (e.g., follower detection, list caching).
type PostFetchHook func(storageDir, repoURL, branch, workspaceURL string)

// FetchAll fetches multiple repositories in parallel with commit processing and post-fetch hooks.
// The caller provides the repo list — this function handles workspace sync, parallelism, and version reconciliation.
func FetchAll(workdir, cacheDir string, opts *Options, repos []RepoInfo, processors []CommitProcessor, hooks []PostFetchHook) result.Result[Stats] {
	if opts == nil {
		opts = &Options{}
	}
	if opts.Parallel == 0 {
		opts.Parallel = 4
	}
	if opts.Since == "" {
		opts.Since = time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	}

	start := time.Now()
	log.Info("fetch started", "since", opts.Since)

	stats := Stats{}

	// Sync workspace origin — workspace always uses all-branch logic
	originURL := protocol.NormalizeURL(git.GetOriginURL(workdir))
	if originURL != "" {
		wsBranch := opts.WorkspaceBranch
		if wsBranch == "" {
			wsBranch = "main"
		}
		// Always fetch default refspec (configured tracking branches)
		fetchErr := git.FetchRemote(workdir, "origin", nil)
		// Always fetch gitmsg refs (config, lists, extension branches)
		if err := git.FetchRefspec(workdir, "origin", "+refs/gitmsg/*:refs/gitmsg/*"); err != nil {
			log.Debug("fetch gitmsg refs", "error", err)
		}
		// Optionally fetch all upstream branches
		if opts.FetchAllBranches {
			if err := git.FetchRefspec(workdir, "origin", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
				log.Debug("fetch all branches", "error", err)
			}
		}
		if fetchErr == nil {
			if err := cache.InsertRepository(cache.Repository{
				URL:         originURL,
				Branch:      "*",
				StoragePath: workdir,
			}); err != nil {
				log.Warn("insert workspace repository failed", "url", originURL, "error", err)
			}
			meta, metaErr := cache.GetRepositoryFetchMeta(originURL)
			if metaErr == nil {
				var wsCount int
				var wsErr error
				if !meta.HasCommits {
					wsCount, wsErr = fetchFullHistoryAllBranches(workdir, originURL, wsBranch, processors)
				} else {
					wsCount, wsErr = fetchIncrementalAllBranches(workdir, originURL, wsBranch, meta.NewestCommitTime, processors)
				}
				if wsErr != nil {
					log.Warn("workspace commit processing failed", "url", originURL, "error", wsErr)
				} else {
					stats.Items += wsCount
				}
			}
			if err := cache.UpdateRepositoryLastFetch(originURL); err != nil {
				log.Debug("update last fetch failed", "url", originURL, "error", err)
			}
			runHooks(hooks, workdir, originURL, "*", originURL)
			stats.Repositories++
		}
	}

	repos = DedupeRepos(repos)

	if len(repos) == 0 {
		log.Info("fetch complete", "repos", stats.Repositories, "items", stats.Items, "duration_ms", time.Since(start).Milliseconds())
		return result.Ok(stats)
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, opts.Parallel)

	workspaceURL := originURL

	for _, repo := range repos {
		if originURL != "" && repo.URL == originURL {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}

		go func(r RepoInfo) {
			defer wg.Done()
			defer func() { <-sem }()

			isFollowed := r.ListID != "" || cache.IsRepositoryInAnyList(r.URL, workdir)

			count, err := fetchRepository(cacheDir, r.URL, r.Branch, isFollowed, opts.Since, opts.Before, workspaceURL, processors, hooks)

			mu.Lock()
			if err != nil {
				log.Warn("fetch failed", "repo", r.URL, "error", err)
				stats.Errors = append(stats.Errors, Error{
					Repository: r.URL,
					Error:      err.Error(),
				})
			} else {
				log.Debug("fetched repo", "repo", r.URL, "items", count)
				stats.Repositories++
				stats.Items += count
			}
			processed := stats.Repositories + len(stats.Errors)
			mu.Unlock()

			if opts.OnProgress != nil {
				opts.OnProgress(r.URL, processed, len(repos))
			}
		}(repo)
	}

	wg.Wait()

	if reconciled, err := cache.ReconcileVersions(); err == nil && reconciled > 0 {
		log.Debug("reconciled version records", "count", reconciled)
	}

	log.Info("fetch complete", "repos", stats.Repositories, "items", stats.Items, "errors", len(stats.Errors), "duration_ms", time.Since(start).Milliseconds())
	return result.Ok(stats)
}

// FetchRepository fetches complete history for a single repository.
func FetchRepository(cacheDir, repoURL, branch, workspaceURL string, processors []CommitProcessor, hooks []PostFetchHook) result.Result[Stats] {
	if branch == "" {
		branch = "main"
	}

	storageDir, err := storage.EnsureRepository(cacheDir, repoURL, branch, &storage.EnsureOptions{
		IsPersistent: true,
	})
	if err != nil {
		return result.ErrWithDetails[Stats]("STORAGE_ERROR", "Failed to ensure repository", err)
	}

	if err := cache.InsertRepository(cache.Repository{
		URL:         repoURL,
		Branch:      branch,
		StoragePath: storageDir,
	}); err != nil {
		log.Debug("insert repository to cache failed", "url", repoURL, "error", err)
	}

	if err := storage.FetchRepository(storageDir, branch, nil); err != nil {
		return result.ErrWithDetails[Stats]("FETCH_ERROR", "Failed to fetch repository", err)
	}

	count, err := fetchFullHistory(storageDir, repoURL, branch, processors)
	if err != nil {
		return result.ErrWithDetails[Stats]("PROCESS_ERROR", "Failed to process commits", err)
	}

	if err := cache.UpdateRepositoryLastFetch(repoURL); err != nil {
		log.Debug("update last fetch failed", "url", repoURL, "error", err)
	}
	runHooks(hooks, storageDir, repoURL, branch, workspaceURL)

	return result.Ok(Stats{
		Repositories: 1,
		Items:        count,
	})
}

// FetchRepositoryRange fetches a repository within a date range (for pagination).
func FetchRepositoryRange(cacheDir, repoURL, branch, since, before, workspaceURL string, processors []CommitProcessor, hooks []PostFetchHook) result.Result[Stats] {
	if branch == "" {
		branch = "main"
	}

	count, err := fetchRepository(cacheDir, repoURL, branch, false, since, before, workspaceURL, processors, hooks)
	if err != nil {
		return result.ErrWithDetails[Stats]("FETCH_ERROR", "Failed to fetch repository", err)
	}

	return result.Ok(Stats{
		Repositories: 1,
		Items:        count,
	})
}

// fetchRepository clones or updates a repository and processes its commits.
func fetchRepository(cacheDir, repoURL, branch string, isFollowed bool, defaultSince, defaultBefore, workspaceURL string, processors []CommitProcessor, hooks []PostFetchHook) (int, error) {
	storageDir, err := storage.EnsureRepository(cacheDir, repoURL, branch, &storage.EnsureOptions{
		IsPersistent: isFollowed,
	})
	if err != nil {
		return 0, fmt.Errorf("ensure repository: %w", err)
	}

	if err := cache.InsertRepository(cache.Repository{
		URL:         repoURL,
		Branch:      branch,
		StoragePath: storageDir,
	}); err != nil {
		log.Debug("insert repository to cache failed", "url", repoURL, "error", err)
	}

	meta, err := cache.GetRepositoryFetchMeta(repoURL)
	if err != nil {
		return 0, fmt.Errorf("get fetch meta: %w", err)
	}

	var count int
	allBranches := branch == "*"
	if isFollowed {
		if !meta.HasCommits {
			if err := storage.FetchRepository(storageDir, branch, nil); err != nil {
				return 0, fmt.Errorf("fetch full: %w", err)
			}
			if allBranches {
				count, err = fetchFullHistoryAllBranches(storageDir, repoURL, "main", processors)
			} else {
				count, err = fetchFullHistory(storageDir, repoURL, branch, processors)
			}
		} else {
			fetchOpts := &storage.FetchOptions{Since: meta.NewestCommitTime.Format("2006-01-02")}
			if err := storage.FetchRepository(storageDir, branch, fetchOpts); err != nil {
				log.Debug("incremental fetch failed, continuing with cached data", "url", repoURL, "error", err)
			}
			if allBranches {
				count, err = fetchIncrementalAllBranches(storageDir, repoURL, "main", meta.NewestCommitTime, processors)
			} else {
				count, err = fetchIncremental(storageDir, repoURL, branch, meta.NewestCommitTime, processors)
			}
		}
	} else {
		fetchOpts := &storage.FetchOptions{Since: defaultSince}
		if err := storage.FetchRepository(storageDir, branch, fetchOpts); err != nil {
			return 0, fmt.Errorf("fetch window: %w", err)
		}
		count, err = fetch30DayWindow(storageDir, repoURL, branch, defaultSince, defaultBefore, processors)
	}
	if err != nil {
		return 0, err
	}

	if err := cache.UpdateRepositoryLastFetch(repoURL); err != nil {
		log.Debug("update last fetch failed", "url", repoURL, "error", err)
	}
	runHooks(hooks, storageDir, repoURL, branch, workspaceURL)

	return count, nil
}

// deleteHEADBranchCommits removes commits stored with branch="HEAD" (a symbolic ref, not a real branch).
func deleteHEADBranchCommits(repoURL string) {
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`DELETE FROM core_commits WHERE repo_url = ? AND branch = 'HEAD'`, repoURL)
		return err
	}); err != nil {
		log.Debug("delete HEAD branch commits", "error", err)
	}
}

// fetchFullHistoryAllBranches retrieves and processes all commits with per-commit branch tracking.
func fetchFullHistoryAllBranches(storageDir, repoURL, fallbackBranch string, processors []CommitProcessor) (int, error) {
	gitCommits, err := git.GetCommits(storageDir, &git.GetCommitsOptions{All: true})
	if err != nil {
		return 0, fmt.Errorf("get commits: %w", err)
	}
	deleteHEADBranchCommits(repoURL)
	count, err := processAllBranchCommits(gitCommits, repoURL, fallbackBranch, processors)
	if err != nil {
		return 0, err
	}
	liveHashes := make(map[string]bool, len(gitCommits))
	for _, c := range gitCommits {
		liveHashes[c.Hash] = true
	}
	if staled, err := cache.MarkCommitsStaleByRepo(repoURL, liveHashes); err != nil {
		log.Warn("mark stale commits", "error", err, "repo", repoURL)
	} else if staled > 0 {
		log.Debug("marked stale commits", "repo", repoURL, "count", staled)
	}
	startDate := time.Now().Format("2006-01-02")
	if len(gitCommits) > 0 {
		oldest := gitCommits[len(gitCommits)-1].Timestamp
		startDate = oldest.Format("2006-01-02")
	}
	endDate := time.Now().Format("2006-01-02")
	if rangeID, err := cache.InsertFetchRange(repoURL, startDate, endDate); err == nil {
		if err := cache.UpdateFetchRangeStatus(rangeID, "complete", count, ""); err != nil {
			log.Debug("update fetch range status", "error", err, "rangeID", rangeID)
		}
	}
	return count, nil
}

// fetchIncrementalAllBranches retrieves and processes commits since sinceTime with per-commit branches.
func fetchIncrementalAllBranches(storageDir, repoURL, fallbackBranch string, sinceTime time.Time, processors []CommitProcessor) (int, error) {
	gitCommits, err := git.GetCommits(storageDir, &git.GetCommitsOptions{
		All:   true,
		Since: &sinceTime,
	})
	if err != nil {
		return 0, fmt.Errorf("get commits: %w", err)
	}
	deleteHEADBranchCommits(repoURL)
	count, err := processAllBranchCommits(gitCommits, repoURL, fallbackBranch, processors)
	if err != nil {
		return 0, err
	}
	if liveHashes, err := git.GetAllCommitHashes(storageDir); err == nil {
		if staled, err := cache.MarkCommitsStaleByRepo(repoURL, liveHashes); err == nil && staled > 0 {
			log.Debug("marked stale commits", "repo", repoURL, "count", staled)
		}
	}
	startDate := sinceTime.Format("2006-01-02")
	endDate := time.Now().Format("2006-01-02")
	if rangeID, err := cache.InsertFetchRange(repoURL, startDate, endDate); err == nil {
		if err := cache.UpdateFetchRangeStatus(rangeID, "complete", count, ""); err != nil {
			log.Debug("update fetch range status", "error", err, "rangeID", rangeID)
		}
	}
	return count, nil
}

// fetchFullHistory retrieves and processes all commits from a repository.
func fetchFullHistory(storageDir, repoURL, branch string, processors []CommitProcessor) (int, error) {
	gitCommits, err := git.GetCommits(storageDir, &git.GetCommitsOptions{All: true})
	if err != nil {
		return 0, fmt.Errorf("get commits: %w", err)
	}
	count, err := ProcessCommits(gitCommits, repoURL, branch, processors)
	if err != nil {
		return 0, err
	}
	liveHashes := make(map[string]bool, len(gitCommits))
	for _, c := range gitCommits {
		liveHashes[c.Hash] = true
	}
	if staled, err := cache.MarkCommitsStale(repoURL, branch, liveHashes); err != nil {
		log.Warn("mark stale commits", "error", err, "repo", repoURL)
	} else if staled > 0 {
		log.Debug("marked stale commits", "repo", repoURL, "count", staled)
	}
	startDate := time.Now().Format("2006-01-02")
	if len(gitCommits) > 0 {
		oldest := gitCommits[len(gitCommits)-1].Timestamp
		startDate = oldest.Format("2006-01-02")
	}
	endDate := time.Now().Format("2006-01-02")
	if rangeID, err := cache.InsertFetchRange(repoURL, startDate, endDate); err == nil {
		if err := cache.UpdateFetchRangeStatus(rangeID, "complete", count, ""); err != nil {
			log.Debug("update fetch range status", "error", err, "rangeID", rangeID)
		}
	}
	return count, nil
}

// fetchIncremental retrieves and processes commits since the given time.
func fetchIncremental(storageDir, repoURL, branch string, sinceTime time.Time, processors []CommitProcessor) (int, error) {
	gitCommits, err := git.GetCommits(storageDir, &git.GetCommitsOptions{
		All:   true,
		Since: &sinceTime,
	})
	if err != nil {
		return 0, fmt.Errorf("get commits: %w", err)
	}
	count, err := ProcessCommits(gitCommits, repoURL, branch, processors)
	if err != nil {
		return 0, err
	}
	// Get all hashes from bare repo (not just since sinceTime) for stale detection
	if liveHashes, err := git.GetAllCommitHashes(storageDir); err == nil {
		if staled, err := cache.MarkCommitsStale(repoURL, branch, liveHashes); err == nil && staled > 0 {
			log.Debug("marked stale commits", "repo", repoURL, "count", staled)
		}
	}
	startDate := sinceTime.Format("2006-01-02")
	endDate := time.Now().Format("2006-01-02")
	if rangeID, err := cache.InsertFetchRange(repoURL, startDate, endDate); err == nil {
		if err := cache.UpdateFetchRangeStatus(rangeID, "complete", count, ""); err != nil {
			log.Debug("update fetch range status", "error", err, "rangeID", rangeID)
		}
	}
	return count, nil
}

// fetch30DayWindow retrieves commits within a specified date range.
func fetch30DayWindow(storageDir, repoURL, branch, since, before string, processors []CommitProcessor) (int, error) {
	sinceTime, _ := time.Parse("2006-01-02", since)
	opts := &git.GetCommitsOptions{
		All:   true,
		Since: &sinceTime,
	}
	if before != "" {
		beforeTime, _ := time.Parse("2006-01-02", before)
		opts.Until = &beforeTime
	}
	gitCommits, err := git.GetCommits(storageDir, opts)
	if err != nil {
		return 0, fmt.Errorf("get commits: %w", err)
	}
	count, err := ProcessCommits(gitCommits, repoURL, branch, processors)
	if err != nil {
		return 0, err
	}
	endDate := before
	if endDate == "" {
		endDate = time.Now().Format("2006-01-02")
	}
	if rangeID, err := cache.InsertFetchRange(repoURL, since, endDate); err == nil {
		if err := cache.UpdateFetchRangeStatus(rangeID, "complete", count, ""); err != nil {
			log.Debug("update fetch range status", "error", err, "rangeID", rangeID)
		}
	}
	return count, nil
}

// DedupeRepos removes duplicate repositories from the list by URL and branch.
func DedupeRepos(repos []RepoInfo) []RepoInfo {
	seen := make(map[string]bool)
	deduped := make([]RepoInfo, 0, len(repos))
	for _, r := range repos {
		key := r.URL + "#" + r.Branch
		if !seen[key] {
			seen[key] = true
			deduped = append(deduped, r)
		}
	}
	return deduped
}

// runHooks executes all post-fetch hooks for a repository.
func runHooks(hooks []PostFetchHook, storageDir, repoURL, branch, workspaceURL string) {
	for _, hook := range hooks {
		hook(storageDir, repoURL, branch, workspaceURL)
	}
}
