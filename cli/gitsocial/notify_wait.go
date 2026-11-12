// notify_wait.go - Hidden command that shows one desktop message on Linux and gives its click to the TUI or opens its URL
package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/notifications/desktop"
)

// newNotifyWaitCmd creates the hidden command that the Linux sender starts in a detached process.
func newNotifyWaitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "notify-wait -- <title> <body> <url> [<socket> <ref>]",
		Short: "Show a desktop message and wait for its click",
		Long: `Show one desktop message through the session D-Bus and wait for its
click or its close. On a click, send the ref to the socket of the TUI
when the message names a socket, else open the URL. In Ghostty, GNOME
Terminal, Ptyxis and Konsole on KDE Plasma, a click that the TUI gets
also brings the terminal to the front. With no usable session bus, show
the message with notify-send and no click. The desktop sender of a fetch
starts it in its own session; it is not a command to run by hand.`,
		Hidden: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 3 && len(args) != 5 {
				return fmt.Errorf("notify-wait takes 3 or 5 arguments, got %d", len(args))
			}
			return nil
		},
		// No cache and no workspace: the wait can last an hour and must not hold the cache open.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			socket, ref := "", ""
			if len(args) == 5 {
				socket, ref = args[3], args[4]
			}
			if err := desktop.NotifyWait(args[0], args[1], args[2], socket, ref); err != nil {
				PrintError(cmd, err.Error())
				return exit(ExitError)
			}
			return nil
		},
	}
}
