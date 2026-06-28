// fork.go - Top-level fork management commands
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
)

// newForkCmd creates the top-level fork command for managing registered forks.
func newForkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fork",
		Short: "Manage registered forks",
	}
	cmd.AddCommand(
		newForkAddCmd(),
		newForkRemoveCmd(),
		newForkListCmd(),
	)
	return cmd
}

func newForkAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <url>",
		Short: "Register a fork for PR and issue discovery",
		Long: `Register a fork so its PRs, issues, and comments surface alongside yours.

Use a fork when another repo is a true fork of yours (or yours of theirs) and
you want to collaborate on shared items: PRs against your code, comments on
your issues, and cross-repo edits. A fork's edits to your items are proposals
you accept (or not) from the item's history view.

Forks vs. lists: for a soft fork or packaging fork (you keep your own issues
and just follow an upstream for awareness), use a list instead:
  gitsocial social list add <list> <upstream-url>
A list follows a repo without entangling its items with your own.`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			if err := gitmsg.AddFork(cfg.WorkDir, args[0]); err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(map[string]interface{}{"added": args[0]})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("Fork added: %s", args[0]))
			}
		},
	}
}

func newForkRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <url>",
		Short: "Remove a registered fork",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			if err := gitmsg.RemoveFork(cfg.WorkDir, args[0]); err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(map[string]string{"removed": args[0]})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("Fork removed: %s", args[0]))
			}
		},
	}
}

func newForkListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered forks",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			forks := gitmsg.GetForks(cfg.WorkDir)
			if cfg.JSONOutput {
				out := make([]map[string]interface{}, 0, len(forks))
				for _, f := range forks {
					out = append(out, map[string]interface{}{"url": f})
				}
				PrintJSON(out)
			} else {
				if len(forks) == 0 {
					fmt.Println("No forks registered")
					return
				}
				for _, f := range forks {
					fmt.Println(f)
				}
			}
		},
	}
}
