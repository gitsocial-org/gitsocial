// process.go - Generic commit processing with extension dispatch
package fetch

import (
	"strings"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// CommitProcessor is called for each newly fetched commit after it's inserted into core_commits.
// Extensions register processors to handle their own commit types (e.g., social posts, PM issues).
// The msg parameter may be nil if the commit has no GitMsg header.
type CommitProcessor func(commit git.Commit, msg *protocol.Message, repoURL, branch string)

// ProcessCommits filters unfetched commits, inserts them into core_commits, and dispatches to processors.
func ProcessCommits(gitCommits []git.Commit, repoURL, branch string, processors []CommitProcessor) (int, error) {
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

	newCommits := make([]cache.Commit, 0, len(gitCommits))
	newGitCommits := make([]git.Commit, 0, len(gitCommits))
	for _, gc := range gitCommits {
		if !unfetchedSet[gc.Hash] {
			continue
		}
		newCommits = append(newCommits, cache.Commit{
			Hash:        gc.Hash,
			RepoURL:     repoURL,
			Branch:      branch,
			AuthorName:  gc.Author,
			AuthorEmail: gc.Email,
			Message:     gc.Message,
			Timestamp:   gc.Timestamp,
		})
		newGitCommits = append(newGitCommits, gc)
	}

	if err := cache.InsertCommits(newCommits); err != nil {
		return 0, err
	}

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
	if ref == "" {
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
func processAllBranchCommits(gitCommits []git.Commit, repoURL, fallbackBranch string, processors []CommitProcessor) (int, error) {
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

	newCommits := make([]cache.Commit, 0, len(gitCommits))
	newGitCommits := make([]git.Commit, 0, len(gitCommits))
	commitBranches := make(map[string]string, len(gitCommits))
	for _, gc := range gitCommits {
		branch := CleanRefname(gc.Refname)
		if branch == "" {
			branch = fallbackBranch
		}
		commitBranches[gc.Hash] = branch
		if !unfetchedSet[gc.Hash] {
			continue
		}
		newCommits = append(newCommits, cache.Commit{
			Hash:        gc.Hash,
			RepoURL:     repoURL,
			Branch:      branch,
			AuthorName:  gc.Author,
			AuthorEmail: gc.Email,
			Message:     gc.Message,
			Timestamp:   gc.Timestamp,
		})
		newGitCommits = append(newGitCommits, gc)
	}

	if err := cache.InsertCommits(newCommits); err != nil {
		return 0, err
	}

	for _, gc := range newGitCommits {
		branch := commitBranches[gc.Hash]
		msg := protocol.ParseMessage(gc.Message)
		for _, proc := range processors {
			proc(gc, msg, repoURL, branch)
		}
	}

	return len(newCommits), nil
}
