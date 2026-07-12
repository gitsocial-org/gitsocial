// site.go - gitsocial site: static browser read-surface on s3 remotes
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// defaultBranchStats returns the current branch and every regular commit's
// author time (unix seconds) on it — the served default branch in the bucket.
// The browser buckets these into the analytics activity chart (a "commits"
// series) with the same period logic it uses for items, and the count is len().
func defaultBranchStats(workdir string) (string, []int, error) {
	br, err := git.ExecGit(workdir, []string{"rev-parse", "--abbrev-ref", "HEAD"})
	if err != nil {
		return "", nil, err
	}
	lr, err := git.ExecGit(workdir, []string{"log", "--format=%ct", "HEAD"})
	if err != nil {
		return "", nil, err
	}
	fields := strings.Fields(lr.Stdout)
	times := make([]int, 0, len(fields))
	for _, f := range fields {
		if n, e := strconv.Atoi(f); e == nil {
			times = append(times, n)
		}
	}
	return strings.TrimSpace(br.Stdout), times, nil
}

// newSiteCmd creates the `site` command group.
func newSiteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "site",
		Short: "Manage the static browser read-surface on s3 remotes",
	}
	cmd.AddCommand(newSitePushCmd())
	return cmd
}

// newSitePushCmd uploads the embedded site shell to an s3 remote's bucket.
func newSitePushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push [remote]",
		Short: "Upload the browser read-surface to an s3 remote",
		Long: `Upload the embedded static site to an s3 remote's bucket, alongside the
repo data. Anyone can then browse the repo's timeline, issues, PRs, and
releases from the bucket's public domain with no gitsocial install. Remote
defaults to "origin".

The page reads the bucket directly, so it stays current with every push; the
bucket (or its public domain, e.g. r2.dev or a custom domain on Cloudflare R2)
must allow public reads for visitors without credentials.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			name := "origin"
			if len(args) == 1 {
				name = args[0]
			}
			result, err := git.ExecGit(cfg.WorkDir, []string{"remote", "get-url", name})
			if err != nil {
				PrintError(cmd, fmt.Sprintf("remote %q: %v", name, err))
				os.Exit(ExitError)
			}
			remoteURL := result.Stdout
			if !strings.HasPrefix(remoteURL, "s3://") {
				PrintError(cmd, fmt.Sprintf("remote %q is not an s3 remote: %s", name, remoteURL))
				os.Exit(ExitError)
			}
			// Live progress to stderr (same TTY/non-TTY policy as the git-spawned
			// helper); suppressed in --json so machine output stays clean.
			var progress objstore.Progress
			var progressDone = func() {}
			if !cfg.JSONOutput {
				progress, progressDone = objstore.StderrProgress()
			}
			err = objstore.PushSite(remoteURL, objstore.HelperEnvFromOS(), cfg.WorkDir, progress)
			progressDone()
			if err != nil {
				PrintError(cmd, fmt.Sprintf("push site to %s: %v", remoteURL, err))
				os.Exit(ExitError)
			}
			// Point the bucket HEAD at the repo's real default branch (not an assumed
			// "main"), and publish push-time stats (the default branch's commit count +
			// times) the browser can't cheaply derive. Best-effort: never fails the push.
			if branch, times, err := defaultBranchStats(cfg.WorkDir); err == nil {
				if err := objstore.SetRemoteHead(remoteURL, objstore.HelperEnvFromOS(), branch); err != nil {
					PrintError(cmd, fmt.Sprintf("set default HEAD: %v", err))
				}
				stats := map[string]any{"branch": branch, "commits": len(times), "commitTimes": times}
				if err := objstore.WriteSiteStats(remoteURL, objstore.HelperEnvFromOS(), stats); err != nil {
					PrintError(cmd, fmt.Sprintf("write site stats: %v", err))
				}
			}
			PrintSuccess(cmd, fmt.Sprintf("Uploaded browser read-surface to %s", remoteURL))
		},
	}
}
