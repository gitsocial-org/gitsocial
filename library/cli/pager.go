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
func printWithPager(output string) {
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		fmt.Println(output)
		return
	}

	lines := countLines(output)
	height := getTerminalHeight()

	if lines <= height-2 {
		fmt.Println(output)
		return
	}

	pager := getPager()
	if pager == "" {
		fmt.Println(output)
		return
	}

	cmd := exec.Command(pager)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Println(output)
		return
	}

	if err := cmd.Start(); err != nil {
		fmt.Println(output)
		return
	}

	_, _ = io.WriteString(stdin, output+"\n")
	_ = stdin.Close()

	_ = cmd.Wait()
}
