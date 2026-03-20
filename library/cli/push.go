// push.go - CLI command for pushing local changes to remote
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/core/gitmsg"
)

// newPushCmd creates the command for pushing local changes to remote.
func newPushCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push local changes to remote",
		Long: `Push all local GitMsg changes to the remote repository.

This pushes:
  - Branch commits (posts, comments, reposts, quotes)
  - GitMsg refs (lists, configs)

Examples:
  gitsocial push              # Push all changes
  gitsocial push --dry-run    # Preview what would be pushed`,
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)

			if dryRun && !cfg.JSONOutput {
				fmt.Println("Dry run - no changes will be pushed")
			}

			// Check for diverged branches and prompt for rebase
			if !dryRun && gitmsg.HasDivergedBranches(cfg.WorkDir) {
				fmt.Print("Some branches have diverged from remote. Rebase local commits? [y/n] ")
				reader := bufio.NewReader(os.Stdin)
				answer, _ := reader.ReadString('\n')
				if strings.TrimSpace(strings.ToLower(answer)) != "y" {
					fmt.Println("Push canceled.")
					return
				}
				if err := gitmsg.RebaseDivergedBranches(cfg.WorkDir, gitmsg.GetExtBranches(cfg.WorkDir)); err != nil {
					PrintError(cmd, fmt.Sprintf("Rebase failed: %s", err))
					os.Exit(ExitError)
				}
				fmt.Println("Rebased successfully.")
			}

			result, err := gitmsg.Push(cfg.WorkDir, dryRun)
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result)
				return
			}

			if result.Commits == 0 && result.Refs == 0 {
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
			if result.Refs > 0 {
				fmt.Printf("  Refs: %d\n", result.Refs)
			}
			if !dryRun {
				fmt.Println("Done.")
			}
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without pushing")

	return cmd
}
