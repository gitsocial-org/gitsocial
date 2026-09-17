// pager.go - Output paging utilities for long content
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// getPager returns the pager command from environment or defaults to less.
func getPager() string {
	if p := os.Getenv("GM_PAGER"); p != "" {
		return p
	}
	if p := os.Getenv("PAGER"); p != "" {
		return p
	}
	return "less"
}

// getTerminalHeight returns the terminal height or 24 as default.
func getTerminalHeight() int {
	w, h, err := term.GetSize(os.Stdout.Fd())
	if err != nil || w == 0 || h == 0 {
		return 24
	}
	return h
}

// countLines counts the number of lines in a string.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// printWithPager prints output through a pager if content exceeds terminal height.
func printWithPager(cmd *cobra.Command, output string) {
	out := cmd.OutOrStdout()
	// Paging needs the command's output to be the process terminal.
	if out != io.Writer(os.Stdout) || !isatty.IsTerminal(os.Stdout.Fd()) {
		fmt.Fprintln(out, output)
		return
	}

	lines := countLines(output)
	height := getTerminalHeight()

	if lines <= height-2 {
		fmt.Fprintln(out, output)
		return
	}

	pager := getPager()
	if pager == "" {
		fmt.Fprintln(out, output)
		return
	}

	// The pager draws on the terminal, so it inherits the process streams.
	pagerCmd := exec.Command(pager)
	pagerCmd.Stdout = os.Stdout
	pagerCmd.Stderr = os.Stderr

	stdin, err := pagerCmd.StdinPipe()
	if err != nil {
		fmt.Fprintln(out, output)
		return
	}

	if err := pagerCmd.Start(); err != nil {
		fmt.Fprintln(out, output)
		return
	}

	_, _ = io.WriteString(stdin, output+"\n")
	_ = stdin.Close()

	_ = pagerCmd.Wait()
}
