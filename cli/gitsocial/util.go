// util.go - CLI utilities for config, output formatting, and exit codes
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

const (
	ExitSuccess     = 0
	ExitError       = 1
	ExitInvalidArgs = 2
	ExitPermission  = 3
	ExitNetwork     = 4
	ExitNotRepo     = 5
)

type Config struct {
	WorkDir    string
	CacheDir   string
	JSONOutput bool
}

type ctxKey struct{}

// WithConfig stores config in context for command handlers.
func WithConfig(ctx context.Context, cfg *Config) context.Context {
	return context.WithValue(ctx, ctxKey{}, cfg)
}

// GetConfig retrieves config from the command context.
func GetConfig(cmd *cobra.Command) *Config {
	if cmd.Context() == nil {
		return nil
	}
	cfg, ok := cmd.Context().Value(ctxKey{}).(*Config)
	if !ok {
		return nil
	}
	return cfg
}

// EnsureGitRepo verifies the working directory is a git repository.
func EnsureGitRepo(cmd *cobra.Command) bool {
	cfg := GetConfig(cmd)
	if cfg == nil || !git.IsRepository(cfg.WorkDir) {
		PrintError(cmd, "not a git repository")
		return false
	}
	return true
}

// PrintJSON outputs the value as formatted JSON to stdout.
func PrintJSON(v interface{}) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: failed to marshal JSON")
		os.Exit(ExitError)
	}
	fmt.Println(string(data))
}

// PrintError outputs an error message in text or JSON format.
func PrintError(cmd *cobra.Command, msg string) {
	cfg := GetConfig(cmd)
	if cfg != nil && cfg.JSONOutput {
		PrintJSON(map[string]string{"error": msg})
	} else {
		fmt.Fprintf(os.Stderr, "error: %s\n", msg)
	}
}

// PrintSuccess outputs a success message in text or JSON format.
func PrintSuccess(cmd *cobra.Command, msg string) {
	cfg := GetConfig(cmd)
	if cfg != nil && cfg.JSONOutput {
		PrintJSON(map[string]string{"status": "success", "message": msg})
	} else {
		fmt.Printf("✓ %s\n", msg)
	}
}

// OpenInEditor writes initial to a temp file, opens it in the user's editor,
// reads back the saved content, and strips lines beginning with `#` (treated
// as instructional comments, git-commit style). The suffix (e.g. ".md") is
// applied so editors apply the right syntax highlighting. Editor lookup
// order: GITSOCIAL_EDITOR, EDITOR, VISUAL, then `vi`.
//
// Returns an error if the editor exits non-zero. Returns "" with no error
// when the saved content is empty or only comments (caller decides whether
// that aborts the command — git commit treats it as abort; memo create
// treats it as "subject-only memo").
func OpenInEditor(initial, suffix string) (string, error) {
	f, err := os.CreateTemp("", "gitsocial-edit-*"+suffix)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath)

	if initial != "" {
		if _, err := f.WriteString(initial); err != nil {
			f.Close()
			return "", fmt.Errorf("write template: %w", err)
		}
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close temp file: %w", err)
	}

	editor := os.Getenv("GITSOCIAL_EDITOR")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}

	// Run editor with passthrough stdio so the user sees the editor UI.
	cmd := exec.Command("sh", "-c", editor+" "+tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor %q exited with error: %w", editor, err)
	}

	raw, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("read edited content: %w", err)
	}
	return stripCommentLines(string(raw)), nil
}

// stripCommentLines removes lines starting with `#` (after leading whitespace)
// from the editor's saved content, mirroring git's commit-message convention.
// Trailing/leading whitespace is also trimmed so an editor save that left a
// single trailing newline doesn't count as content.
func stripCommentLines(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// IsInteractive reports whether the process is running attached to a terminal
// on stdin and stdout. Used by commands that want to open $EDITOR only when
// the user is at a TTY (scripts and piped invocations should skip the editor).
func IsInteractive() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}

// ExitCode converts an error code string to an exit code integer.
func ExitCode(code string) int {
	switch code {
	case "NOT_A_REPOSITORY":
		return ExitNotRepo
	case "INVALID_ARGUMENT":
		return ExitInvalidArgs
	case "PERMISSION_DENIED":
		return ExitPermission
	case "NETWORK_ERROR":
		return ExitNetwork
	default:
		return ExitError
	}
}
