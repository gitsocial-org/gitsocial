// clone.go - gitsocial clone: git clone with zero-setup s3:// remote support
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// s3AliasKey/s3AliasValue is the per-repo git alias that lets the user's own
// git commands resolve s3:// remotes after gitsocial has touched the repo
// once (git spawns remote helpers as `git remote-s3`, which resolves aliases).
const (
	s3AliasKey   = "alias.remote-s3"
	s3AliasValue = "!gitsocial __git-remote-s3"
)

// newCloneCmd creates the clone command: git clone through gitsocial's git
// runner (which injects the s3 helper alias), so s3:// URLs need no setup.
func newCloneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clone <url> [directory]",
		Short: "Clone a repository (s3:// remotes work with no extra setup)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GetConfig(cmd)
			remoteURL := args[0]
			if canonical, isS3, err := protocol.ResolveS3URL(remoteURL); err != nil {
				return err
			} else if isS3 {
				remoteURL = canonical
			}
			dir := ""
			if len(args) == 2 {
				dir = args[1]
			} else {
				derived, err := cloneDir(remoteURL)
				if err != nil {
					return err
				}
				dir = derived
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Minute)
			defer cancel()
			if _, err := git.ExecGitContext(ctx, cfg.WorkDir, []string{"clone", remoteURL, dir}); err != nil {
				return err
			}
			repoDir := dir
			if !filepath.IsAbs(repoDir) {
				repoDir = filepath.Join(cfg.WorkDir, repoDir)
			}
			if strings.HasPrefix(remoteURL, "s3://") {
				if err := ensureLocalS3Alias(repoDir); err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Cloned into '%s'\n", dir)
			return nil
		},
	}
}

// cloneDir derives the target directory from the remote URL. For s3 URLs the
// name comes from the bucket/prefix (git's own guess would include the query
// params); otherwise it mirrors git's humanish rule (last segment, .git off).
func cloneDir(remoteURL string) (string, error) {
	if strings.HasPrefix(remoteURL, "s3://") {
		_, bucket, prefix, err := objstore.ParseS3URL(remoteURL)
		if err != nil {
			return "", err
		}
		if prefix == "" {
			return bucket, nil
		}
		return path.Base(strings.TrimSuffix(prefix, "/")), nil
	}
	trimmed := strings.TrimSuffix(strings.TrimSuffix(remoteURL, "/"), ".git")
	if u, err := url.Parse(trimmed); err == nil && u.Path != "" {
		trimmed = u.Path
	}
	name := path.Base(strings.ReplaceAll(trimmed, ":", "/"))
	if name == "" || name == "." || name == "/" {
		return "", fmt.Errorf("cannot derive a directory name from %q; pass one explicitly", remoteURL)
	}
	return name, nil
}

// ensureLocalS3Alias writes the s3 helper alias into the repo's local git
// config (unless one is already set), so plain git commands in that repo can
// resolve its s3:// remote without gitsocial in the loop.
func ensureLocalS3Alias(repoDir string) error {
	// --local scope only: the injected environment config must not mask an
	// unset local value, and a user's custom alias must never be overwritten.
	if _, err := git.ExecGit(repoDir, []string{"config", "--local", "--get", s3AliasKey}); err == nil {
		return nil
	}
	if _, err := git.ExecGit(repoDir, []string{"config", "--local", s3AliasKey, s3AliasValue}); err != nil {
		return fmt.Errorf("set %s in %s: %w", s3AliasKey, repoDir, err)
	}
	return nil
}

// ensureWorkspaceS3Alias best-effort applies the local alias when the current
// workspace's origin is an s3:// remote (covers repos pointed at a bucket by
// hand). Never fails a command.
func ensureWorkspaceS3Alias(workdir string) {
	if _, err := os.Stat(filepath.Join(workdir, ".git")); err != nil {
		return
	}
	if !strings.HasPrefix(git.GetGitConfig(workdir, "remote.origin.url"), "s3://") {
		return
	}
	_ = ensureLocalS3Alias(workdir)
}
