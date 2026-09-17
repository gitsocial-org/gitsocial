// root.go - Root command setup, global flags, and initialization
package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/identity"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

// newRootCmd creates the root command with global flags and initialization.
func newRootCmd() *cobra.Command {
	// One config per command tree: the flags write here, GetConfig reads it.
	cfg := &Config{}
	cmd := &cobra.Command{
		Use:           "gitsocial",
		Short:         "GitSocial - Social networking over Git",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if cfg.WorkDir == "" {
				wd, err := os.Getwd()
				if err != nil {
					return err
				}
				cfg.WorkDir = wd
			}

			if cfg.CacheDir == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				cfg.CacheDir = filepath.Join(home, ".cache", "gitsocial")
			}

			initLogging(cmd.ErrOrStderr(), cfg.JSONOutput)
			applyGitTimeout()
			// The remote-helper command is itself spawned by git; skip the
			// workspace alias check there to keep helper invocations lean.
			if cmd.Name() != "__git-remote-s3" {
				ensureWorkspaceS3Alias(cfg.WorkDir)
			}

			cmd.SetContext(WithConfig(context.Background(), cfg))

			return cache.Open(cfg.CacheDir)
		},
	}

	cmd.PersistentFlags().BoolVar(&cfg.JSONOutput, "json", false, "Output in JSON format")
	cmd.PersistentFlags().StringVarP(&cfg.WorkDir, "workdir", "C", "", "Working directory, default the current directory")
	cmd.PersistentFlags().StringVar(&cfg.CacheDir, "cache-dir", "", "Cache directory, default ~/.cache/gitsocial")

	cmd.CompletionOptions.HiddenDefaultCmd = true

	return cmd
}

// initLogging initializes the logger based on settings and output format.
func initLogging(out io.Writer, jsonOutput bool) {
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
		Output: out,
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
