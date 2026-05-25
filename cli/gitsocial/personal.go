// personal.go - Top-level personal-repo management: init and sync.
// The personal bare repo holds user-scoped state that travels with the user
// across machines — settings prefs under refs/gitmsg/core/config today, with
// the memo personal tier expected to share the same repo once memo lands.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

// newPersonalCmd builds the `personal` command group.
func newPersonalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "personal",
		Short: "Manage the personal bare repo (user state synced across machines)",
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
		Short: "Create the personal bare repo (and optionally attach a remote)",
		Run: func(cmd *cobra.Command, _ []string) {
			path, err := settings.EnsurePersonalRepo()
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			if remote != "" {
				if _, err := git.ExecGit(path, []string{"remote", "remove", "origin"}); err != nil {
					_ = err // ignore — likely "no such remote"
				}
				if _, err := git.ExecGit(path, []string{"remote", "add", "origin", remote}); err != nil {
					PrintError(cmd, fmt.Sprintf("attach remote: %s", err))
					os.Exit(ExitError)
				}
			}
			cfg := GetConfig(cmd)
			if cfg != nil && cfg.JSONOutput {
				PrintJSON(map[string]string{"path": path, "remote": remote})
				return
			}
			PrintSuccess(cmd, fmt.Sprintf("personal repo at %s", path))
			if remote != "" {
				PrintSuccess(cmd, "origin → "+remote)
			}
		},
	}
	cmd.Flags().StringVar(&remote, "remote", "", "Attach an `origin` remote URL for sync")
	return cmd
}

func newPersonalSyncCmd() *cobra.Command {
	var pushOnly, fetchOnly bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Push/fetch the personal repo (gitmsg/* branches auto-merge on divergence)",
		Long: `Sync the personal bare repo with its remote.

Per-branch handling: each refs/heads/gitmsg/* branch is fetched/pushed via the
auto-merge helper — empty-tree append-only branches reconcile divergent
histories without conflicts. State refs under refs/gitmsg/* (settings config,
list metadata, etc.) sync as a single bulk refspec.`,
		Run: func(cmd *cobra.Command, _ []string) {
			path, err := settings.PersonalRepoPath()
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			if !settings.PersonalRepoExists() {
				PrintError(cmd, fmt.Sprintf("personal repo not initialized at %s (run `gitsocial personal init`)", path))
				os.Exit(ExitError)
			}
			if !personalHasOrigin(path) {
				PrintError(cmd, "personal repo has no `origin` remote (run `gitsocial personal init --remote <url>`)")
				os.Exit(ExitError)
			}
			doFetch := !pushOnly
			doPush := !fetchOnly
			branches := gitmsg.GetExtBranches(path)

			if doFetch {
				for _, branch := range branches {
					if err := gitmsg.FetchAndMergeBranch(path, branch); err != nil {
						PrintError(cmd, fmt.Sprintf("fetch %s: %s", branch, err))
						os.Exit(ExitError)
					}
				}
				if _, err := git.ExecGit(path, []string{
					"fetch", "origin", "refs/gitmsg/*:refs/gitmsg/*",
				}); err != nil {
					PrintError(cmd, fmt.Sprintf("fetch gitmsg refs: %s", err))
					os.Exit(ExitError)
				}
			}
			if doPush {
				for _, branch := range branches {
					if err := gitmsg.PushBranchWithMerge(path, branch); err != nil {
						PrintError(cmd, fmt.Sprintf("push %s: %s", branch, err))
						os.Exit(ExitError)
					}
				}
				if _, err := git.ExecGit(path, []string{
					"push", "origin", "refs/gitmsg/*:refs/gitmsg/*",
				}); err != nil {
					PrintError(cmd, fmt.Sprintf("push gitmsg refs: %s", err))
					os.Exit(ExitError)
				}
			}
			cfg := GetConfig(cmd)
			if cfg != nil && cfg.JSONOutput {
				PrintJSON(map[string]bool{"fetched": doFetch, "pushed": doPush})
				return
			}
			switch {
			case doFetch && doPush:
				PrintSuccess(cmd, "personal repo synced")
			case doFetch:
				PrintSuccess(cmd, "personal repo fetched")
			case doPush:
				PrintSuccess(cmd, "personal repo pushed")
			}
		},
	}
	cmd.Flags().BoolVar(&pushOnly, "push", false, "Push only (skip fetch)")
	cmd.Flags().BoolVar(&fetchOnly, "fetch", false, "Fetch only (skip push)")
	return cmd
}

func newPersonalStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show personal repo path, initialization state, and remote",
		Run: func(cmd *cobra.Command, _ []string) {
			path, err := settings.PersonalRepoPath()
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
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
				PrintJSON(map[string]interface{}{
					"path":        path,
					"initialized": exists,
					"remote":      remote,
				})
				return
			}
			fmt.Println("path:        " + path)
			fmt.Println("initialized: " + boolLabel(exists))
			if remote != "" {
				fmt.Println("remote:      " + remote)
			} else if exists {
				fmt.Println("remote:      (none — set with `gitsocial personal init --remote <url>`)")
			}
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
