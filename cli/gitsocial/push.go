// push.go - CLI command for sending local data to a remote and rebuilding the site
package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/client"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// newPushCmd creates the push command.
func newPushCmd() *cobra.Command {
	var dryRun bool
	var noCode bool
	var noSite bool
	var siteOnly bool
	var allBranches bool
	var full bool

	cmd := &cobra.Command{
		Use:   "push [remote...]",
		Short: "Send local changes to remotes",
		Long: `Send local GitMsg data to one or more remotes. A push to an s3 remote
with site.publish then rebuilds the browser static site.

Remotes resolve in order: the arguments, git config gitsocial.pushRemote,
then origin, or the first s3 remote when origin is not one. Diverged
gitmsg/* branches merge automatically; diverged code branches fail with a
hint. See documentation/S3.md for remotes and thin fork buckets.

Each push:
  branch commits  posts, comments, reposts, quotes
  state refs      lists and configs under refs/gitmsg/
  tags            every local tag
  code branches   the default branch when it is ahead, and open PR heads
  the site        rebuilt on an s3 remote with site.publish

Examples:
  gitsocial push                 # resolved remotes, data and site
  gitsocial push r2 backup       # named remotes, in order
  gitsocial push --dry-run       # print the plan, send nothing
  gitsocial push --no-code       # data and site, no code branches
  gitsocial push --site-only     # rebuild the site, send no refs
  gitsocial push --all-branches  # every local branch
  gitsocial push --full          # detach a thin fork bucket`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)

			remotes, resolution := client.ResolveRemotes(cfg.WorkDir, args)
			printRemoteHint(cmd.ErrOrStderr(), cfg.WorkDir, remotes, resolution)

			if dryRun && !cfg.JSONOutput {
				fmt.Fprintln(cmd.OutOrStdout(), "Dry run - no changes will be pushed")
			}

			// Progress goes to stderr, and --json keeps machine output clean.
			var siteProgress objstore.Progress
			var onBranch func(remote, branch string, done, total int)
			siteDone := func() {}
			if !cfg.JSONOutput {
				siteProgress, siteDone = objstore.WriterProgress(cmd.ErrOrStderr())
				onBranch = func(remote, branch string, done, total int) {
					siteProgress(remote+" "+branch, done, total)
				}
			}
			var onRemote func(remote string)
			if !cfg.JSONOutput && !dryRun {
				onRemote = func(remote string) {
					fmt.Fprintf(cmd.OutOrStdout(), "Pushing to %s ...\n", remote)
					if gitmsg.RemoteIsEmpty(cfg.WorkDir, remote) {
						fmt.Fprintf(cmd.OutOrStdout(), "Sending to empty remote %q ...\n", remote)
					}
				}
			}

			opts := client.Options{
				DryRun:      dryRun,
				NoCode:      noCode,
				NoSite:      noSite,
				SiteOnly:    siteOnly,
				AllBranches: allBranches,
				Full:        full,
			}
			results, err := client.PublishAll(cfg.WorkDir, remotes, opts, onRemote, onBranch, siteProgress)
			siteDone()

			if cfg.JSONOutput {
				// One remote keeps the object shape, several return the array.
				if len(remotes) == 1 && len(results) == 1 {
					if err := PrintJSON(cmd, results[0]); err != nil {
						return err
					}
				} else {
					if err := PrintJSON(cmd, results); err != nil {
						return err
					}
				}
			} else {
				for i := range results {
					printPushResult(cmd.OutOrStdout(), &results[i], dryRun)
				}
			}

			if err != nil {
				if !cfg.JSONOutput {
					PrintError(cmd, err.Error())
				}
				return exit(ExitError)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without pushing")
	cmd.Flags().BoolVar(&noCode, "no-code", false, "Skip code branches")
	cmd.Flags().BoolVar(&noSite, "no-site", false, "Skip the site rebuild for this push")
	cmd.Flags().BoolVar(&siteOnly, "site-only", false, "Rebuild the site, send no refs")
	cmd.Flags().BoolVar(&allBranches, "all-branches", false, "Send every local branch")
	cmd.Flags().BoolVar(&full, "full", false, "Detach a thin fork bucket and upload every object")
	cmd.MarkFlagsMutuallyExclusive("no-site", "site-only")

	return cmd
}

// printRemoteHint writes the one hint a resolution earns, once per command.
func printRemoteHint(out io.Writer, workdir string, remotes []string, resolution git.PushResolution) {
	switch resolution {
	case git.PushAmbiguous:
		fmt.Fprintf(out, "gitsocial: several s3 remotes, pushing to %q. Choose one with: gitsocial remote default <name>\n", remotes[0])
	case git.PushStale:
		configured := strings.Join(git.ConfiguredPushRemotes(workdir), " ")
		fmt.Fprintf(out, "gitsocial: configured push remote %q does not exist, pushing to %q. Set it with: gitsocial remote default <name>\n", configured, remotes[0])
	}
}

// printPushResult renders the push and site result for humans.
func printPushResult(out io.Writer, result *client.Result, dryRun bool) {
	p := result.Push
	nothing := p.Commits == 0 && p.CodeCommits == 0 && p.Refs == 0 && p.Tags == 0 && p.AllBranches == 0
	if nothing && !result.Site.Published {
		fmt.Fprintln(out, "Nothing to push")
		if result.Site.Err != nil {
			fmt.Fprintf(out, "Site: failed: %v\n", result.Site.Err)
		} else if result.Site.Skipped != "" {
			fmt.Fprintf(out, "Site: skipped (%s)\n", result.Site.Skipped)
		}
		return
	}

	if dryRun {
		fmt.Fprintf(out, "Would push to %s (%s)\n", p.Remote, p.RemoteURL)
	} else {
		fmt.Fprintf(out, "Pushed to %s (%s)\n", p.Remote, p.RemoteURL)
	}
	if p.Commits > 0 {
		fmt.Fprintf(out, "  Commits: %d\n", p.Commits)
	}
	if p.CodeCommits > 0 {
		fmt.Fprintf(out, "  Code commits: %d\n", p.CodeCommits)
	}
	if p.AllBranches > 0 {
		fmt.Fprintf(out, "  Branches (--all-branches): %d\n", p.AllBranches)
	}
	if p.Refs > 0 {
		fmt.Fprintf(out, "  Refs: %d\n", p.Refs)
	}
	if p.Tags > 0 {
		fmt.Fprintf(out, "  Tags: %d\n", p.Tags)
	}

	switch {
	case result.Site.Published && !result.Site.Complete:
		fmt.Fprintln(out, "Site: published (incomplete: a bootstrap is still in progress, push again to finish)")
	case result.Site.Published:
		fmt.Fprintln(out, "Site: published")
	case result.Site.Err != nil:
		fmt.Fprintf(out, "Site: failed: %v\n", result.Site.Err)
	case result.Site.Skipped != "":
		fmt.Fprintf(out, "Site: skipped (%s)\n", result.Site.Skipped)
	}

	if !dryRun {
		fmt.Fprintln(out, "Done.")
	}
}
