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
	cmd.AddCommand(newSitePushCmd(), newSitePutCmd())
	return cmd
}

// newSitePutCmd uploads a single local file as a plain object to an s3 remote's
// bucket. It publishes foreign objects that live alongside the repo data but
// aren't part of the generated site (e.g. install.sh at the bucket root), which
// the release driver keeps current with each release. Site maintenance never
// deletes unrecognized root keys, so such objects are safe.
func newSitePutCmd() *cobra.Command {
	var remote string
	var contentType string
	cmd := &cobra.Command{
		Use:   "put <key> <file>",
		Short: "Upload a single local file as a plain object to an s3 remote's bucket",
		Long: `Upload <file> to the s3 remote's bucket at <key> (under the remote's prefix)
as a plain object, overwriting any existing object at that key. Remote defaults
to the gitsocial push remote. Used by the release driver to publish foreign
objects such as install.sh at the bucket root. The Cache-Control header follows
the key's mutability, so a root key like install.sh is stored no-cache.`,
		Args: cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			key, file := args[0], args[1]
			if remote == "" {
				remote = git.PushRemote(cfg.WorkDir)
			}
			remoteURL := git.RemoteURL(cfg.WorkDir, remote)
			if remoteURL == "" {
				PrintError(cmd, fmt.Sprintf("remote %q is not configured", remote))
				os.Exit(ExitError)
			}
			if !strings.HasPrefix(remoteURL, "s3://") {
				PrintError(cmd, fmt.Sprintf("remote %q (%s) is not an s3 remote", remote, remoteURL))
				os.Exit(ExitError)
			}
			data, err := os.ReadFile(file)
			if err != nil {
				PrintError(cmd, fmt.Sprintf("read %s: %v", file, err))
				os.Exit(ExitError)
			}
			if err := objstore.PutObjectToRemote(remoteURL, objstore.HelperEnvFromOS(), key, data, contentType); err != nil {
				PrintError(cmd, fmt.Sprintf("upload %s: %v", key, err))
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(map[string]any{"remote": remote, "key": key, "size": len(data)})
				return
			}
			PrintSuccess(cmd, fmt.Sprintf("Uploaded %s (%d bytes) to %s", key, len(data), remote))
		},
	}
	cmd.Flags().StringVar(&remote, "remote", "", "Target remote (default: the push remote)")
	cmd.Flags().StringVar(&contentType, "content-type", "", "Content-Type for the uploaded object")
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
			override := clientpush.ResolveSiteOverride(cfg.WorkDir, name)
			published, err := clientpush.PublishSite(cfg.WorkDir, remoteURL, override, progress)
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
