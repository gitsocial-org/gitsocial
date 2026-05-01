// refs.go - Symmetric branch-tip resolution for any repo URL.
//
// `ResolveBranchTip` is the single entry point for "what is the current tip
// of branch X on remote Y." It treats the workspace's origin and any
// registered fork uniformly: a local remote-tracking ref wins when one of
// the workdir's git remotes points at the URL; otherwise we ls-remote
// against the URL. Strictly remote — refs/heads/<branch> is never
// consulted, so observation paths can rely on this returning an error
// when a branch has been deleted on the remote (no local-fallback masking).
// `resolveTipForWrite` adds the local-ref fallback for write paths that
// want to capture unpushed work.
package review

import (
	"errors"
	"fmt"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// ResolveBranchTip returns the current remote tip of `branch` in `repoURL`.
//
// Resolution depends on whether repoURL refers to the workspace's own
// origin or to a separate (fork) URL:
//
//   - Workspace URL: read refs/remotes/origin/<branch> only. Never runs
//     ls-remote — the user already has a fast local cache populated by
//     `gitsocial fetch` / `git fetch origin`. Forcing a network round-trip
//     here would surprise interactive callers (`pr create`, `pr update`)
//     and burn time against unreachable hosts in tests / offline use.
//   - Cross-fork URL: prefer a local remote-tracking ref when any of the
//     workdir's git remotes happens to point at repoURL; otherwise
//     ls-remote against the URL. Network is the only authoritative source
//     for forks the user hasn't checked out.
//
// Returns an error when no source resolves the branch — including when the
// branch was deleted on the remote. Callers that want a local-ref fallback
// (e.g., CreatePR / UpdatePRTips capturing unpushed work) must apply it
// explicitly via resolveTipForWrite.
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

// resolveTipForWrite returns the tip preferring remote (via
// ResolveBranchTip), falling back to refs/heads/<branch> when repoURL
// names the workspace and the remote couldn't be consulted at all. This is
// the resolver used by CreatePR / UpdatePRTips so a user can record an
// unpushed local branch — observation paths intentionally do NOT use this.
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

// resolveTipForObservation returns the strict remote tip for a parsed PR
// ref, normalizing the empty-Repository shorthand to the workspace URL.
// Wraps ResolveBranchTip without a local fallback so deletions surface as
// errors (which observation translates into HeadExists/BaseExists = false).
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

// findRemoteForURL returns the git remote name (e.g. "origin") whose URL
// matches normalizedURL after normalization, or "" if none matches.
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

// isWorkspaceURL reports whether normalizedURL refers to the workdir's own
// origin (or is empty, the protocol-level "this repo" shorthand). Uses the
// memoized workspace URL resolver so hot paths (ObserveLivePR, observation
// refresh) don't shell out to `git remote -v` per call.
func isWorkspaceURL(workdir, normalizedURL string) bool {
	if normalizedURL == "" {
		return true
	}
	wsURL := gitmsg.ResolveRepoURL(workdir)
	return wsURL == normalizedURL
}
