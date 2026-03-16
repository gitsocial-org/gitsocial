// explore.go - CLI command for browsing and discovering repositories
package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/extensions/social"
)

// newExploreCmd creates the command for browsing and discovering repositories.
func newExploreCmd() *cobra.Command {
	var listName string
	var limit int

	cmd := &cobra.Command{
		Use:   "explore",
		Short: "Browse and discover repositories",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			scope := "all"
			if listName != "" {
				scope = "list:" + listName
			}

			result := social.GetRepositories(cfg.WorkDir, scope, limit)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitCode(result.Error.Code))
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				printWithPager(social.FormatRepositories(result.Data))
			}
		},
	}

	cmd.Flags().StringVarP(&listName, "list", "l", "", "Filter by list name")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum number of repositories to show (0 for unlimited)")

	return cmd
}
