//go:build !(darwin && cgo)

// notifier_other.go - The notifier of a build with no cgo or for another OS: it posts nothing and fails, so the sender falls back.
package main

import (
	"fmt"
	"os"
)

// notify reports on stderr that this build posts nothing and fails.
func notify(_, _, _, _, _ string) int {
	fmt.Fprintln(os.Stderr, "gitsocial-notifier: this build has no notification support")
	return 1
}

// handleClick reports on stderr that this build posts nothing and fails.
func handleClick() int {
	fmt.Fprintln(os.Stderr, "gitsocial-notifier: this build has no notification support")
	return 1
}
