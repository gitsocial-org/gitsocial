// push.go - CLI command for pushing local changes to remote
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
)

// newPushCmd creates the command for pushing local changes to remote.
func newPushCmd() *cobra.Command {
	var dryRun bool
	var noCode bool

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push local changes to remote",
		Long: `Push all local GitMsg changes to the remote repository.

Pushes to origin, or to the configured s3 remote when one exists.

This pushes:
  - Branch commits (posts, comments, reposts, quotes)
  - GitMsg refs (lists, configs)
  - Code branches: the default branch when it's ahead of the remote, and heads
    of open pull requests, so others can fetch the code your published data
    points at (--no-code skips)

Divergent histories on gitmsg/* branches (when two clones write between syncs)
are auto-merged — the empty-tree append-only shape of those branches makes the
merge conflict-free and preserves every commit hash on both sides. Code
branches are never auto-merged; a diverged PR head (e.g. after a rebase) fails
with a hint to force-push explicitly.

Examples:
  gitsocial push              # Push all changes
  gitsocial push --dry-run    # Preview what would be pushed
  gitsocial push --no-code    # Skip code branches (default branch + PR heads)`,
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)

			if dryRun && !cfg.JSONOutput {
				fmt.Println("Dry run - no changes will be pushed")
			}

			var codeBranches map[string]int
			if !noCode {
				codeBranches, _ = review.CodeBranchesToPush(cfg.WorkDir)
			}
			result, err := gitmsg.Push(cfg.WorkDir, dryRun, codeBranches)
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result)
				return
			}

			if result.Commits == 0 && result.CodeCommits == 0 && result.Refs == 0 {
				fmt.Println("Nothing to push")
				return
			}

			if dryRun {
				fmt.Println("Would push:")
			} else {
				fmt.Println("Pushed:")
			}
			if result.Commits > 0 {
				fmt.Printf("  Commits: %d\n", result.Commits)
			}
			if result.CodeCommits > 0 {
				fmt.Printf("  Code commits: %d\n", result.CodeCommits)
			}
			if result.Refs > 0 {
				fmt.Printf("  Refs: %d\n", result.Refs)
			}
			if !dryRun {
				fmt.Println("Done.")
			}
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without pushing")
	cmd.Flags().BoolVar(&noCode, "no-code", false, "Skip code branches (default branch and open-PR heads)")

	return cmd
}
