// fetch.go - CLI command for fetching updates from subscribed repositories
package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/client"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

// newFetchCmd creates the command for fetching updates from subscribed repositories.
func newFetchCmd() *cobra.Command {
	var listID string
	var parallel int
	var allBranches bool

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
				countBefore, err := notifications.GetUnreadCount(cfg.WorkDir)
				if err != nil {
					slog.Debug("get unread count", "error", err)
				}
				result := client.FetchRepository(cfg.CacheDir, repoURL, "", workspaceURL)
				if !result.Success {
					PrintError(cmd, result.Error.Message)
					os.Exit(ExitCode(result.Error.Code))
				}

				if cfg.JSONOutput {
					PrintJSON(result.Data)
				} else {
					fmt.Printf("✓ %s (%d posts)\n", repoURL, result.Data.Items)
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

			result, forkStats := runFullFetch(cfg, client.FetchOptions{
				ListID:   listID,
				Parallel: parallel,
			}, false, allBranches)
			if !cfg.JSONOutput && forkStats.Items > 0 {
				fmt.Printf("Fetched %d items from %d forks\n", forkStats.Items, forkStats.Repositories)
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
					fmt.Printf("\nFetched %d items from %d repositories\n", stats.Items, stats.Repositories)
				}

				if len(stats.Errors) > 0 {
					fmt.Printf("Failed: %d repositories\n", len(stats.Errors))
				}
				printNotificationDelta(cfg.WorkDir, countBefore)
			}
		},
	}

	cmd.Flags().StringVarP(&listID, "list", "l", "", "Fetch only repos from this list")
	cmd.Flags().IntVarP(&parallel, "parallel", "p", 4, "Number of concurrent fetches")
	cmd.Flags().BoolVar(&allBranches, "all-branches", false, "Track all upstream branches on the first fetch")

	return cmd
}

// resolveWorkspaceMode checks the saved workspace fetch mode and prompts on first use.
// A saved mode always wins. On first use, allBranches selects all-branch mode explicitly;
// assumeYes, JSON output, or a non-interactive stdin selects the default without prompting.
func resolveWorkspaceMode(workdir string, jsonOutput, assumeYes, allBranches bool) bool {
	originURL := protocol.NormalizeURL(git.GetOriginURL(workdir))
	if originURL == "" {
		return false
	}
	mode := settings.GetWorkspaceMode(originURL)
	if mode != "" {
		return mode == "*"
	}
	if allBranches {
		if err := settings.WriteWorkspaceMode(originURL, "*"); err != nil {
			slog.Warn("save workspace mode", "error", err)
		}
		return true
	}
	if jsonOutput || assumeYes || !isatty.IsTerminal(os.Stdin.Fd()) {
		if err := settings.WriteWorkspaceMode(originURL, "default"); err != nil {
			slog.Warn("save workspace mode", "error", err)
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
	if err := settings.WriteWorkspaceMode(originURL, mode); err != nil {
		slog.Warn("save workspace mode", "error", err)
	}
	fmt.Println()
	return mode == "*"
}

// runFullFetch fetches the subscribed repos, the registered forks and the workspace.
func runFullFetch(cfg *Config, opts client.FetchOptions, assumeYes, allBranches bool) (fetch.Result, fetch.Stats) {
	opts.FetchAllBranches = resolveWorkspaceMode(cfg.WorkDir, cfg.JSONOutput, assumeYes, allBranches)
	return client.Fetch(cfg.WorkDir, cfg.CacheDir, opts)
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
