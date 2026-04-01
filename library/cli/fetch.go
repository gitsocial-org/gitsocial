// fetch.go - CLI command for fetching updates from subscribed repositories
package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/core/fetch"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/notifications"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/settings"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/extensions/social"
)

// newFetchCmd creates the command for fetching updates from subscribed repositories.
func newFetchCmd() *cobra.Command {
	var listID string
	var since string
	var before string
	var parallel int

	cmd := &cobra.Command{
		Use:   "fetch [url]",
		Short: "Fetch updates from all extensions",
		Long: `Fetch updates from all extensions.

This is a convenience wrapper that calls each extension's fetch command.
Currently fetches: social, pm

Examples:
  gitsocial fetch                     # Fetch all subscribed repos
  gitsocial fetch --list reading      # Fetch only repos in 'reading' list
  gitsocial fetch https://github.com/user/repo  # Fetch specific repo

For extension-specific options, use the extension's fetch command directly:
  gitsocial social fetch --list reading`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)

			if len(args) == 1 {
				repoURL := args[0]
				workspaceURL := gitmsg.ResolveRepoURL(cfg.WorkDir)
				extraProcessors := append(pm.Processors(), review.Processors()...)
				extraProcessors = append(extraProcessors, notifications.MentionProcessor(), notifications.TrailerProcessor())
				countBefore, err := notifications.GetUnreadCount(cfg.WorkDir)
				if err != nil {
					slog.Debug("get unread count", "error", err)
				}
				result := social.FetchRepository(cfg.CacheDir, repoURL, "", workspaceURL, extraProcessors...)
				if !result.Success {
					PrintError(cmd, result.Error.Message)
					os.Exit(ExitCode(result.Error.Code))
				}

				if cfg.JSONOutput {
					PrintJSON(result.Data)
				} else {
					fmt.Printf("✓ %s (%d posts)\n", repoURL, result.Data.Posts)
					printNotificationDelta(cfg.WorkDir, countBefore)
				}
				return
			}

			countBefore, err := notifications.GetUnreadCount(cfg.WorkDir)
			if err != nil {
				slog.Debug("get unread count", "error", err)
			}
			if !cfg.JSONOutput {
				if listID != "" {
					fmt.Printf("Fetching repositories from list '%s'...\n", listID)
				} else {
					fmt.Println("Fetching all subscribed repositories...")
				}
			}

			result, forkStats := runFullFetch(cfg, &social.FetchOptions{
				ListID:   listID,
				Since:    since,
				Before:   before,
				Parallel: parallel,
			})
			if !cfg.JSONOutput && forkStats.Items > 0 {
				fmt.Printf("Fetched %d items from %d forks\n", forkStats.Items, forkStats.Forks)
			}
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitCode(result.Error.Code))
			}

			stats := result.Data

			if cfg.JSONOutput {
				PrintJSON(stats)
			} else {
				for _, e := range stats.Errors {
					fmt.Printf("  ✗ %s (%s)\n", e.Repository, e.Error)
				}

				if stats.Repositories > 0 || len(stats.Errors) == 0 {
					fmt.Printf("\nFetched %d posts from %d repositories\n", stats.Posts, stats.Repositories)
				}

				if len(stats.Errors) > 0 {
					fmt.Printf("Failed: %d repositories\n", len(stats.Errors))
				}
				printNotificationDelta(cfg.WorkDir, countBefore)
			}
		},
	}

	cmd.Flags().StringVarP(&listID, "list", "l", "", "Fetch only repos from this list")
	cmd.Flags().StringVar(&since, "since", "", "Fetch posts since date (YYYY-MM-DD, default: 30 days ago)")
	cmd.Flags().StringVar(&before, "before", "", "Fetch posts before date (YYYY-MM-DD, default: today)")
	cmd.Flags().IntVarP(&parallel, "parallel", "p", 4, "Number of concurrent fetches")

	return cmd
}

// resolveWorkspaceMode checks the saved workspace fetch mode and prompts on first use.
func resolveWorkspaceMode(workdir string, jsonOutput bool) bool {
	originURL := protocol.NormalizeURL(git.GetOriginURL(workdir))
	if originURL == "" {
		return false
	}
	settingsPath, err := settings.DefaultPath()
	if err != nil {
		return false
	}
	s, err := settings.Load(settingsPath)
	if err != nil {
		return false
	}
	mode := settings.GetWorkspaceMode(s, originURL)
	if mode != "" {
		return mode == "*"
	}
	if jsonOutput {
		settings.SetWorkspaceMode(s, originURL, "default")
		if err := settings.Save(settingsPath, s); err != nil {
			slog.Warn("save settings", "error", err)
		}
		return false
	}
	branches, _ := git.ListRemoteBranches(workdir, "origin")
	branchCount := len(branches)
	fmt.Println("\nWorkspace fetch mode (first time setup):")
	fmt.Println("  [1] Default branch + gitmsg refs only")
	if branchCount > 0 {
		fmt.Printf("  [2] All upstream branches (%d branches)\n", branchCount)
	} else {
		fmt.Println("  [2] All upstream branches")
	}
	fmt.Print("\nChoice [1]: ")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "2" {
		mode = "*"
	} else {
		mode = "default"
	}
	settings.SetWorkspaceMode(s, originURL, mode)
	if err := settings.Save(settingsPath, s); err != nil {
		slog.Warn("save settings", "error", err)
	}
	fmt.Println()
	return mode == "*"
}

// runFullFetch performs a full workspace fetch: subscribed repos, forks, and workspace sync.
// If opts is nil, defaults are used. The caller can pre-populate opts with CLI-specific fields
// (ListID, Since, Before, Parallel); this function fills in processors and branch mode.
func runFullFetch(cfg *Config, opts *social.FetchOptions) (social.Result[social.FetchStats], fetch.FetchForkStats) {
	if opts == nil {
		opts = &social.FetchOptions{}
	}
	opts.FetchAllBranches = resolveWorkspaceMode(cfg.WorkDir, cfg.JSONOutput)
	extraProcessors := append(pm.Processors(), review.Processors()...)
	extraProcessors = append(extraProcessors, notifications.MentionProcessor(), notifications.TrailerProcessor())
	opts.ExtraProcessors = extraProcessors
	result := social.Fetch(cfg.WorkDir, cfg.CacheDir, opts)
	forkProcessors := append(review.Processors(), pm.Processors()...)
	forkProcessors = append(forkProcessors, notifications.MentionProcessor(), notifications.TrailerProcessor())
	forkStats := fetch.FetchForks(cfg.WorkDir, cfg.CacheDir, forkProcessors)
	if err := fetch.SyncWorkspace(cfg.WorkDir); err != nil {
		slog.Debug("workspace sync", "error", err)
	}
	return result, forkStats
}

// printNotificationDelta prints new notification count if it increased after fetch.
func printNotificationDelta(workdir string, countBefore int) {
	countAfter, _ := notifications.GetUnreadCount(workdir)
	delta := countAfter - countBefore
	if delta > 0 {
		fmt.Printf("You have %d new notification", delta)
		if delta != 1 {
			fmt.Print("s")
		}
		fmt.Println()
	} else if countAfter > 0 {
		fmt.Printf("You have %d unread notification", countAfter)
		if countAfter != 1 {
			fmt.Print("s")
		}
		fmt.Println()
	}
}
