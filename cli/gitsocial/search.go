// search.go - CLI command for searching with filters
package main

import (
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/client"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/search"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
	"github.com/gitsocial-org/gitsocial/library/extensions/memo"
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
	var tier string

	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search across all extensions",
		Long: `Search posts, issues, pull requests, releases and memos.

A filter is a flag or an inline term, and the flag wins when both are
given.

Inline filters:
  author:<email>     filter by author email
  repo:<url>         filter by repository URL
  type:<type>        filter by item type
  hash:<prefix>      filter by commit hash prefix
  after:YYYY-MM-DD   items after the date
  before:YYYY-MM-DD  items before the date

Types: post, comment, repost, quote, pr, issue, milestone, sprint,
release, memo.

Type filters:
  --state       open, closed, merged, canceled
  --labels      comma-separated, any match
  --assignee    implies --type issue
  --milestone   implies --type issue
  --sprint      implies --type issue
  --reviewer    implies --type pr
  --draft       implies --type pr
  --base        implies --type pr
  --prerelease  implies --type release
  --tag         implies --type release
  --tier        implies --type memo: session, personal, project,
                inherited, external

Scopes: timeline, the default, list:<name>, repository:<url>.
Sort: score, the default, or date.
Group by: state, author, type, extension, repo, label, assignee,
reviewer, milestone, base. --top caps the items per group and
--count-only prints the counts alone.

Examples:
  gitsocial search "hello world"
  gitsocial search "feature" --author dev@example.com --type post
  gitsocial search --type pr --state open --json
  gitsocial search --type issue --labels bug --assignee dev@example.com
  gitsocial search --type pr --group-by author --top 5`,
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

			// Tier scoping is memo-specific; resolve to a `repos:<csv>` scope
			// so core/search stays generic. Only meaningful for --type memo.
			if tier != "" {
				if !strings.EqualFold(typeFilter, "memo") {
					PrintError(cmd, "--tier is only valid with --type memo")
					os.Exit(ExitInvalidArgs)
				}
				if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
					slog.Debug("sync workspace", "error", err)
				}
				if err := memo.SyncAllTierReposToCache(cfg.WorkDir); err != nil {
					slog.Debug("memo sync", "error", err)
				}
				urls := tierRepoURLs(memo.Tier(tier), cfg.WorkDir)
				if len(urls) == 0 {
					PrintError(cmd, "no repos for tier "+tier)
					os.Exit(ExitError)
				}
				scope = "repos:" + strings.Join(urls, ",")
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
	cmd.Flags().StringVarP(&typeFilter, "type", "t", "", "Filter by item type")
	cmd.Flags().StringVar(&hash, "hash", "", "Filter by commit hash prefix")
	cmd.Flags().StringVar(&after, "after", "", "Items after this date, YYYY-MM-DD")
	cmd.Flags().StringVar(&before, "before", "", "Items before this date, YYYY-MM-DD")
	cmd.Flags().StringVarP(&scope, "scope", "s", "", "Search scope: timeline, list:<name>, repository:<url>")
	cmd.Flags().StringVar(&sortBy, "sort", "", "Sort by score or date")
	cmd.Flags().StringVar(&state, "state", "", "Filter by state: open, closed, merged, canceled")
	cmd.Flags().StringVar(&labels, "labels", "", "Filter by comma-separated labels, any match")
	cmd.Flags().StringVar(&assignee, "assignee", "", "Filter by assignee email")
	cmd.Flags().StringVar(&reviewer, "reviewer", "", "Filter by reviewer email")
	cmd.Flags().StringVar(&milestone, "milestone", "", "Filter by milestone name")
	cmd.Flags().StringVar(&sprint, "sprint", "", "Filter by sprint name")
	cmd.Flags().BoolVar(&draft, "draft", false, "Filter draft PRs only")
	cmd.Flags().BoolVar(&prerelease, "prerelease", false, "Filter pre-releases only")
	cmd.Flags().StringVar(&tag, "tag", "", "Filter by release tag")
	cmd.Flags().StringVar(&base, "base", "", "Filter by PR base branch")
	cmd.Flags().StringVar(&groupByField, "group-by", "", "Group results by one field")
	cmd.Flags().IntVar(&top, "top", 0, "Max items per group")
	cmd.Flags().BoolVar(&countOnly, "count-only", false, "Show only group counts, no items")
	cmd.Flags().StringVar(&tier, "tier", "", "Memo tier: session, personal, project, inherited, external")

	return cmd
}

// tierRepoURLs resolves a memo tier to the list of repo URLs that hold that
// tier's memos, used to express `--tier` via the core search `repos:` scope.
func tierRepoURLs(t memo.Tier, workdir string) []string {
	personalURL := ""
	if path, err := settings.PersonalRepoPath(); err == nil {
		personalURL = memo.LocalRepoURL(path)
	}
	switch t {
	case memo.TierProject:
		if u := memoWorkspaceURL(workdir); u != "" {
			return []string{u}
		}
	case memo.TierPersonal:
		if personalURL != "" {
			return []string{personalURL}
		}
	case memo.TierSession:
		ws := memoWorkspaceURL(workdir)
		all := memo.AllTierRepoURLs(ws)
		inherited := memo.ListInherits(workdir)
		var out []string
		for _, u := range all {
			if memo.TierForRepoURL(u, ws, inherited) == memo.TierSession {
				out = append(out, u)
			}
		}
		return out
	case memo.TierInherited:
		return memo.ListInherits(workdir)
	case memo.TierExternal:
		// External tier not enumerable from search; the caller should rely on
		// the default merged view minus the local tier URLs.
	}
	return nil
}

func memoWorkspaceURL(workdir string) string {
	return gitmsg.ResolveRepoURL(workdir)
}
