// main.go - The executable of GitSocial.app: posts one notification, or opens the URL of a clicked one.
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
	action, title, body, url := parseArgs(args)
	switch action {
	case "notify":
		return notify(title, body, url)
	case "click":
		return handleClick()
	}
	fmt.Fprintln(errOut, "usage: gitsocial-notifier [notify <title> <body> <url>]")
	return 2
}

// parseArgs returns "notify" with the text and the URL, "click" for no arguments, or "" for anything else.
func parseArgs(args []string) (action, title, body, url string) {
	switch {
	case len(args) == 0:
		return "click", "", "", ""
	case len(args) == 4 && args[0] == "notify":
		return "notify", args[1], args[2], args[3]
	}
	return "", "", "", ""
}
