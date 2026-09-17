// id.go - CLI commands for identity verification (verify, resolve)
package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/identity"
)

// newIDCmd creates the top-level id command.
func newIDCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "id",
		Short: "Verify and resolve commit identities",
	}
	cmd.AddCommand(
		newIDVerifyCmd(),
		newIDResolveCmd(),
	)
	return cmd
}

func newIDVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify <commit>",
		Short: "Verify a commit signature",
		Long: `Verify a commit's signature against the forge's attestations and, when
identity.dns_verification is on, a DNS binding. The trust model is in
documentation/IDENTITY.md.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)

			hash := args[0]
			signerFormat, signerKey, err := git.GetCommitSignerKey(cfg.WorkDir, hash)
			if err != nil {
				if cfg.JSONOutput {
					if err := PrintJSON(cmd, map[string]any{"verified": false, "error": err.Error()}); err != nil {
						return err
					}
				} else {
					fmt.Printf("Unverified: %s\n", err.Error())
				}
				return nil
			}
			email, _ := getCommitEmail(cfg.WorkDir, hash)
			repoURL := gitmsg.ResolveRepoURL(cfg.WorkDir)
			signerKey = identity.NormalizeSignerKey(signerKey)

			binding, vErr := identity.VerifyBinding(signerKey, email, repoURL, hash)
			if cfg.JSONOutput {
				out := map[string]any{
					"verified":      binding != nil && binding.Verified,
					"signer_format": signerFormat,
					"signer_key":    signerKey,
					"email":         email,
				}
				if binding != nil {
					out["source"] = string(binding.Source)
					if binding.ForgeHost != "" {
						out["forge_host"] = binding.ForgeHost
					}
					if binding.ForgeAccount != "" {
						out["forge_account"] = binding.ForgeAccount
					}
				}
				if vErr != nil {
					out["error"] = vErr.Error()
				}
				return PrintJSON(cmd, out)
			}
			if binding != nil && binding.Verified {
				fmt.Printf("Verified: %s (%s, source: %s", email, signerFormat, binding.Source)
				if binding.ForgeHost != "" {
					fmt.Printf(", host: %s", binding.ForgeHost)
				}
				fmt.Println(")")
				return nil
			}
			fmt.Println("Unverified")
			return nil
		},
	}
}

func newIDResolveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resolve <email>",
		Short: "Resolve an identity over DNS",
		Long: `Resolve an email address to its identity through the DNS well-known
endpoint of its domain.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GetConfig(cmd)
			email := args[0]

			resolved, err := identity.ResolveIdentity(email)
			if err != nil {
				PrintError(cmd, err.Error())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, resolved)
			} else {
				fmt.Printf("Email:    %s\n", resolved.Email)
				fmt.Printf("Key:      %s\n", resolved.Key)
				fmt.Printf("Type:     %s\n", resolved.KeyType())
				if resolved.Repo != "" {
					fmt.Printf("Repo:     %s\n", resolved.Repo)
				}
				if resolved.Cached {
					fmt.Printf("Source:   cached\n")
				} else {
					fmt.Printf("Source:   fetched\n")
				}
			}
			return nil
		},
	}
}

// getCommitEmail returns a commit's author email, or "" on lookup failure.
func getCommitEmail(workdir, hash string) (string, error) {
	r, err := git.ExecGit(workdir, []string{"log", "-1", "--format=%ae", hash})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(r.Stdout), nil
}

// FormatAuthorWithVerification returns "Name ⚿ <email>" when the commit's
// signing key has been verified-bound to the email; otherwise "Name <email>".
func FormatAuthorWithVerification(name, email, repoURL, hash string) string {
	if repoURL != "" && email != "" && hash != "" && identity.IsVerifiedCommit(repoURL, hash, email) {
		return fmt.Sprintf("%s ⚿ <%s>", name, email)
	}
	return fmt.Sprintf("%s <%s>", name, email)
}
