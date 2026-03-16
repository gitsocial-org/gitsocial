// main.go - CLI entry point, command registration, and TUI launcher
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/tui"
)

var version = "dev"

// main is the CLI entry point that registers commands and executes the root command.
func main() {
	rootCmd := newRootCmd()

	// Core commands
	rootCmd.AddCommand(
		newStatusCmd(),
		newFetchCmd(),
		newPushCmd(),
		NewExtConfigCmd(coreExt),
		newSettingsCmd(),
		newLogCmd(),
		newSearchCmd(),
		newRelatedCmd(),
		newExploreCmd(),
		newHistoryCmd(),
		newNotificationsCmd(),
		newTUICmd(),
		newDocsCmd(),
		newRPCCmd(),
		newImportCmd(),
	)

	// Extension commands (auto-registered via init())
	RegisterAllExtensions(rootCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(ExitError)
	}
}

// newTUICmd creates the command for launching the interactive TUI.
func newTUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch interactive TUI for browsing posts",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := tui.Run(cfg.WorkDir, cfg.CacheDir); err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
		},
	}

	return cmd
}
