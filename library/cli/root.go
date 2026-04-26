// root.go - Root command setup, global flags, and initialization
package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/identity"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/settings"
)

var (
	jsonOutput bool
	workdir    string
	cacheDir   string
)

// newRootCmd creates the root command with global flags and initialization.
func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "gitsocial",
		Short:   "GitSocial - Social networking over Git",
		Version: version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if workdir == "" {
				wd, err := os.Getwd()
				if err != nil {
					return err
				}
				workdir = wd
			}

			if cacheDir == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				cacheDir = filepath.Join(home, ".cache", "gitsocial")
			}

			initLogging(jsonOutput)
			applyGitTimeout()

			cfg := &Config{
				WorkDir:    workdir,
				CacheDir:   cacheDir,
				JSONOutput: jsonOutput,
			}
			ctx := WithConfig(context.Background(), cfg)
			cmd.SetContext(ctx)

			return cache.Open(cacheDir)
		},
	}

	cmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	cmd.PersistentFlags().StringVarP(&workdir, "workdir", "C", "", "Working directory (default: current directory)")
	cmd.PersistentFlags().StringVar(&cacheDir, "cache-dir", "", "Cache directory (default: ~/.cache/gitsocial)")

	cmd.CompletionOptions.HiddenDefaultCmd = true

	return cmd
}

// initLogging initializes the logger based on settings and output format.
func initLogging(jsonOutput bool) {
	settingsPath, err := settings.DefaultPath()
	if err != nil {
		slog.Debug("settings default path", "error", err)
	}
	s, err := settings.Load(settingsPath)
	if err != nil {
		slog.Debug("settings load", "error", err)
	}

	level := log.LevelInfo
	if s != nil {
		if lvl, ok := settings.Get(s, "log.level"); ok {
			level = log.ParseLevel(lvl)
		}
		identity.SetDNSVerificationEnabled(s.Identity.DNSVerification)
	}

	mode := log.ModeText
	if jsonOutput {
		mode = log.ModeJSON
	}

	log.Init(log.Config{
		Level:  level,
		Mode:   mode,
		Output: os.Stderr,
	})
}

// applyGitTimeout loads fetch.timeout from settings and applies it to the git package.
func applyGitTimeout() {
	settingsPath, err := settings.DefaultPath()
	if err != nil {
		slog.Debug("settings default path", "error", err)
	}
	s, err := settings.Load(settingsPath)
	if err != nil {
		slog.Debug("settings load", "error", err)
	}
	if s == nil {
		return
	}
	if val, ok := settings.Get(s, "fetch.timeout"); ok {
		var seconds int
		for _, c := range val {
			if c >= '0' && c <= '9' {
				seconds = seconds*10 + int(c-'0')
			}
		}
		if seconds > 0 {
			git.SetTimeout(time.Duration(seconds) * time.Second)
		}
	}
}
