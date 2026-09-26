// site_localwalk.go - local-first object reads and git commands for the site passes.
package site

import (
	"os/exec"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// getCommit returns one commit for the walk, preferring the local odb and falling back to the bucket on a miss; both paths share parseBucketCommit.
func getCommit(src *objstore.LocalCommitSource, client *objstore.Client, prefix, sha string) (bucketCommit, error) {
	if body, ok := src.Commit(sha); ok {
		return parseBucketCommit(sha, body)
	}
	return getBucketCommit(client, prefix, sha)
}

// siteGitCommand builds a git command over the source's odb, with lazy fetch off so a partial clone answers from local objects only.
func siteGitCommand(src *objstore.LocalCommitSource, args ...string) *exec.Cmd {
	if workdir := src.Workdir(); workdir != "" {
		args = append([]string{"-C", workdir}, args...)
	}
	cmd := exec.Command("git", args...)
	cmd.Env = append(cmd.Environ(), "GIT_NO_LAZY_FETCH=1")
	if gitDir := src.GitDir(); gitDir != "" {
		cmd.Env = append(cmd.Env, "GIT_DIR="+gitDir)
	}
	return cmd
}
