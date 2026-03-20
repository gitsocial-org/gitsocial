// rebase.go - Rebase diverged gitmsg branches with ref rewriting
package gitmsg

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

type unpushedCommit struct {
	Hash    string
	Message string
	Branch  string
	Date    string
}

// rebaseDivergedBranches collects all unpushed commits across all gitmsg branches,
// resets branches to their remote tips, and recreates commits chronologically
// with rewritten refs to preserve reply chains, edits, and cross-references.
func RebaseDivergedBranches(workdir string, branches []string) error {
	var allUnpushed []unpushedCommit
	for _, branch := range branches {
		commits, err := getUnpushedCommitList(workdir, branch)
		if err != nil {
			log.Debug("skip branch for rebase", "branch", branch, "error", err)
			continue
		}
		allUnpushed = append(allUnpushed, commits...)
	}
	if len(allUnpushed) == 0 {
		return nil
	}

	// Sort chronologically (oldest first) — refs always point backward
	sort.SliceStable(allUnpushed, func(i, j int) bool {
		return allUnpushed[i].Date < allUnpushed[j].Date
	})

	// Reset all branches with unpushed commits to their remote tip
	resetBranches := map[string]bool{}
	for _, c := range allUnpushed {
		resetBranches[c.Branch] = true
	}
	for branch := range resetBranches {
		remoteTip, err := resolveRemoteTip(workdir, branch)
		if err != nil {
			log.Debug("no remote tip, skipping reset", "branch", branch)
			continue
		}
		if _, err := git.ExecGit(workdir, []string{"update-ref", "refs/heads/" + branch, remoteTip}); err != nil {
			return fmt.Errorf("reset %s to remote: %w", branch, err)
		}
	}

	// Recreate commits chronologically with rewritten refs
	hashMap := make(map[string]string, len(allUnpushed))
	for _, c := range allUnpushed {
		message := rewriteHashRefs(c.Message, hashMap)
		newHash, err := git.CreateCommitOnBranch(workdir, c.Branch, message)
		if err != nil {
			return fmt.Errorf("recreate commit on %s: %w", c.Branch, err)
		}
		hashMap[c.Hash] = newHash
	}

	// Invalidate workspace sync tip so next sync picks up new hashes
	repoURL := protocol.NormalizeURL(git.GetOriginURL(workdir))
	if repoURL != "" {
		_ = cache.SetSyncTip("workspace:"+repoURL, "")
	}

	log.Info("rebased diverged branches", "commits", len(allUnpushed), "branches", len(resetBranches))
	return nil
}

// getUnpushedCommitList returns unpushed commits on a branch (oldest first).
func getUnpushedCommitList(workdir, branch string) ([]unpushedCommit, error) {
	rangeSpec := "origin/" + branch + ".." + branch
	result, err := git.ExecGit(workdir, []string{
		"log", rangeSpec, "--reverse",
		"--format=%h%x1f%B%x1f%aI%x1e",
		"--abbrev=12",
	})
	if err != nil {
		return nil, err
	}
	return parseUnpushedOutput(result.Stdout, branch), nil
}

// parseUnpushedOutput parses git log output into unpushed commits.
func parseUnpushedOutput(output, branch string) []unpushedCommit {
	if output == "" {
		return nil
	}
	var commits []unpushedCommit
	for _, entry := range strings.Split(output, "\x1e") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "\x1f", 3)
		if len(parts) < 3 {
			continue
		}
		commits = append(commits, unpushedCommit{
			Hash:    strings.TrimSpace(parts[0]),
			Message: strings.TrimRight(parts[1], "\n"),
			Branch:  branch,
			Date:    strings.TrimSpace(parts[2]),
		})
	}
	return commits
}

// resolveRemoteTip returns the full hash of origin/<branch>.
func resolveRemoteTip(workdir, branch string) (string, error) {
	result, err := git.ExecGit(workdir, []string{"rev-parse", "origin/" + branch})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

// rewriteHashRefs replaces old 12-char commit hashes with new ones in a message.
func rewriteHashRefs(message string, hashMap map[string]string) string {
	for oldHash, newHash := range hashMap {
		if oldHash != newHash {
			message = strings.ReplaceAll(message, oldHash, newHash)
		}
	}
	return message
}
