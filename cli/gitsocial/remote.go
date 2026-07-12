// remote.go - gitsocial remote add: register a remote, translating a pasted
// AWS S3 console URL to canonical s3:// and recording the s3 helper alias.
package main

import (
	"fmt"
	"os"

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
// remote gitsocial push and site push target by default.
func newRemoteDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "default [name]",
		Short: "Set or show the default push remote (gitsocial.pushRemote)",
		Long: `Set the remote gitsocial pushes to by default, stored in
git config gitsocial.pushRemote. With no argument, prints the current
resolution: the configured name, or "heuristic: <resolved>" when unset.

Examples:
  gitsocial remote default            # Show the current resolution
  gitsocial remote default backup     # Set "backup" as the default push remote`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)

			if len(args) == 0 {
				configured := git.ConfiguredPushRemote(cfg.WorkDir)
				if cfg.JSONOutput {
					PrintJSON(map[string]string{"configured": configured, "resolved": git.PushRemote(cfg.WorkDir)})
					return
				}
				if configured != "" {
					fmt.Println(configured)
				} else {
					fmt.Printf("heuristic: %s\n", git.PushRemote(cfg.WorkDir))
				}
				return
			}

			name := args[0]
			if _, err := git.ExecGit(cfg.WorkDir, []string{"remote", "get-url", name}); err != nil {
				PrintError(cmd, fmt.Sprintf("remote %q does not exist", name))
				os.Exit(ExitError)
			}
			if _, err := git.ExecGit(cfg.WorkDir, []string{"config", "gitsocial.pushRemote", name}); err != nil {
				PrintError(cmd, fmt.Sprintf("set gitsocial.pushRemote: %v", err))
				os.Exit(ExitError)
			}
			PrintSuccess(cmd, fmt.Sprintf("Default push remote set to %q", name))
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
