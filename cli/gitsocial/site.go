// site.go - gitsocial site: static browser read-surface on s3 remotes
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/clientpush"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// newSiteCmd creates the `site` command group.
func newSiteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "site",
		Short: "Manage the static browser read-surface on s3 remotes",
	}
	cmd.AddCommand(newSitePushCmd())
	return cmd
}

// newSitePushCmd uploads the embedded site shell to an s3 remote's bucket. This
// is the explicit site refresh / catch-up; once `site.publish` is enabled,
// `gitsocial push` maintains the site on every push, so this command is for
// refreshing the site without a data push.
func newSitePushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push [remote]",
		Short: "Upload the browser read-surface to an s3 remote (explicit refresh)",
		Long: `Upload the embedded static site to an s3 remote's bucket, alongside the
repo data. Anyone can then browse the repo's timeline, issues, PRs, and
releases from the bucket's public domain with no gitsocial install. Remote
defaults to the gitsocial push remote (git config gitsocial.pushRemote, else
the s3 remote heuristic).

The site is enabled per repo with ` + "`gitsocial config site set publish true`" + `
(default off; publish the config with a regular push). Once enabled, every
` + "`gitsocial push`" + ` maintains the site; use this command to refresh it — or to
catch an already-pushed repo up right after enabling the guard — without
pushing new data.

The page reads the bucket directly, so it stays current with every push; the
bucket (or its public domain, e.g. r2.dev or a custom domain on Cloudflare R2)
must allow public reads for visitors without credentials.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			name := git.PushRemote(cfg.WorkDir)
			if len(args) == 1 {
				name = args[0]
			}
			result, err := git.ExecGit(cfg.WorkDir, []string{"remote", "get-url", name})
			if err != nil {
				PrintError(cmd, fmt.Sprintf("remote %q: %v", name, err))
				os.Exit(ExitError)
			}
			remoteURL := result.Stdout
			if !strings.HasPrefix(remoteURL, "s3://") {
				PrintError(cmd, fmt.Sprintf("remote %q is not an s3 remote: %s", name, remoteURL))
				os.Exit(ExitError)
			}
			// Live progress to stderr (same TTY/non-TTY policy as the git-spawned
			// helper); suppressed in --json so machine output stays clean.
			var progress objstore.Progress
			var progressDone = func() {}
			if !cfg.JSONOutput {
				progress, progressDone = objstore.StderrProgress()
			}
			published, err := clientpush.PublishSite(cfg.WorkDir, remoteURL, progress)
			progressDone()
			if err != nil {
				PrintError(cmd, fmt.Sprintf("push site to %s: %v", remoteURL, err))
				os.Exit(ExitError)
			}
			if !published {
				PrintError(cmd, "site publishing is disabled for this repo; enable with `gitsocial config site set publish true`")
				os.Exit(ExitError)
			}
			PrintSuccess(cmd, fmt.Sprintf("Uploaded browser read-surface to %s", remoteURL))
		},
	}
}
