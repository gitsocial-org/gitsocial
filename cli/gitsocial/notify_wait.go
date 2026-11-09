// notify_wait.go - Hidden command that shows one desktop message on Linux and opens its URL on a click
package main

import (
	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/notifications/desktop"
)

// newNotifyWaitCmd creates the hidden command that the Linux sender starts in a detached process.
func newNotifyWaitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "notify-wait -- <title> <body> <url>",
		Short: "Show a desktop message and open its URL on a click",
		Long: `Show one desktop message with notify-send, wait for its click or its
close, and open the URL on a click. The desktop sender of a fetch starts
it in its own session; it is not a command to run by hand.`,
		Hidden: true,
		Args:   cobra.ExactArgs(3),
		// No cache and no workspace: the wait can last an hour and must not hold the cache open.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := desktop.NotifyWait(args[0], args[1], args[2]); err != nil {
				PrintError(cmd, err.Error())
				return exit(ExitError)
			}
			return nil
		},
	}
}
