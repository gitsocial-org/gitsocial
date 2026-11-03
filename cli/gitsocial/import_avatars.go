// import_avatars.go - CLI command that copies author pictures into the avatar store and the site bucket
package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/client"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	importpkg "github.com/gitsocial-org/gitsocial/library/import"
	ghimport "github.com/gitsocial-org/gitsocial/library/import/github"
)

// newImportAvatarsCmd builds the import subcommand that copies author pictures into the store and the bucket.
func newImportAvatarsCmd() *cobra.Command {
	var limit int
	var dryRun bool
	var setEmail, setFile, remote string
	cmd := &cobra.Command{
		Use:   "avatars [url]",
		Short: "Copy author pictures for the site",
		Long: `Copy the GitHub picture of each author of the workspace into the local
avatar store and the s3 remote, for the avatars of the site. With no URL
origin is used. With no s3 remote the run fills only the store, and the
next push to an s3 remote publishes it. Re-running skips an author whose
picture the store or the bucket holds, and for 30 days an author with no
picture. A noreply address costs no API call; any other author costs one
gh api call. At the rate limit the run stops; run it again later.

The run writes:
  <cache>/imports/<repo>.avatars/<key>  one picture per author email
  .gitsocial/site/avatars/<key>         the same picture in the bucket
  .gitsocial/site/avatars.json          the keys, after the pictures
  <cache>/imports/<repo>.avatars.json   the memo of hits and misses

Examples:
  gitsocial import avatars                      # origin, the push remote
  gitsocial import avatars --dry-run            # the counts, no forge call
  gitsocial import avatars --remote r2 --limit 50
  gitsocial import avatars --set-email ada@example.com --set-file ada.png`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			rawURL := git.GetOriginURL(cfg.WorkDir)
			if len(args) > 0 {
				rawURL = args[0]
			}
			if rawURL == "" {
				return fmt.Errorf("no URL given and no origin remote: pass the repository URL")
			}
			if !strings.Contains(rawURL, "://") && !strings.HasPrefix(rawURL, "git@") {
				rawURL = "https://" + rawURL
			}
			repoURL := protocol.NormalizeURL(rawURL)
			repoInfo := protocol.ParseRepo(repoURL)
			if repoInfo == nil || protocol.DetectHost(repoURL) != protocol.HostGitHub {
				return fmt.Errorf("%s is not a GitHub repository: pass a github.com URL", rawURL)
			}
			var remoteArgs []string
			if remote != "" {
				remoteArgs = []string{remote}
			}
			remotes, resolution := client.ResolveRemotes(cfg.WorkDir, remoteArgs)
			remoteURL := ""
			if len(remotes) > 0 {
				remoteURL = git.RemoteURL(cfg.WorkDir, remotes[0])
			}
			switch {
			case strings.HasPrefix(remoteURL, "s3://"):
				printRemoteHint(cmd.ErrOrStderr(), cfg.WorkDir, remotes, resolution)
			case remote != "":
				return fmt.Errorf("remote %q is not an s3 remote: choose one with --remote", remote)
			default:
				remoteURL = ""
				if !cfg.JSONOutput {
					fmt.Fprintln(cmd.ErrOrStderr(), "gitsocial: no s3 remote, so the pictures go to the local store. The next push publishes them")
				}
			}
			if !dryRun && setEmail == "" {
				if err := ghimport.CheckGH(); err != nil {
					return err
				}
			}
			slug := repoInfo.Owner + "/" + repoInfo.Repo
			opts := importpkg.AvatarOptions{
				WorkDir:   cfg.WorkDir,
				RepoURL:   repoURL,
				RemoteURL: remoteURL,
				CacheDir:  cfg.CacheDir,
				Limit:     limit,
				DryRun:    dryRun,
				SetEmail:  setEmail,
				SetFile:   setFile,
				Source:    "github",
				Resolve: func(email string, originURLs []string, sha string) (string, error) {
					return ghimport.AvatarURL(slug, email, originURLs, sha)
				},
				NoCall: func(email string) bool {
					_, ok := ghimport.ParseNoreply(email)
					return ok
				},
			}
			progress := func(msg string) {
				if !cfg.JSONOutput {
					fmt.Fprintln(cmd.ErrOrStderr(), msg)
				}
			}
			stats, err := importpkg.RunAvatars(opts, progress)
			if err != nil {
				return err
			}
			if cfg.JSONOutput {
				return PrintJSON(cmd, stats)
			}
			out := cmd.OutOrStdout()
			verb, setVerb := "uploaded", "Uploaded"
			if remoteURL == "" {
				verb, setVerb = "stored", "Stored"
			}
			switch {
			case setEmail != "":
				fmt.Fprintf(out, "%s %d picture\n", setVerb, stats.Uploaded)
			case dryRun:
				fmt.Fprintf(out, "done %d, fresh misses %d, noreply %d, lookup %d\n", stats.Done, stats.FreshMisses, stats.NoCall, stats.Lookup)
			default:
				fmt.Fprintf(out, "%s %d, missed %d, failed %d, remaining %d\n", verb, stats.Uploaded, stats.Missed, stats.Failed, stats.Remaining)
				if stats.RateLimited {
					fmt.Fprintf(cmd.ErrOrStderr(), "gitsocial: stopped at the GitHub rate limit, %d remain. Run the command again later\n", stats.Remaining)
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 200, "Max authors to look up in one run")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the counts, call no forge and write nothing")
	cmd.Flags().StringVar(&setEmail, "set-email", "", "Email whose picture --set-file sets")
	cmd.Flags().StringVar(&setFile, "set-file", "", "Local PNG, JPEG or GIF to set for --set-email")
	cmd.Flags().StringVar(&remote, "remote", "", "The s3 remote, else the push remote")
	cmd.MarkFlagsRequiredTogether("set-email", "set-file")
	return cmd
}
