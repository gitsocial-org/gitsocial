// history.go - CLI command for viewing edit history of a message
package main

import (
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/client"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
)

// newHistoryCmd creates the command for viewing edit history of a message.
func newHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history <ref>",
		Short: "View edit history of a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			ref := args[0]

			if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "error", err)
			}

			workspaceURL := gitmsg.ResolveRepoURL(cfg.WorkDir)
			versions, err := gitmsg.GetHistory(ref, workspaceURL)
			if err != nil {
				PrintError(cmd, "read history: "+err.Error())
				return exit(ExitError)
			}

			if len(versions) == 0 {
				PrintError(cmd, "no edit history for "+ref)
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, versions)
			} else {
				printWithPager(cmd, gitmsg.FormatHistory(versions))
			}
			return nil
		},
	}

	return cmd
}
