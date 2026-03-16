// refs.go - Git ref operations for GitMsg protocol data
package gitmsg

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/gitsocial-org/gitsocial/core/git"
)

type UnpushedCounts struct {
	Posts int `json:"posts"`
	Lists int `json:"lists"`
}

// GetUnpushedCounts returns counts of unpushed posts and lists.
func GetUnpushedCounts(workdir, branch string) (*UnpushedCounts, error) {
	counts := &UnpushedCounts{}

	if branch != "" {
		result, err := git.ExecGit(workdir, []string{
			"rev-list", "--count",
			"origin/" + branch + ".." + branch,
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

	remoteRefs, err := getRemoteGitMsgRefs(workdir)
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

// getRemoteGitMsgRefs returns all remote gitmsg refs with their hashes.
func getRemoteGitMsgRefs(workdir string) (map[string]string, error) {
	result, err := git.ExecGit(workdir, []string{
		"ls-remote",
		"origin",
		"refs/gitmsg/*",
	})
	if err != nil {
		return nil, err
	}

	return parseRemoteOutput(result.Stdout), nil
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
