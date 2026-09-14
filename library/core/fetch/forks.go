// forks.go - Fetch data from registered fork repositories
package fetch

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/storage"
)

// FetchForks fetches all gitmsg branches from registered forks concurrently,
// processing commits through all registered extension processors.
func FetchForks(workdir, cacheDir string, processors []CommitProcessor) Stats {
	addresses := gitmsg.ForkAddresses(workdir)
	if len(addresses) == 0 {
		return Stats{}
	}
	forks := slices.Sorted(maps.Keys(addresses))
	wsURL := gitmsg.ResolveRepoURL(workdir)
	forkDir, err := storage.EnsureForkRepository(cacheDir, wsURL)
	if err != nil {
		return Stats{Repositories: len(forks), Errors: []Error{{Repository: wsURL, Error: err.Error()}}}
	}
	stats, missingObject := fetchForksInto(forkDir, forks, addresses, processors)
	if !missingObject {
		return stats
	}
	// The fork repo borrows the workspace's object database, so a donor that moved
	// or gc'd leaves it naming objects nobody has. It is a disposable cache:
	// rebuild it and fetch everything once more.
	repaired, repairErr := storage.RepairForkRepository(cacheDir, wsURL)
	if repairErr != nil {
		log.Debug("fork repo repair failed", "dir", forkDir, "error", repairErr)
		return stats
	}
	stats, _ = fetchForksInto(repaired, forks, addresses, processors)
	return stats
}

// fetchForksInto fetches every fork into one bare repo, reporting whether any
// failure named a missing or bad object.
func fetchForksInto(forkDir string, forks []string, addresses map[string]string, processors []CommitProcessor) (Stats, bool) {
	stats := Stats{Repositories: len(forks)}
	missingObject := false
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, forkURL := range forks {
		wg.Add(1)
		sem <- struct{}{}
		go func(url string) {
			defer wg.Done()
			defer func() { <-sem }()
			count, fetchErr := fetchFork(forkDir, url, ForkAddress(addresses, url), processors)
			mu.Lock()
			if fetchErr != nil {
				log.Debug("fork fetch failed", "fork", url, "error", fetchErr)
				stats.Errors = append(stats.Errors, Error{Repository: url, Error: fetchErr.Error()})
				missingObject = missingObject || storage.IsMissingObjectError(fetchErr)
			} else {
				stats.Items += count
			}
			mu.Unlock()
		}(forkURL)
	}
	wg.Wait()
	return stats, missingObject
}

// ForkAddress returns the address registered for a fork identity, or the identity itself.
func ForkAddress(addresses map[string]string, identity string) string {
	if address := addresses[identity]; address != "" {
		return address
	}
	return identity
}

// fetchFork adds a remote for a fork in the shared bare repo and fetches all
// gitmsg data. The remote name hashes the identity, so it is stable across
// spellings; the remote URL is the address.
func fetchFork(forkDir, forkURL, address string, processors []CommitProcessor) (int, error) {
	hash := URLHash(forkURL)
	remoteName := "remote-" + hash
	if err := git.EnsureRemote(forkDir, remoteName, address); err != nil {
		return 0, fmt.Errorf("fork remote: %w", err)
	}
	refspec := fmt.Sprintf("+refs/heads/gitmsg/*:refs/forks/%s/gitmsg/*", hash)
	// Also mirror the fork's published decline markers so we can learn which of our
	// cross-repo proposals the fork (as owner) has declined. Acceptance needs no
	// marker: it rides the fork's mirror edit on the gitmsg/* branches above.
	declineRefspec := fmt.Sprintf("+%s*:refs/forks/%s/declines/*", gitmsg.DeclinesRefPrefix, hash)
	if _, err := git.ExecGit(forkDir, []string{"fetch", remoteName, refspec, declineRefspec, "--no-tags"}); err != nil {
		return 0, fmt.Errorf("fetch fork: %w", err)
	}
	syncForkDeclines(forkDir, hash)
	// List all fetched gitmsg branches for this fork
	prefix := fmt.Sprintf("refs/forks/%s/gitmsg/", hash)
	refList, err := git.ExecGit(forkDir, []string{"for-each-ref", "--format=%(refname)", prefix})
	if err != nil {
		return 0, fmt.Errorf("list fork refs: %w", err)
	}
	totalCount := 0
	for _, ref := range strings.Fields(refList.Stdout) {
		branch := ref[len(fmt.Sprintf("refs/forks/%s/", hash)):]
		gitCommits, err := git.GetCommits(forkDir, &git.GetCommitsOptions{Branch: ref})
		if err != nil {
			// A ref whose objects are gone means the shared repo needs rebuilding,
			// which only the caller can do — every other read failure is per-ref.
			if storage.IsMissingObjectError(err) {
				return 0, fmt.Errorf("read fork commits: %w", err)
			}
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

// syncForkDeclines records the fork's published decline markers (fetched into
// refs/forks/<hash>/declines/*) into the local decline table, so a proposal this
// workspace made that the fork declined stops showing as pending.
func syncForkDeclines(forkDir, hash string) {
	out, err := git.ExecGit(forkDir, []string{
		"for-each-ref", "--format=%(contents:subject)",
		fmt.Sprintf("refs/forks/%s/declines/", hash),
	})
	if err != nil || out.Stdout == "" {
		return
	}
	for _, line := range strings.Fields(out.Stdout) {
		p := protocol.ParseRef(line)
		if p.Value == "" {
			continue
		}
		_ = cache.RecordDecline(protocol.NormalizeURL(p.Repository), p.Value, p.Branch)
	}
}

// URLHash returns a short hash for differentiating fork remote names.
func URLHash(url string) string {
	h := uint32(0)
	for _, c := range url {
		h = h*31 + uint32(c)
	}
	return fmt.Sprintf("%08x", h)
}
