// push.go - CLI command for publishing local changes (data + browser site) to a remote
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/clientpush"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// newPushCmd creates the command for publishing local changes to a remote.
func newPushCmd() *cobra.Command {
	var dryRun bool
	var noCode bool
	var noSite bool
	var siteOnly bool
	var allBranches bool
	var full bool

	cmd := &cobra.Command{
		Use:   "push [remote...]",
		Short: "Publish local changes (data + browser site) to one or more remotes",
		Long: `Publish all local GitMsg changes to the remote repository, and — for s3
remotes with the site.publish guard enabled — the browsable static site
alongside the data.

The target remotes are resolved in this order: the positional [remote...]
arguments, then git config gitsocial.pushRemote (multi-valued), then a heuristic
(origin if it's an s3 remote, else the first s3 remote alphabetically, else
origin). Set defaults with ` + "`gitsocial remote default <name...>`" + `. Several
remotes push sequentially: a failed remote is reported and skipped, and the
command exits non-zero if any remote failed.

This publishes:
  - Branch commits (posts, comments, reposts, quotes)
  - GitMsg refs (lists, configs)
  - Tags (all local tags)
  - Code branches: the default branch when it's ahead of the remote, and heads
    of open pull requests, so others can fetch the code your published data
    points at (--no-code skips)
  - The browser static site, for s3 remotes when the repo enables it with
    ` + "`gitsocial config site set publish true`" + ` (default off; a bucket with
    no site then gets one). --no-site skips per push, and ` + "`git config`" + `
    ` + "`gitsocial.pushSite false`" + ` opts a machine out persistently. Non-s3
    remotes skip this step silently.

A first push to an empty remote bootstraps the whole bucket with no extra
flags. Use --all-branches to publish every local branch, not just the default
branch and open-PR heads.

--full detaches a thin fork bucket (one pushed with
` + "`remote.<name>.gitsocial-thin`" + ` set): it uploads every object the bucket
left to its upstream, restores the stock-git ref advertisement, drops the
` + "`.gitsocial/upstream`" + ` marker, and clears the thin flag — the escape hatch
for an upstream that is going away. It is a no-op on a bucket that is not thin.

--site-only publishes only the browser site — the explicit site refresh, e.g.
to catch an already-pushed repo up right after enabling site.publish, without
pushing new data. Where a plain push silently skips the site for repos without
the guard, an explicit --site-only fails loudly when site.publish is off or
the remote is not an s3 remote.

Divergent histories on gitmsg/* branches (when two clones write between syncs)
are auto-merged — the empty-tree append-only shape of those branches makes the
merge conflict-free and preserves every commit hash on both sides. Code
branches (and --all-branches extras) are never auto-merged; a diverged head
(e.g. after a rebase) fails with a hint to force-push explicitly.

Examples:
  gitsocial push              # Publish to the resolved remote(s) (data + site)
  gitsocial push backup       # Publish to the remote named "backup"
  gitsocial push r2 s3local   # Publish to both "r2" and "s3local" in turn
  gitsocial push --dry-run    # Preview what would be pushed
  gitsocial push --no-code    # Skip code branches (default branch + PR heads)
  gitsocial push --no-site    # Skip the browser site
  gitsocial push --site-only  # Refresh only the browser site, no data push
  gitsocial push --all-branches   # Publish every local branch
  gitsocial push --full       # Detach a thin fork bucket (upload everything)`,
		Args: cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)

			// Resolve the target remotes: explicit positionals win, else the
			// (multi-valued) configured defaults / heuristic.
			remotes := args
			if len(remotes) == 0 {
				remotes = git.PushRemotes(cfg.WorkDir)
			}

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
			results := make([]*clientpush.Result, 0, len(remotes))
			failed := false
			for _, remote := range remotes {
				opts := clientpush.Options{
					Remote:      remote,
					DryRun:      dryRun,
					NoCode:      noCode,
					NoSite:      noSite,
					SiteOnly:    siteOnly,
					AllBranches: allBranches,
					Full:        full,
				}
				if !cfg.JSONOutput && !dryRun {
					resolved := clientpush.ResolveRemote(cfg.WorkDir, remote)
					fmt.Printf("Pushing to %s ...\n", resolved)
					if gitmsg.RemoteIsEmpty(cfg.WorkDir, resolved) {
						fmt.Printf("Publishing to empty remote %q ...\n", resolved)
					}
				}
				result, err := clientpush.Publish(cfg.WorkDir, opts, onBranch, siteProgress)
				if err != nil {
					failed = true
					if !cfg.JSONOutput {
						PrintError(cmd, fmt.Sprintf("push to %s: %v", clientpush.ResolveRemote(cfg.WorkDir, remote), err))
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
	cmd.Flags().BoolVar(&noCode, "no-code", false, "Skip code branches (default branch and open-PR heads)")
	cmd.Flags().BoolVar(&noSite, "no-site", false, "Skip the browser static site (s3 remotes)")
	cmd.Flags().BoolVar(&siteOnly, "site-only", false, "Publish only the browser site, no data push (errors when site.publish is off)")
	cmd.Flags().BoolVar(&allBranches, "all-branches", false, "Publish every local branch, not just the default branch and open-PR heads")
	cmd.Flags().BoolVar(&full, "full", false, "Detach a thin fork bucket: upload every object it lacks and restore its stock-git ref advertisement")
	cmd.MarkFlagsMutuallyExclusive("no-site", "site-only")

	return cmd
}

// printPushResult renders the combined data + site publish result for humans.
func printPushResult(result *clientpush.Result, dryRun bool) {
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
