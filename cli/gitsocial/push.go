// push.go - CLI command for publishing local changes (data + browser site) to a remote
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/clientpush"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// newPushCmd creates the command for publishing local changes to a remote.
func newPushCmd() *cobra.Command {
	var dryRun bool
	var noCode bool
	var noSite bool
	var allBranches bool

	cmd := &cobra.Command{
		Use:   "push [remote]",
		Short: "Publish local changes (data + browser site) to a remote",
		Long: `Publish all local GitMsg changes to the remote repository, and — for s3
remotes with the site.publish guard enabled — the browsable static site
alongside the data.

The target remote is resolved in this order: the positional [remote] argument,
then git config gitsocial.pushRemote, then a heuristic (origin if it's an s3
remote, else the first s3 remote alphabetically, else origin). Set a default
with ` + "`gitsocial remote default <name>`" + `.

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
flags. Use --all to publish every local branch (wholesale mirror), not just
the reason-based set.

Divergent histories on gitmsg/* branches (when two clones write between syncs)
are auto-merged — the empty-tree append-only shape of those branches makes the
merge conflict-free and preserves every commit hash on both sides. Code
branches (and --all branches) are never auto-merged; a diverged head (e.g.
after a rebase) fails with a hint to force-push explicitly.

Examples:
  gitsocial push              # Publish to the resolved remote (data + site)
  gitsocial push backup       # Publish to the remote named "backup"
  gitsocial push --dry-run    # Preview what would be pushed
  gitsocial push --no-code    # Skip code branches (default branch + PR heads)
  gitsocial push --no-site    # Skip the browser site
  gitsocial push --all        # Publish every local branch (wholesale mirror)`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)

			remote := ""
			if len(args) == 1 {
				remote = args[0]
			}

			opts := clientpush.Options{
				Remote:      remote,
				DryRun:      dryRun,
				NoCode:      noCode,
				NoSite:      noSite,
				AllBranches: allBranches,
			}

			if dryRun && !cfg.JSONOutput {
				fmt.Println("Dry run - no changes will be pushed")
			}

			// Immediate start line (resolution is local git config), then the
			// first-publish line: bare push to an empty remote bootstraps the whole
			// bucket. Detected before the push (one refs listing); printed for the
			// human, suppressed under --json.
			if !cfg.JSONOutput && !dryRun {
				resolved := clientpush.ResolveRemote(cfg.WorkDir, remote)
				fmt.Printf("Pushing to %s ...\n", resolved)
				if gitmsg.RemoteIsEmpty(cfg.WorkDir, resolved) {
					fmt.Printf("Publishing to empty remote %q ...\n", resolved)
				}
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
			result, err := clientpush.Publish(cfg.WorkDir, opts, onBranch, siteProgress)
			siteDone()
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result)
				return
			}

			printPushResult(result, dryRun)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without pushing")
	cmd.Flags().BoolVar(&noCode, "no-code", false, "Skip code branches (default branch and open-PR heads)")
	cmd.Flags().BoolVar(&noSite, "no-site", false, "Skip the browser static site (s3 remotes)")
	cmd.Flags().BoolVar(&allBranches, "all", false, "Publish every local branch (wholesale mirror), not just reasoned ones")

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
		fmt.Printf("  Branches (--all): %d\n", p.AllBranches)
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
