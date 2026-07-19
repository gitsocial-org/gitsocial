// remote.go - gitsocial remote add: register a remote, translating a pasted
// AWS S3 console URL to canonical s3:// and recording the s3 helper alias.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/git"
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
	return cmd
}

// newRemoteDefaultCmd sets or reports git config gitsocial.pushRemote, the
// remote(s) gitsocial push and site push target by default. The key is
// multi-valued: a bare `gitsocial push` fans out to every configured remote.
func newRemoteDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "default [name...]",
		Short: "Set or show the default push remote(s) (gitsocial.pushRemote)",
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
				configured := git.ConfiguredPushRemotes(cfg.WorkDir)
				if cfg.JSONOutput {
					PrintJSON(map[string]any{"configured": configured, "resolved": git.PushRemotes(cfg.WorkDir)})
					return
				}
				if len(configured) > 0 {
					fmt.Println(strings.Join(configured, " "))
				} else {
					fmt.Printf("heuristic: %s\n", strings.Join(git.PushRemotes(cfg.WorkDir), " "))
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
	return &cobra.Command{
		Use:   "add [name] <url>",
		Short: "Add a remote (accepts s3:// and pasted AWS S3 console URLs)",
		Long: `Add a git remote. When the URL is an s3:// remote or a pasted AWS S3
console URL it is normalized to the canonical s3://<endpoint-host>/<bucket>/<prefix>
form and the s3 helper alias is recorded, so both gitsocial and plain git work
with no further setup. Name defaults to "origin".

Examples:
  gitsocial remote add s3://s3.us-east-1.amazonaws.com/my-bucket/repo
  gitsocial remote add https://us-east-1.console.aws.amazon.com/s3/buckets/my-bucket
  gitsocial remote add upstream s3://s3.us-east-1.amazonaws.com/my-bucket/repo`,
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
		},
	}
}
