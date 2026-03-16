// history.go - CLI command for viewing edit history of a message
package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/extensions/social"
)

// newHistoryCmd creates the command for viewing edit history of a message.
func newHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history <ref>",
		Short: "View edit history of a message",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			ref := args[0]

			if err := social.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "social", "error", err)
			}
			if err := pm.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "pm", "error", err)
			}

			workspaceURL := gitmsg.ResolveRepoURL(cfg.WorkDir)
			versions, err := gitmsg.GetHistory(ref, workspaceURL)
			if err != nil {
				PrintError(cmd, "Failed to get history: "+err.Error())
				os.Exit(ExitError)
			}

			if len(versions) == 0 {
				PrintError(cmd, "No history found")
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(versions)
			} else {
				printWithPager(gitmsg.FormatHistory(versions))
			}
		},
	}

	return cmd
}
