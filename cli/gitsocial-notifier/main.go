// main.go - The executable of GitSocial.app: posts one notification, or handles the click of one.
package main

import (
	"fmt"
	"io"
	"os"
)

// main runs the action of the arguments and exits with its code.
func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

// run posts for notify, handles the launch of a click for no arguments, and prints the usage for anything else.
func run(args []string, errOut io.Writer) int {
	action, title, body, url, socket, ref := parseArgs(args)
	switch action {
	case "notify":
		return notify(title, body, url, socket, ref)
	case "click":
		return handleClick()
	}
	fmt.Fprintln(errOut, "usage: gitsocial-notifier [notify <title> <body> <url> [<socket> <ref>]]")
	return 2
}

// parseArgs returns "notify" with the text, the URL, and the socket and the ref of a TUI, "click" for no arguments, or "" for anything else.
func parseArgs(args []string) (action, title, body, url, socket, ref string) {
	switch {
	case len(args) == 0:
		return "click", "", "", "", "", ""
	case len(args) == 4 && args[0] == "notify":
		return "notify", args[1], args[2], args[3], "", ""
	case len(args) == 6 && args[0] == "notify" && args[4] != "":
		return "notify", args[1], args[2], args[3], args[4], args[5]
	}
	return "", "", "", "", "", ""
}
