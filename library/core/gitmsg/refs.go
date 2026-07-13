// refs.go - Git ref operations for GitMsg protocol data
package gitmsg

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

type UnpushedCounts struct {
	Posts int `json:"posts"`
	Lists int `json:"lists"`
}

// GetUnpushedCounts returns counts of posts and lists not yet on the push remote.
func GetUnpushedCounts(workdir, branch string) (*UnpushedCounts, error) {
	counts := &UnpushedCounts{}
	remote := git.PushRemote(workdir)

	if branch != "" {
		result, err := git.ExecGit(workdir, []string{
			"rev-list", "--count",
			remote + "/" + branch + ".." + branch,
		})
		if err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(result.Stdout)); err == nil {
				counts.Posts = n
			} else {
				slog.Debug("parse unpushed count", "error", err, "output", result.Stdout)
			}
		} else {
			// Remote branch doesn't exist yet - all local commits are unpushed
			result, err = git.ExecGit(workdir, []string{"rev-list", "--count", branch})
			if err == nil {
				if n, err := strconv.Atoi(strings.TrimSpace(result.Stdout)); err == nil {
					counts.Posts = n
				} else {
					slog.Debug("parse unpushed count fallback", "error", err, "output", result.Stdout)
				}
			}
		}
	}

	localRefs, err := getLocalGitMsgRefs(workdir)
	if err != nil {
		return counts, nil
	}

	remoteRefs, err := getRemoteGitMsgRefs(workdir, remote)
	if err != nil {
		remoteRefs = make(map[string]string)
	}

	for ref, localHash := range localRefs {
		remoteHash, exists := remoteRefs[ref]
		if !exists || localHash != remoteHash {
			if strings.Contains(ref, "/lists/") {
				counts.Lists++
			}
		}
	}

	return counts, nil
}

// getLocalGitMsgRefs returns all local gitmsg refs with their hashes.
func getLocalGitMsgRefs(workdir string) (map[string]string, error) {
	result, err := git.ExecGit(workdir, []string{
		"for-each-ref",
		"--format=%(refname) %(objectname)",
		"refs/gitmsg/",
	})
	if err != nil {
		return nil, err
	}

	return parseRefOutput(result.Stdout), nil
}

// trackingPrefix returns the local namespace mirroring the remote's
// refs/gitmsg/* state refs, as last seen by a push or fetch. Deliberately
// outside refs/remotes/<remote>/: there the gitmsg/<ext> branch tracking refs
// (e.g. .../gitmsg/release) block child mirrors (.../gitmsg/release/0.1.0/artifacts)
// with git's directory/file ref conflict, so those mirrors could never be
// written and their refs read as unpushed forever.
func trackingPrefix(remote string) string {
	return "refs/gitsocial/tracking/" + remote + "/gitmsg/"
}

// TrackingRefspec returns the fetch refspec that mirrors the remote's
// refs/gitmsg/* state refs into the tracking namespace the offline push
// preview reads (see trackingPrefix).
func TrackingRefspec(remote string) string {
	return "+refs/gitmsg/*:" + trackingPrefix(remote) + "*"
}

// getRemoteGitMsgRefs returns the remote's gitmsg state refs from the local
// tracking mirror (no network calls). Reads the legacy refs/remotes/<remote>/gitmsg/
// mirror first, then overlays the current tracking namespace, so refs mirrored
// before the namespace move still count as pushed.
func getRemoteGitMsgRefs(workdir, remote string) (map[string]string, error) {
	refs := make(map[string]string)
	legacy, err := git.ExecGit(workdir, []string{
		"for-each-ref",
		"--format=%(refname) %(objectname)",
		"refs/remotes/" + remote + "/gitmsg/",
	})
	if err == nil {
		// Convert refs/remotes/<remote>/gitmsg/X → refs/gitmsg/X to match local ref format
		for ref, hash := range parseRefOutput(legacy.Stdout) {
			if local := strings.TrimPrefix(ref, "refs/remotes/"+remote+"/"); local != ref {
				refs["refs/"+local] = hash
			}
		}
	}

	result, err := git.ExecGit(workdir, []string{
		"for-each-ref",
		"--format=%(refname) %(objectname)",
		trackingPrefix(remote),
	})
	if err != nil {
		return refs, err
	}
	for ref, hash := range parseRefOutput(result.Stdout) {
		if local := strings.TrimPrefix(ref, trackingPrefix(remote)); local != ref {
			refs["refs/gitmsg/"+local] = hash
		}
	}
	return refs, nil
}

// parseRefOutput parses for-each-ref output into a map.
func parseRefOutput(output string) map[string]string {
	refs := make(map[string]string)
	if output == "" {
		return refs
	}

	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			refs[parts[0]] = parts[1]
		}
	}

	return refs
}

// parseRemoteOutput parses ls-remote output into a map.
func parseRemoteOutput(output string) map[string]string {
	refs := make(map[string]string)
	if output == "" {
		return refs
	}

	for _, line := range strings.Split(output, "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			refs[parts[1]] = parts[0]
		}
	}

	return refs
}
