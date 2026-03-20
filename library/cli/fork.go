// fork.go - Top-level fork management commands
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/core/gitmsg"
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
		Args:  cobra.ExactArgs(1),
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
				PrintJSON(map[string]string{"added": args[0]})
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
				PrintJSON(forks)
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
