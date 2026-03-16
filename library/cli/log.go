// log.go - CLI command for showing activity log with filters
package main

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/extensions/social"
)

// newLogCmd creates the command for showing activity log with filters.
func newLogCmd() *cobra.Command {
	var limit int
	var scope string
	var typeFilter string
	var after string
	var before string
	var author string

	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show activity log",
		Long: `Show activity log for the current repository or timeline.

Scopes:
  timeline              All activity from timeline
  repository:my         Current repository (default)

Types (comma-separated):
  post, comment, repost, quote, list-create, list-delete,
  repository-follow, repository-unfollow, config, metadata

Examples:
  gitsocial log
  gitsocial log --scope timeline
  gitsocial log --type post,comment
  gitsocial log --after 2024-01-01 --before 2024-06-01
  gitsocial log --author john@example.com`,
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			var types []social.LogEntryType
			if typeFilter != "" {
				for _, t := range strings.Split(typeFilter, ",") {
					types = append(types, social.LogEntryType(strings.TrimSpace(t)))
				}
			}

			var afterTime, beforeTime *time.Time
			if after != "" {
				t, err := time.Parse("2006-01-02", after)
				if err != nil {
					PrintError(cmd, "invalid --after date format (use YYYY-MM-DD)")
					os.Exit(ExitInvalidArgs)
				}
				afterTime = &t
			}
			if before != "" {
				t, err := time.Parse("2006-01-02", before)
				if err != nil {
					PrintError(cmd, "invalid --before date format (use YYYY-MM-DD)")
					os.Exit(ExitInvalidArgs)
				}
				beforeTime = &t
			}

			if scope == "" {
				scope = "repository:my"
			}

			cfg := GetConfig(cmd)
			result := social.GetLogs(cfg.WorkDir, scope, &social.GetLogsOptions{
				Limit:  limit,
				Types:  types,
				After:  afterTime,
				Before: beforeTime,
				Author: author,
			})
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitCode(result.Error.Code))
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				printWithPager(social.FormatLogs(result.Data))
			}
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum number of entries")
	cmd.Flags().StringVarP(&scope, "scope", "s", "", "Scope: timeline, repository:my (default)")
	cmd.Flags().StringVarP(&typeFilter, "type", "t", "", "Filter by types (comma-separated)")
	cmd.Flags().StringVar(&after, "after", "", "Show entries after date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&before, "before", "", "Show entries before date (YYYY-MM-DD)")
	cmd.Flags().StringVarP(&author, "author", "a", "", "Filter by author email")

	return cmd
}
