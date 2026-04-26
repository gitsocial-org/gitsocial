// forks.go - Fetch data from registered fork repositories
package fetch

import (
	"fmt"
	"sync"

	"github.com/gitsocial-org/gitsocial/core/cache"
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

// FetchForks fetches all gitmsg branches from registered forks concurrently,
// processing commits through all registered extension processors.
func FetchForks(workdir, cacheDir string, processors []CommitProcessor) FetchForkStats {
	forks := gitmsg.GetForks(workdir)
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

// fetchFork adds a remote for a fork URL in the shared bare repo and fetches all gitmsg data.
func fetchFork(forkDir, forkURL string, processors []CommitProcessor) (int, error) {
	hash := URLHash(forkURL)
	remoteName := "remote-" + hash
	if _, err := git.ExecGit(forkDir, []string{"remote", "add", remoteName, forkURL}); err != nil {
		log.Debug("add fork remote (may already exist)", "remote", remoteName, "error", err)
	}
	refspec := fmt.Sprintf("+refs/heads/gitmsg/*:refs/forks/%s/gitmsg/*", hash)
	if _, err := git.ExecGit(forkDir, []string{"fetch", remoteName, refspec, "--no-tags"}); err != nil {
		return 0, fmt.Errorf("fetch fork: %w", err)
	}
	// List all fetched gitmsg branches for this fork
	prefix := fmt.Sprintf("refs/forks/%s/gitmsg/", hash)
	refList, err := git.ExecGit(forkDir, []string{"for-each-ref", "--format=%(refname)", prefix})
	if err != nil {
		return 0, fmt.Errorf("list fork refs: %w", err)
	}
	totalCount := 0
	for _, ref := range splitNonEmpty(refList.Stdout) {
		branch := ref[len(fmt.Sprintf("refs/forks/%s/", hash)):]
		gitCommits, err := git.GetCommits(forkDir, &git.GetCommitsOptions{Branch: ref})
		if err != nil {
			log.Debug("get fork commits failed", "fork", forkURL, "ref", ref, "error", err)
			continue
		}
		if err := cache.InsertRepository(cache.Repository{
			URL:         forkURL,
			Branch:      branch,
			StoragePath: forkDir,
		}); err != nil {
			log.Debug("insert fork repository failed", "url", forkURL, "branch", branch, "error", err)
		}
		count, err := ProcessCommits(forkDir, gitCommits, forkURL, branch, processors)
		if err != nil {
			log.Debug("process fork commits failed", "fork", forkURL, "branch", branch, "error", err)
			continue
		}
		totalCount += count
		liveHashes := make(map[string]bool, len(gitCommits))
		for _, c := range gitCommits {
			liveHashes[c.Hash] = true
		}
		if staled, err := cache.MarkCommitsStale(forkURL, branch, liveHashes); err == nil && staled > 0 {
			log.Debug("marked stale fork commits", "fork", forkURL, "branch", branch, "count", staled)
		}
	}
	if err := cache.UpdateRepositoryLastFetch(forkURL); err != nil {
		log.Debug("update last fetch failed", "url", forkURL, "error", err)
	}
	return totalCount, nil
}

// URLHash returns a short hash for differentiating fork remote names.
func URLHash(url string) string {
	h := uint32(0)
	for _, c := range url {
		h = h*31 + uint32(c)
	}
	return fmt.Sprintf("%08x", h)
}

func splitNonEmpty(s string) []string {
	var result []string
	for _, line := range splitLines(s) {
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := make([]string, 0)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
