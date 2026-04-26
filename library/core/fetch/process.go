// process.go - Generic commit processing with extension dispatch
package fetch

import (
	"strings"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/identity"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// CommitProcessor is called for each newly fetched commit after it's inserted into core_commits.
// Extensions register processors to handle their own commit types (e.g., social posts, PM issues).
// The msg parameter may be nil if the commit has no GitMsg header.
type CommitProcessor func(commit git.Commit, msg *protocol.Message, repoURL, branch string)

// ProcessCommits filters unfetched commits, inserts them into core_commits, and dispatches to processors.
func ProcessCommits(storageDir string, gitCommits []git.Commit, repoURL, branch string, processors []CommitProcessor) (int, error) {
	hashes := make([]string, len(gitCommits))
	for i, c := range gitCommits {
		hashes[i] = c.Hash
	}
	unfetchedHashes, err := cache.FilterUnfetchedCommits(repoURL, branch, hashes)
	if err != nil {
		return 0, err
	}
	unfetchedSet := make(map[string]bool, len(unfetchedHashes))
	for _, h := range unfetchedHashes {
		unfetchedSet[h] = true
	}

	newGitCommits := make([]git.Commit, 0, len(gitCommits))
	newHashes := make([]string, 0, len(unfetchedHashes))
	for _, gc := range gitCommits {
		if !unfetchedSet[gc.Hash] {
			continue
		}
		newGitCommits = append(newGitCommits, gc)
		newHashes = append(newHashes, gc.Hash)
	}

	signerKeys := lookupSignerKeys(storageDir, newHashes)
	newCommits := make([]cache.Commit, 0, len(newGitCommits))
	candidates := make([]identity.VerifyCandidate, 0, len(newGitCommits))
	for _, gc := range newGitCommits {
		signer := identity.NormalizeSignerKey(signerKeys[gc.Hash])
		newCommits = append(newCommits, cache.Commit{
			Hash:        gc.Hash,
			RepoURL:     repoURL,
			Branch:      branch,
			AuthorName:  gc.Author,
			AuthorEmail: gc.Email,
			Message:     gc.Message,
			Timestamp:   gc.Timestamp,
			SignerKey:   signer,
		})
		if signer != "" && gc.Email != "" {
			candidates = append(candidates, identity.VerifyCandidate{
				RepoURL:   repoURL,
				Hash:      gc.Hash,
				SignerKey: signer,
				Email:     gc.Email,
			})
		}
	}

	if err := cache.InsertCommits(newCommits); err != nil {
		return 0, err
	}

	identity.VerifyCandidates(candidates, 4)
	backfillRepoSignerKeys(storageDir, repoURL)

	// Dispatch to extension processors
	for _, gc := range newGitCommits {
		msg := protocol.ParseMessage(gc.Message)
		for _, proc := range processors {
			proc(gc, msg, repoURL, branch)
		}
	}

	return len(newCommits), nil
}

// CleanRefname strips ref prefixes to produce a short branch name.
func CleanRefname(ref string) string {
	if ref == "" || ref == "HEAD" || strings.HasSuffix(ref, "/HEAD") {
		return ""
	}
	if strings.HasPrefix(ref, "refs/heads/") {
		return strings.TrimPrefix(ref, "refs/heads/")
	}
	if strings.HasPrefix(ref, "refs/remotes/origin/") {
		return strings.TrimPrefix(ref, "refs/remotes/origin/")
	}
	if strings.HasPrefix(ref, "refs/remotes/") {
		return strings.TrimPrefix(ref, "refs/remotes/")
	}
	if strings.HasPrefix(ref, "refs/tags/") {
		return "tags/" + strings.TrimPrefix(ref, "refs/tags/")
	}
	return ref
}

// processAllBranchCommits processes commits storing each with its actual refname branch.
func processAllBranchCommits(storageDir string, gitCommits []git.Commit, repoURL, fallbackBranch string, processors []CommitProcessor) (int, error) {
	hashes := make([]string, len(gitCommits))
	for i, c := range gitCommits {
		hashes[i] = c.Hash
	}
	unfetchedHashes, err := cache.FilterUnfetchedCommitsByRepo(repoURL, hashes)
	if err != nil {
		return 0, err
	}
	unfetchedSet := make(map[string]bool, len(unfetchedHashes))
	for _, h := range unfetchedHashes {
		unfetchedSet[h] = true
	}

	newGitCommits := make([]git.Commit, 0, len(gitCommits))
	commitBranches := make(map[string]string, len(gitCommits))
	newHashes := make([]string, 0, len(unfetchedHashes))
	for _, gc := range gitCommits {
		branch := CleanRefname(gc.Refname)
		if branch == "" {
			branch = fallbackBranch
		}
		commitBranches[gc.Hash] = branch
		if !unfetchedSet[gc.Hash] {
			continue
		}
		newGitCommits = append(newGitCommits, gc)
		newHashes = append(newHashes, gc.Hash)
	}

	signerKeys := lookupSignerKeys(storageDir, newHashes)
	newCommits := make([]cache.Commit, 0, len(newGitCommits))
	candidates := make([]identity.VerifyCandidate, 0, len(newGitCommits))
	for _, gc := range newGitCommits {
		signer := identity.NormalizeSignerKey(signerKeys[gc.Hash])
		newCommits = append(newCommits, cache.Commit{
			Hash:        gc.Hash,
			RepoURL:     repoURL,
			Branch:      commitBranches[gc.Hash],
			AuthorName:  gc.Author,
			AuthorEmail: gc.Email,
			Message:     gc.Message,
			Timestamp:   gc.Timestamp,
			SignerKey:   signer,
		})
		if signer != "" && gc.Email != "" {
			candidates = append(candidates, identity.VerifyCandidate{
				RepoURL:   repoURL,
				Hash:      gc.Hash,
				SignerKey: signer,
				Email:     gc.Email,
			})
		}
	}

	if err := cache.InsertCommits(newCommits); err != nil {
		return 0, err
	}

	identity.VerifyCandidates(candidates, 4)
	backfillRepoSignerKeys(storageDir, repoURL)

	for _, gc := range newGitCommits {
		branch := commitBranches[gc.Hash]
		msg := protocol.ParseMessage(gc.Message)
		for _, proc := range processors {
			proc(gc, msg, repoURL, branch)
		}
	}

	return len(newCommits), nil
}

// backfillRepoSignerKeys scans for legacy NULL-signer_key rows in this repo,
// extracts signer keys via git, updates the cache, and feeds the updated
// (signer_key, email) pairs to the verifier. Bounded per call by the identity
// package's batch limit. After every legacy row is backfilled, this is a cheap
// no-op (one indexed SELECT returning 0 rows).
func backfillRepoSignerKeys(storageDir, repoURL string) {
	if storageDir == "" || repoURL == "" {
		return
	}
	updated, candidates := identity.BackfillSignerKeys(storageDir, repoURL)
	if updated > 0 {
		log.Debug("backfilled signer keys", "repo", repoURL, "count", updated, "candidates", len(candidates))
	}
	if len(candidates) > 0 {
		identity.VerifyCandidates(candidates, 4)
	}
}

// lookupSignerKeys batch-fetches signing keys for commits via git log. Errors are
// non-fatal — unsigned commits and lookup failures both yield missing entries.
func lookupSignerKeys(storageDir string, hashes []string) map[string]string {
	if storageDir == "" || len(hashes) == 0 {
		return map[string]string{}
	}
	keys, err := git.GetCommitSignerKeys(storageDir, hashes)
	if err != nil {
		log.Debug("batch signer key lookup", "error", err, "count", len(hashes))
		return map[string]string{}
	}
	return keys
}
