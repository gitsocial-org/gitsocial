// search.go - CLI command for searching with filters
package main

import (
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/core/search"
)

// newSearchCmd creates the command for searching posts with filters.
func newSearchCmd() *cobra.Command {
	var limit int
	var author string
	var repo string
	var typeFilter string
	var hash string
	var after string
	var before string
	var scope string
	var sortBy string

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search across all extensions",
		Long: `Search across posts, issues, PRs, releases, and more.

Filters can be specified as flags or inline in the query (flags take precedence).

Inline filters (for backward compatibility):
  author:<email>     Filter by author email
  repo:<url>         Filter by repository URL
  type:<type>        Filter by post type
  hash:<prefix>      Filter by commit hash prefix
  after:YYYY-MM-DD   Posts after date
  before:YYYY-MM-DD  Posts before date

Scopes:
  timeline           Search across entire timeline (default)
  list:<name>        Search within a specific list
  repository:<url>   Search within a specific repository

Sort options:
  score              Sort by relevance score (default)
  date               Sort by date (newest first)

Examples:
  gitsocial search "hello world"
  gitsocial search "feature" --author john@example.com --type post
  gitsocial search "bug fix" --scope list:favorites --sort date
  gitsocial search "author:john feature"`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
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

			if sortBy != "" && sortBy != "score" && sortBy != "date" {
				PrintError(cmd, "invalid --sort option (use 'score' or 'date')")
				os.Exit(ExitInvalidArgs)
			}

			cfg := GetConfig(cmd)
			result, err := search.Search(cfg.WorkDir, search.Params{
				Query:  args[0],
				Author: author,
				Repo:   repo,
				Type:   typeFilter,
				Hash:   hash,
				After:  afterTime,
				Before: beforeTime,
				Limit:  limit,
				Scope:  scope,
				Sort:   sortBy,
			})
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result)
			} else {
				printWithPager(search.FormatResult(result))
			}
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum number of results")
	cmd.Flags().StringVarP(&author, "author", "a", "", "Filter by author email")
	cmd.Flags().StringVarP(&repo, "repo", "r", "", "Filter by repository URL")
	cmd.Flags().StringVarP(&typeFilter, "type", "t", "", "Filter by type (post|comment|repost|quote|pr|issue|milestone|sprint|release)")
	cmd.Flags().StringVar(&hash, "hash", "", "Filter by commit hash prefix")
	cmd.Flags().StringVar(&after, "after", "", "Posts after date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&before, "before", "", "Posts before date (YYYY-MM-DD)")
	cmd.Flags().StringVarP(&scope, "scope", "s", "", "Search scope: timeline (default), list:<name>, repository:<url>")
	cmd.Flags().StringVar(&sortBy, "sort", "", "Sort by: score (default) or date")

	return cmd
}
