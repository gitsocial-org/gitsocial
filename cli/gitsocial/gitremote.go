// gitremote.go - Hidden git remote-helper entry point for s3:// remotes
package main

import (
	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"

	"github.com/gitsocial-org/gitsocial/library/core/site"
)

// newGitRemoteS3Cmd creates the hidden command git execs as `git-remote-s3`
// (via a shim). Args per gitremote-helpers(7): <remote-name> [<url>].
func newGitRemoteS3Cmd() *cobra.Command {
	return &cobra.Command{
		Use:   "__git-remote-s3 <remote> [<url>]",
		Short: "git remote helper for s3:// remotes",
		Long: `The remote helper git execs for an s3:// remote, per
gitremote-helpers(7). git invokes it through the helper alias; it is not
a command to run by hand.`,
		Hidden: true,
		Args:   cobra.RangeArgs(1, 2),
		// No PersistentPreRunE side effects wanted here (cache open, logging
		// re-init) — but they are harmless and give the helper log config.
		RunE: func(cmd *cobra.Command, args []string) error {
			// Two args: <remote-name> <url> (a configured remote, whose per-remote
			// site overrides the helper reads). One arg: an anonymous URL, no name
			// and so no overrides.
			name, url := "", args[0]
			if len(args) == 2 {
				name, url = args[0], args[1]
			}
			// The hook is the only path from a plain git push to the site.
			return objstore.RunHelper(name, url, objstore.HelperEnvFromOS(), cmd.InOrStdin(), cmd.OutOrStdout(), site.PostPushMaintenance)
		},
	}
}
