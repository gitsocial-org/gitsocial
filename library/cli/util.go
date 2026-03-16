// util.go - CLI utilities for config, output formatting, and exit codes
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/core/git"
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
