// gitremote.go - Hidden git remote-helper entry point for s3:// remotes
package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// newGitRemoteS3Cmd creates the hidden command git execs as `git-remote-s3`
// (via a shim). Args per gitremote-helpers(7): <remote-name> [<url>].
func newGitRemoteS3Cmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__git-remote-s3 <remote> [<url>]",
		Short:  "git remote helper for s3:// remotes (invoked by git, not directly)",
		Hidden: true,
		Args:   cobra.RangeArgs(1, 2),
		// No PersistentPreRunE side effects wanted here (cache open, logging
		// re-init) — but they are harmless and give the helper log config.
		RunE: func(cmd *cobra.Command, args []string) error {
			url := args[0]
			if len(args) == 2 {
				url = args[1]
			}
			return objstore.RunHelper(url, objstore.HelperEnvFromOS(), os.Stdin, os.Stdout)
		},
	}
}
