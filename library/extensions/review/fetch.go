// fetch.go - Review extension fetch wrapper over core/fetch
package review

import (
	"fmt"
	"sync"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/fetch"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/storage"
)

// FetchForkError records a per-fork fetch failure.
type FetchForkError struct {
	ForkURL string
	Error   string
}

// FetchForkStats contains aggregate stats from fetching all registered forks.
type FetchForkStats struct {
	Forks  int
	Items  int
	Errors []FetchForkError
}

// FetchRepository fetches review data from a remote repository.
func FetchRepository(cacheDir, repoURL, branch string) fetch.Result {
	if branch == "" {
		branch = "gitmsg/review"
	}
	return fetch.FetchRepository(cacheDir, repoURL, branch, "", Processors(), nil)
}

// FetchForks fetches review data from all registered fork repositories concurrently.
// Uses a single shared bare repo in forks/ with one remote per fork URL.
func FetchForks(workdir, cacheDir string) FetchForkStats {
	forks := GetForks(workdir)
	if len(forks) == 0 {
		return FetchForkStats{}
	}
	wsURL := gitmsg.ResolveRepoURL(workdir)
	forkDir, err := storage.EnsureForkRepository(cacheDir, wsURL)
	if err != nil {
		return FetchForkStats{Forks: len(forks), Errors: []FetchForkError{{ForkURL: wsURL, Error: err.Error()}}}
	}
	stats := FetchForkStats{Forks: len(forks)}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	processors := Processors()
	for _, forkURL := range forks {
		wg.Add(1)
		sem <- struct{}{}
		go func(url string) {
			defer wg.Done()
			defer func() { <-sem }()
			count, fetchErr := fetchFork(forkDir, url, processors)
			mu.Lock()
			if fetchErr != nil {
				log.Debug("fork fetch failed", "fork", url, "error", fetchErr)
				stats.Errors = append(stats.Errors, FetchForkError{ForkURL: url, Error: fetchErr.Error()})
			} else {
				stats.Items += count
			}
			mu.Unlock()
		}(forkURL)
	}
	wg.Wait()
	return stats
}

// fetchFork adds a remote for a fork URL in the shared bare repo and fetches review data.
func fetchFork(forkDir, forkURL string, processors []fetch.CommitProcessor) (int, error) {
	hash := urlHash(forkURL)
	remoteName := "remote-" + hash
	// Add remote (may already exist)
	if _, err := git.ExecGit(forkDir, []string{"remote", "add", remoteName, forkURL}); err != nil {
		log.Debug("add fork remote (may already exist)", "remote", remoteName, "error", err)
	}
	// Fetch gitmsg branches into namespaced refs
	refspec := fmt.Sprintf("+refs/heads/gitmsg/*:refs/forks/%s/gitmsg/*", hash)
	if _, err := git.ExecGit(forkDir, []string{"fetch", remoteName, refspec, "--no-tags"}); err != nil {
		return 0, fmt.Errorf("fetch fork: %w", err)
	}
	// Get commits from the fork's review branch
	branch := "gitmsg/review"
	forkRef := fmt.Sprintf("refs/forks/%s/%s", hash, branch)
	gitCommits, err := git.GetCommits(forkDir, &git.GetCommitsOptions{Branch: forkRef})
	if err != nil {
		return 0, fmt.Errorf("get commits: %w", err)
	}
	// Register in cache DB (storage path points to shared fork dir)
	if err := cache.InsertRepository(cache.Repository{
		URL:         forkURL,
		Branch:      branch,
		StoragePath: forkDir,
	}); err != nil {
		log.Debug("insert fork repository failed", "url", forkURL, "error", err)
	}
	// Process commits
	count, err := fetch.ProcessCommits(gitCommits, forkURL, branch, processors)
	if err != nil {
		return 0, fmt.Errorf("process commits: %w", err)
	}
	// Mark stale commits
	liveHashes := make(map[string]bool, len(gitCommits))
	for _, c := range gitCommits {
		liveHashes[c.Hash] = true
	}
	if staled, err := cache.MarkCommitsStale(forkURL, branch, liveHashes); err == nil && staled > 0 {
		log.Debug("marked stale fork commits", "fork", forkURL, "count", staled)
	}
	if err := cache.UpdateRepositoryLastFetch(forkURL); err != nil {
		log.Debug("update last fetch failed", "url", forkURL, "error", err)
	}
	return count, nil
}

// Processors returns the commit processors for the review extension.
func Processors() []fetch.CommitProcessor {
	return []fetch.CommitProcessor{processReviewCommit}
}
