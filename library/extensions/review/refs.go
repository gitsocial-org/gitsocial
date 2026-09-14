// refs.go - Branch-tip resolution for the workspace and for fork URLs
package review

import (
	"errors"
	"fmt"

	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// resolveBranchTip returns branch's remote tip in repoURL; a branch gone from the remote is an error. addresses is gitmsg.ForkAddresses, read once by the caller.
func resolveBranchTip(workdir, repoURL, branch string, addresses map[string]string) (string, error) {
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
		// A registered fork is reached at its address, which may be the only URL that serves it.
		address := fetch.ForkAddress(addresses, normalizedURL)
		if tip, err := git.ReadRemoteRef(workdir, address, branch); err == nil && tip != "" {
			return tip, nil
		}
	}
	return "", fmt.Errorf("branch %q not found on remote %s", branch, repoURL)
}

// resolveTipForWrite returns the remote tip, falling back to a local workspace branch.
func resolveTipForWrite(workdir, workspaceURL string, parsed protocol.ParsedRef, addresses map[string]string) (string, error) {
	if parsed.Type != protocol.RefTypeBranch || parsed.Value == "" {
		return "", fmt.Errorf("not a branch ref")
	}
	repoURL := parsed.Repository
	if repoURL == "" {
		repoURL = workspaceURL
	}
	if tip, err := resolveBranchTip(workdir, repoURL, parsed.Value, addresses); err == nil {
		return tip, nil
	} else if !isWorkspaceURL(workdir, protocol.NormalizeURL(repoURL)) {
		return "", err
	}
	return git.ReadRef(workdir, parsed.Value)
}

// resolveTipForAuthor prefers a local workspace branch, so an unpushed tip is the proposed state.
func resolveTipForAuthor(workdir, workspaceURL string, parsed protocol.ParsedRef, addresses map[string]string) (string, error) {
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
	return resolveBranchTip(workdir, repoURL, parsed.Value, addresses)
}

// resolveTipForObservation returns the remote tip with no local fallback, so a deletion surfaces.
func resolveTipForObservation(workdir, workspaceURL string, parsed protocol.ParsedRef, addresses map[string]string) (string, error) {
	if parsed.Type != protocol.RefTypeBranch || parsed.Value == "" {
		return "", fmt.Errorf("not a branch ref")
	}
	repoURL := parsed.Repository
	if repoURL == "" {
		repoURL = workspaceURL
	}
	return resolveBranchTip(workdir, repoURL, parsed.Value, addresses)
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
