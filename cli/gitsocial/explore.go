// explore.go - CLI command for browsing and discovering repositories
package main

import (
	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

// newExploreCmd creates the command for browsing and discovering repositories.
func newExploreCmd() *cobra.Command {
	var listName string
	var limit int

	cmd := &cobra.Command{
		Use:   "explore",
		Short: "Browse and discover repositories",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			scope := "all"
			if listName != "" {
				scope = "list:" + listName
			}

			result := social.GetRepositories(cfg.WorkDir, scope, limit)
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitCode(result.Error.Code))
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				printWithPager(social.FormatRepositories(result.Data))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&listName, "list", "l", "", "Filter by list name")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum repositories to show, 0 for all")

	return cmd
}
