// remote.go - gitsocial remote: target-scoped operations — add (translating a
// pasted AWS S3 console URL to canonical s3:// and recording the s3 helper
// alias), default (the gitsocial.pushRemote defaults), and put (a plain
// bucket-object upload).
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// newRemoteCmd creates the `remote` command group.
func newRemoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote",
		Short: "Manage repository remotes",
	}
	cmd.AddCommand(newRemoteAddCmd())
	cmd.AddCommand(newRemoteDefaultCmd())
	cmd.AddCommand(newRemotePutCmd())
	return cmd
}

// newRemotePutCmd uploads a single local file as a plain object to an s3
// remote's bucket. It publishes foreign objects that live alongside the repo
// data but aren't part of the generated site (e.g. install.sh at the bucket
// root), which the release driver keeps current with each release. Site
// maintenance never deletes unrecognized root keys, so such objects are safe.
func newRemotePutCmd() *cobra.Command {
	var remote string
	var contentType string
	cmd := &cobra.Command{
		Use:   "put <key> <file>",
		Short: "Upload one file to an s3 remote's bucket",
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
				PrintError(cmd, fmt.Sprintf("remote %q is %s, not an s3 remote", remote, remoteURL))
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
	cmd.Flags().StringVar(&remote, "remote", "", "Target remote, default the push remote")
	cmd.Flags().StringVar(&contentType, "content-type", "", "Content-Type for the uploaded object")
	return cmd
}

// newRemoteDefaultCmd sets or reports git config gitsocial.pushRemote, the
// remote(s) gitsocial push targets by default. The key is
// multi-valued: a bare `gitsocial push` fans out to every configured remote.
func newRemoteDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "default [name...]",
		Short: "Set or show the default push remotes",
		Long: `Set the remote(s) gitsocial pushes to by default, stored in
git config gitsocial.pushRemote (multi-valued). With no argument, prints the
current resolution: the configured names, or "heuristic: <resolved>" when unset.
With several names, a bare ` + "`gitsocial push`" + ` fans out to each in order.

Examples:
  gitsocial remote default            # Show the current resolution
  gitsocial remote default backup     # Set "backup" as the default push remote
  gitsocial remote default r2 s3      # Push to both "r2" and "s3" by default`,
		Args: cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)

			if len(args) == 0 {
				resolved, resolution := git.ResolvePushRemotes(cfg.WorkDir)
				if cfg.JSONOutput {
					PrintJSON(map[string]any{"configured": git.ConfiguredPushRemotes(cfg.WorkDir), "resolved": resolved})
					return
				}
				if resolution == git.PushConfigured {
					fmt.Println(strings.Join(resolved, " "))
				} else {
					fmt.Printf("heuristic: %s\n", strings.Join(resolved, " "))
				}
				return
			}

			for _, name := range args {
				if _, err := git.ExecGit(cfg.WorkDir, []string{"remote", "get-url", name}); err != nil {
					PrintError(cmd, fmt.Sprintf("remote %q does not exist", name))
					os.Exit(ExitError)
				}
			}
			if err := git.SetConfiguredPushRemotes(cfg.WorkDir, args); err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			PrintSuccess(cmd, fmt.Sprintf("Default push remote(s) set to %q", strings.Join(args, " ")))
		},
	}
}

// newRemoteAddCmd adds a remote, resolving s3:// and pasted AWS S3 console URLs
// to a canonical s3:// remote and recording the helper alias so plain git works.
func newRemoteAddCmd() *cobra.Command {
	var makeDefault bool
	var enableSite bool
	cmd := &cobra.Command{
		Use:   "add [name] <url>",
		Short: "Add a git or s3 remote",
		Long: `Add a git remote. An s3:// URL or a pasted AWS S3 console URL is
normalized to s3://<endpoint-host>/<bucket>/<prefix> and the s3 helper
alias is recorded, so plain git works there too. The name defaults to
origin. The accepted URL shapes are in documentation/S3.md.

--default appends the remote to git config gitsocial.pushRemote.
--site sets site.publish in the core config, so a push to an s3 remote
also rebuilds the browser static site.

Examples:
  gitsocial remote add s3://s3.us-east-1.amazonaws.com/my-bucket/repo
  gitsocial remote add https://us-east-1.console.aws.amazon.com/s3/buckets/my-bucket
  gitsocial remote add upstream s3://<endpoint-host>/<bucket>/repo
  gitsocial remote add s3 s3://<endpoint-host>/<bucket>/repo --default --site`,
		Args: cobra.RangeArgs(1, 2),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			name, rawURL := "origin", args[0]
			if len(args) == 2 {
				name, rawURL = args[0], args[1]
			}
			remoteURL := rawURL
			canonical, isS3, err := protocol.ResolveS3URL(rawURL)
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			if isS3 {
				remoteURL = canonical
			}
			if _, err := git.ExecGit(cfg.WorkDir, []string{"remote", "add", name, remoteURL}); err != nil {
				PrintError(cmd, fmt.Sprintf("add remote %q: %v", name, err))
				os.Exit(ExitError)
			}
			if isS3 {
				if err := ensureLocalS3Alias(cfg.WorkDir); err != nil {
					PrintError(cmd, err.Error())
					os.Exit(ExitError)
				}
			}
			PrintSuccess(cmd, fmt.Sprintf("Added remote %q → %s", name, remoteURL))
			if makeDefault {
				if err := git.AppendConfiguredPushRemote(cfg.WorkDir, name); err != nil {
					PrintError(cmd, err.Error())
					os.Exit(ExitError)
				}
				PrintSuccess(cmd, fmt.Sprintf("Default push remote(s): %s", strings.Join(git.ConfiguredPushRemotes(cfg.WorkDir), " ")))
			}
			if enableSite {
				if err := writeSiteConfigValue(cfg.WorkDir, "publish", "true"); err != nil {
					PrintError(cmd, err.Error())
					os.Exit(ExitError)
				}
				PrintSuccess(cmd, "Site rebuild enabled (site.publish = true)")
			}
		},
	}
	cmd.Flags().BoolVar(&makeDefault, "default", false, "Append the remote to the default push targets")
	cmd.Flags().BoolVar(&enableSite, "site", false, "Set site.publish so a push rebuilds the site")
	return cmd
}
