// refs.go - Branch-tip resolution for the workspace and for fork URLs
package review

import (
	"errors"
	"fmt"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// ResolveBranchTip returns branch's remote tip in repoURL; a branch gone from the remote is an error.
func ResolveBranchTip(workdir, repoURL, branch string) (string, error) {
	if branch == "" {
		return "", errors.New("branch required")
	}
	normalizedURL := protocol.NormalizeURL(repoURL)
	if isWorkspaceURL(workdir, normalizedURL) {
		if tip, err := git.ReadRef(workdir, "refs/remotes/origin/"+branch); err == nil && tip != "" {
			return tip, nil
		}
		return "", fmt.Errorf("branch %q not found in refs/remotes/origin (run `git fetch`?)", branch)
	}
	if remoteName := findRemoteForURL(workdir, normalizedURL); remoteName != "" {
		if tip, err := git.ReadRef(workdir, "refs/remotes/"+remoteName+"/"+branch); err == nil && tip != "" {
			return tip, nil
		}
	}
	if normalizedURL != "" {
		if tip, err := git.ReadRemoteRef(workdir, normalizedURL, branch); err == nil && tip != "" {
			return tip, nil
		}
	}
	return "", fmt.Errorf("branch %q not found on remote %s", branch, repoURL)
}

// resolveTipForWrite returns the remote tip, falling back to a local workspace branch.
func resolveTipForWrite(workdir, workspaceURL string, parsed protocol.ParsedRef) (string, error) {
	if parsed.Type != protocol.RefTypeBranch || parsed.Value == "" {
		return "", fmt.Errorf("not a branch ref")
	}
	repoURL := parsed.Repository
	if repoURL == "" {
		repoURL = workspaceURL
	}
	if tip, err := ResolveBranchTip(workdir, repoURL, parsed.Value); err == nil {
		return tip, nil
	} else if !isWorkspaceURL(workdir, protocol.NormalizeURL(repoURL)) {
		return "", err
	}
	return git.ReadRef(workdir, parsed.Value)
}

// resolveTipForAuthor prefers a local workspace branch, so an unpushed tip is the proposed state.
func resolveTipForAuthor(workdir, workspaceURL string, parsed protocol.ParsedRef) (string, error) {
	if parsed.Type != protocol.RefTypeBranch || parsed.Value == "" {
		return "", fmt.Errorf("not a branch ref")
	}
	repoURL := parsed.Repository
	if repoURL == "" {
		repoURL = workspaceURL
	}
	if isWorkspaceURL(workdir, protocol.NormalizeURL(repoURL)) {
		if tip, err := git.ReadRef(workdir, parsed.Value); err == nil && tip != "" {
			return tip, nil
		}
	}
	return ResolveBranchTip(workdir, repoURL, parsed.Value)
}

// resolveTipForObservation returns the remote tip with no local fallback, so a deletion surfaces.
func resolveTipForObservation(workdir, workspaceURL string, parsed protocol.ParsedRef) (string, error) {
	if parsed.Type != protocol.RefTypeBranch || parsed.Value == "" {
		return "", fmt.Errorf("not a branch ref")
	}
	repoURL := parsed.Repository
	if repoURL == "" {
		repoURL = workspaceURL
	}
	return ResolveBranchTip(workdir, repoURL, parsed.Value)
}

// findRemoteForURL returns the remote whose URL matches normalizedURL, or the empty string.
func findRemoteForURL(workdir, normalizedURL string) string {
	if normalizedURL == "" {
		return ""
	}
	remotes, err := git.ListRemotes(workdir)
	if err != nil {
		return ""
	}
	for _, r := range remotes {
		if protocol.NormalizeURL(r.URL) == normalizedURL {
			return r.Name
		}
	}
	return ""
}

// isWorkspaceURL reports whether normalizedURL, or the empty shorthand, names the workdir's origin.
func isWorkspaceURL(workdir, normalizedURL string) bool {
	if normalizedURL == "" {
		return true
	}
	wsURL := gitmsg.ResolveRepoURL(workdir)
	return wsURL == normalizedURL
}
