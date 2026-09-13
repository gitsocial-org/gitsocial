// push.go - CLI command for sending local data to a remote and rebuilding the site
package main

import (
	"fmt"
	"os"
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
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)

			// Resolve the target remotes: explicit positionals win, else the
			// (multi-valued) configured defaults / heuristic.
			remotes := args
			resolution := git.PushConfigured
			if len(remotes) == 0 {
				remotes, resolution = git.ResolvePushRemotes(cfg.WorkDir)
			}
			printRemoteHint(cfg.WorkDir, remotes, resolution)

			if dryRun && !cfg.JSONOutput {
				fmt.Println("Dry run - no changes will be pushed")
			}

			// Live per-branch and site-upload progress to stderr (same policy as
			// the git-spawned helper); suppressed under --json so machine output
			// stays clean.
			var siteProgress objstore.Progress
			var onBranch gitmsg.PushBranchProgress
			siteDone := func() {}
			if !cfg.JSONOutput {
				siteProgress, siteDone = objstore.StderrProgress()
				onBranch = func(branch string, done, total int) { siteProgress(branch, done, total) }
			}

			// Push each remote in turn: report per remote, continue past a failure,
			// and exit non-zero if any failed. Sequential keeps progress readable.
			results := make([]*client.Result, 0, len(remotes))
			failed := false
			for _, remote := range remotes {
				opts := client.Options{
					Remote:      remote,
					DryRun:      dryRun,
					NoCode:      noCode,
					NoSite:      noSite,
					SiteOnly:    siteOnly,
					AllBranches: allBranches,
					Full:        full,
				}
				if !cfg.JSONOutput && !dryRun {
					resolved := client.ResolveRemote(cfg.WorkDir, remote)
					fmt.Printf("Pushing to %s ...\n", resolved)
					if gitmsg.RemoteIsEmpty(cfg.WorkDir, resolved) {
						fmt.Printf("Sending to empty remote %q ...\n", resolved)
					}
				}
				result, err := client.Publish(cfg.WorkDir, opts, onBranch, siteProgress)
				if err != nil {
					failed = true
					if !cfg.JSONOutput {
						PrintError(cmd, fmt.Sprintf("push to %s: %v", client.ResolveRemote(cfg.WorkDir, remote), err))
					}
					continue
				}
				results = append(results, result)
				if !cfg.JSONOutput {
					printPushResult(result, dryRun)
				}
			}
			siteDone()

			if cfg.JSONOutput {
				// Single remote keeps the object shape for existing consumers; a
				// multi-remote push returns the array of per-remote results.
				if len(remotes) == 1 && len(results) == 1 {
					PrintJSON(results[0])
				} else {
					PrintJSON(results)
				}
			}

			if failed {
				os.Exit(ExitError)
			}
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
func printRemoteHint(workdir string, remotes []string, resolution git.PushResolution) {
	switch resolution {
	case git.PushAmbiguous:
		fmt.Fprintf(os.Stderr, "gitsocial: several s3 remotes, pushing to %q. Choose one with: gitsocial remote default <name>\n", remotes[0])
	case git.PushStale:
		configured := strings.Join(git.ConfiguredPushRemotes(workdir), " ")
		fmt.Fprintf(os.Stderr, "gitsocial: configured push remote %q does not exist, pushing to %q. Set it with: gitsocial remote default <name>\n", configured, remotes[0])
	}
}

// printPushResult renders the push and site result for humans.
func printPushResult(result *client.Result, dryRun bool) {
	p := result.Push
	nothing := p.Commits == 0 && p.CodeCommits == 0 && p.Refs == 0 && p.Tags == 0 && p.AllBranches == 0
	if nothing && !result.Site.Published {
		fmt.Println("Nothing to push")
		if result.Site.Err != nil {
			fmt.Printf("Site: failed: %v\n", result.Site.Err)
		} else if result.Site.Skipped != "" {
			fmt.Printf("Site: skipped (%s)\n", result.Site.Skipped)
		}
		return
	}

	if dryRun {
		fmt.Printf("Would push to %s (%s)\n", p.Remote, p.RemoteURL)
	} else {
		fmt.Printf("Pushed to %s (%s)\n", p.Remote, p.RemoteURL)
	}
	if p.Commits > 0 {
		fmt.Printf("  Commits: %d\n", p.Commits)
	}
	if p.CodeCommits > 0 {
		fmt.Printf("  Code commits: %d\n", p.CodeCommits)
	}
	if p.AllBranches > 0 {
		fmt.Printf("  Branches (--all-branches): %d\n", p.AllBranches)
	}
	if p.Refs > 0 {
		fmt.Printf("  Refs: %d\n", p.Refs)
	}
	if p.Tags > 0 {
		fmt.Printf("  Tags: %d\n", p.Tags)
	}

	switch {
	case result.Site.Published && !result.Site.Complete:
		fmt.Println("Site: published (incomplete: a bootstrap is still in progress, push again to finish)")
	case result.Site.Published:
		fmt.Println("Site: published")
	case result.Site.Err != nil:
		fmt.Printf("Site: failed: %v\n", result.Site.Err)
	case result.Site.Skipped != "":
		fmt.Printf("Site: skipped (%s)\n", result.Site.Skipped)
	}

	if !dryRun {
		fmt.Println("Done.")
	}
}
