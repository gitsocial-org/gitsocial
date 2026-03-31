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
	var state string
	var labels string
	var assignee string
	var reviewer string
	var milestone string
	var sprint string
	var draft bool
	var prerelease bool
	var tag string
	var base string
	var groupByField string
	var top int
	var countOnly bool

	cmd := &cobra.Command{
		Use:   "search [query]",
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

Extension-specific filters:
  --state            Filter by state (open, closed, merged, canceled)
  --labels           Filter by labels (comma-separated, any match)
  --assignee         Filter by assignee email (implies --type issue)
  --reviewer         Filter by reviewer email (implies --type pr)
  --milestone        Filter by milestone name (implies --type issue)
  --sprint           Filter by sprint name (implies --type issue)
  --draft            Filter draft PRs only (implies --type pr)
  --prerelease       Filter pre-releases only (implies --type release)
  --tag              Filter by release tag (implies --type release)
  --base             Filter by PR base branch (implies --type pr)

Scopes:
  timeline           Search across entire timeline (default)
  list:<name>        Search within a specific list
  repository:<url>   Search within a specific repository

Sort options:
  score              Sort by relevance score (default)
  date               Sort by date (newest first)

Grouping:
  --group-by <field>   Group results by: state, author, type, extension, repo, label, assignee, reviewer, milestone, base
  --top N              Show only top N items per group (default: all)
  --count-only         Show only group counts, no items

Examples:
  gitsocial search "hello world"
  gitsocial search "feature" --author john@example.com --type post
  gitsocial search "bug fix" --scope list:favorites --sort date
  gitsocial search --type pr --state open --json
  gitsocial search --type issue --state open --labels bug --assignee dev@example.com --json
  gitsocial search --type pr --after 2025-03-23 --group-by state --json
  gitsocial search --type pr --after 2025-03-23 --group-by author --top 5 --json
  gitsocial search --type issue --state open --group-by label --count-only --json`,
		Args: cobra.MaximumNArgs(1),
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

			if groupByField != "" && !search.IsValidGroupBy(groupByField) {
				PrintError(cmd, "invalid --group-by field (use: state, author, type, extension, repo, label, assignee, reviewer, milestone, base)")
				os.Exit(ExitInvalidArgs)
			}

			cfg := GetConfig(cmd)
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			result, err := search.Search(cfg.WorkDir, search.Params{
				Query:      query,
				Author:     author,
				Repo:       repo,
				Type:       typeFilter,
				Hash:       hash,
				After:      afterTime,
				Before:     beforeTime,
				Limit:      limit,
				Scope:      scope,
				Sort:       sortBy,
				State:      state,
				Labels:     labels,
				Assignee:   assignee,
				Reviewer:   reviewer,
				Milestone:  milestone,
				Sprint:     sprint,
				Draft:      draft,
				Prerelease: prerelease,
				Tag:        tag,
				Base:       base,
				GroupBy:    groupByField,
				Top:        top,
				CountOnly:  countOnly,
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
	cmd.Flags().StringVar(&state, "state", "", "Filter by state (open, closed, merged, canceled)")
	cmd.Flags().StringVar(&labels, "labels", "", "Filter by labels (comma-separated, any match)")
	cmd.Flags().StringVar(&assignee, "assignee", "", "Filter by assignee email")
	cmd.Flags().StringVar(&reviewer, "reviewer", "", "Filter by reviewer email")
	cmd.Flags().StringVar(&milestone, "milestone", "", "Filter by milestone name")
	cmd.Flags().StringVar(&sprint, "sprint", "", "Filter by sprint name")
	cmd.Flags().BoolVar(&draft, "draft", false, "Filter draft PRs only")
	cmd.Flags().BoolVar(&prerelease, "prerelease", false, "Filter pre-releases only")
	cmd.Flags().StringVar(&tag, "tag", "", "Filter by release tag")
	cmd.Flags().StringVar(&base, "base", "", "Filter by PR base branch")
	cmd.Flags().StringVar(&groupByField, "group-by", "", "Group results by field (state, author, type, extension, repo, label, assignee, reviewer, milestone, base)")
	cmd.Flags().IntVar(&top, "top", 0, "Max items per group (default: unlimited)")
	cmd.Flags().BoolVar(&countOnly, "count-only", false, "Show only group counts, no items")

	return cmd
}
