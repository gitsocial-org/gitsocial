// personal.go - Top-level personal-repo management: init and sync.
// The personal bare repo holds user-scoped state that travels with the user
// across machines — settings prefs under refs/gitmsg/core/config and the memo
// personal tier (gitmsg/memo) share the same repo.
package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
	"github.com/gitsocial-org/gitsocial/library/extensions/memo"
)

// newPersonalCmd builds the `personal` command group.
func newPersonalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "personal",
		Short: "Manage the personal bare repository",
	}
	cmd.AddCommand(
		newPersonalInitCmd(),
		newPersonalSyncCmd(),
		newPersonalStatusCmd(),
	)
	return cmd
}

func newPersonalInitCmd() *cobra.Command {
	var remote string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create the personal bare repository",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := settings.EnsurePersonalRepo()
			if err != nil {
				PrintError(cmd, err.Error())
				return exit(ExitError)
			}
			if remote != "" {
				if _, err := git.ExecGit(path, []string{"remote", "remove", "origin"}); err != nil {
					_ = err // ignore — likely "no such remote"
				}
				if _, err := git.ExecGit(path, []string{"remote", "add", "origin", remote}); err != nil {
					PrintError(cmd, fmt.Sprintf("attach remote: %s", err))
					return exit(ExitError)
				}
			}
			cfg := GetConfig(cmd)
			if cfg != nil && cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{"path": path, "remote": remote})
			}
			PrintSuccess(cmd, fmt.Sprintf("personal repo at %s", path))
			if remote != "" {
				PrintSuccess(cmd, "origin → "+remote)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&remote, "remote", "", "Attach an `origin` remote URL for sync")
	return cmd
}

func newPersonalSyncCmd() *cobra.Command {
	var pushOnly, fetchOnly bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync the personal repo with its remote",
		Long: `Sync the personal bare repo with its remote.

Each refs/heads/gitmsg/* branch is fetched and pushed through the
auto-merge helper, so diverged branches reconcile without conflicts.
State refs under refs/gitmsg/*, the settings config and the list
metadata, sync as one bulk refspec. After a fetch, personal-tier memos
are re-indexed into the cache.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := settings.PersonalRepoPath()
			if err != nil {
				PrintError(cmd, err.Error())
				return exit(ExitError)
			}
			if !settings.PersonalRepoExists() {
				PrintError(cmd, fmt.Sprintf("personal repo not initialized at %s: run gitsocial personal init", path))
				return exit(ExitError)
			}
			if !personalHasOrigin(path) {
				PrintError(cmd, "personal repo has no origin remote: run gitsocial personal init --remote <url>")
				return exit(ExitError)
			}
			doFetch := !pushOnly
			doPush := !fetchOnly
			branches := gitmsg.GetExtBranches(path)

			if doFetch {
				for _, branch := range branches {
					if err := gitmsg.FetchAndMergeBranch(path, branch); err != nil {
						PrintError(cmd, fmt.Sprintf("fetch %s: %s", branch, err))
						return exit(ExitError)
					}
				}
				if _, err := git.ExecGit(path, []string{
					"fetch", "origin", "refs/gitmsg/*:refs/gitmsg/*",
				}); err != nil {
					PrintError(cmd, fmt.Sprintf("fetch gitmsg refs: %s", err))
					return exit(ExitError)
				}
				if err := memo.SyncTierRepoToCache(path); err != nil {
					PrintError(cmd, fmt.Sprintf("sync memo cache: %s", err))
					return exit(ExitError)
				}
			}
			if doPush {
				for _, branch := range branches {
					if err := gitmsg.PushBranchWithMerge(path, branch); err != nil {
						PrintError(cmd, fmt.Sprintf("push %s: %s", branch, err))
						return exit(ExitError)
					}
				}
				if _, err := git.ExecGit(path, []string{
					"push", "origin", "refs/gitmsg/*:refs/gitmsg/*",
				}); err != nil {
					PrintError(cmd, fmt.Sprintf("push gitmsg refs: %s", err))
					return exit(ExitError)
				}
			}
			cfg := GetConfig(cmd)
			if cfg != nil && cfg.JSONOutput {
				return PrintJSON(cmd, map[string]bool{"fetched": doFetch, "pushed": doPush})
			}
			switch {
			case doFetch && doPush:
				PrintSuccess(cmd, "personal repo synced")
			case doFetch:
				PrintSuccess(cmd, "personal repo fetched")
			case doPush:
				PrintSuccess(cmd, "personal repo pushed")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&pushOnly, "push-only", false, "Push only, skip the fetch")
	cmd.Flags().BoolVar(&fetchOnly, "fetch-only", false, "Fetch only, skip the push")
	return cmd
}

func newPersonalStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the personal repository state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := settings.PersonalRepoPath()
			if err != nil {
				PrintError(cmd, err.Error())
				return exit(ExitError)
			}
			exists := settings.PersonalRepoExists()
			remote := ""
			if exists {
				if out, err := git.ExecGit(path, []string{"config", "--get", "remote.origin.url"}); err == nil {
					remote = trimNewline(out.Stdout)
				}
			}
			cfg := GetConfig(cmd)
			if cfg != nil && cfg.JSONOutput {
				return PrintJSON(cmd, map[string]interface{}{
					"path":        path,
					"initialized": exists,
					"remote":      remote,
				})
			}
			fmt.Println("path:        " + path)
			fmt.Println("initialized: " + boolLabel(exists))
			if remote != "" {
				fmt.Println("remote:      " + remote)
			} else if exists {
				fmt.Println("remote:      (none — set with `gitsocial personal init --remote <url>`)")
			}
			return nil
		},
	}
}

func personalHasOrigin(path string) bool {
	out, err := git.ExecGit(path, []string{"config", "--get", "remote.origin.url"})
	if err != nil {
		return false
	}
	return trimNewline(out.Stdout) != ""
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func boolLabel(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
