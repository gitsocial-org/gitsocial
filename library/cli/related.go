// related.go - CLI command for finding related repositories
package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/extensions/social"
)

// newRelatedCmd creates the command for finding related repositories.
func newRelatedCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "related <repository>",
		Short: "Find repositories related to a given repository",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			repoURL := normalizeRepoURL(args[0])
			result := social.GetRelatedRepositories(cfg.WorkDir, repoURL)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitCode(result.Error.Code))
			}

			repos := result.Data
			if limit > 0 && len(repos) > limit {
				repos = repos[:limit]
			}

			if cfg.JSONOutput {
				PrintJSON(repos)
			} else {
				printWithPager(social.FormatRelatedRepositories(repos))
			}
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "l", 0, "Limit number of results")

	return cmd
}

// normalizeRepoURL converts shorthand repository references to full URLs.
func normalizeRepoURL(input string) string {
	if strings.HasPrefix(input, "https://") || strings.HasPrefix(input, "git@") {
		return input
	}
	if strings.Contains(input, "/") && !strings.Contains(input, "://") {
		return "https://github.com/" + input
	}
	return input
}
