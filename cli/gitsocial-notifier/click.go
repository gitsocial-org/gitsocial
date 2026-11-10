// click.go - The decision of a click on a notification: the TUI that sent it, or its URL.
package main

import (
	"fmt"
	"os"
)

// clickOpensURL sends the ref to the socket of the TUI that sent the notification and opens no URL; with no socket it opens a URL that is not empty.
func clickOpensURL(url, socket, ref string, send func(socket, ref string) error) bool {
	if socket == "" {
		return url != ""
	}
	if err := send(socket, ref); err != nil {
		fmt.Fprintln(os.Stderr, "gitsocial-notifier:", err)
	}
	return false
}
