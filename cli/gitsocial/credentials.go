// credentials.go - gitsocial config credentials: per-endpoint S3 key pairs in
// ~/.config/gitsocial/credentials.json, so a multi-remote push can sign each
// provider with its own keys.
package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// newCredentialsConfigCmd creates the `config credentials` group managing the
// per-endpoint-host S3 credentials file.
func newCredentialsConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "credentials",
		Short: "Manage per-endpoint S3 credentials (credentials.json)",
		Long: `Manage S3 credentials stored per endpoint host in
~/.config/gitsocial/credentials.json (0600; honors XDG_CONFIG_HOME), so a
multi-remote push can authenticate to several providers in one invocation.

Resolution precedence when signing requests to a bucket:
  1. GITSOCIAL_S3_ACCESS_KEY / GITSOCIAL_S3_SECRET_KEY (explicit override)
  2. the credentials.json entry for the remote's endpoint host
  3. AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY

Credentials are keyed by endpoint host (e.g. <account>.r2.cloudflarestorage.com,
s3.us-east-1.amazonaws.com, 127.0.0.1:9000) — the granularity providers scope
keys to.`,
	}
	cmd.AddCommand(newCredentialsSetCmd(), newCredentialsListCmd(), newCredentialsRemoveCmd())
	return cmd
}

// resolveCredentialHost maps a remote name, s3:// URL, or bare endpoint host to
// the credentials-file key (the canonical URL's endpoint host).
func resolveCredentialHost(workdir, arg string) (string, error) {
	target := arg
	if out, err := git.ExecGit(workdir, []string{"remote", "get-url", arg}); err == nil {
		target = strings.TrimSpace(out.Stdout)
	}
	if strings.HasPrefix(target, "s3://") {
		host, _, _, err := objstore.ParseS3URL(target)
		if err != nil {
			return "", fmt.Errorf("resolve endpoint host: %w", err)
		}
		return host, nil
	}
	if target != arg {
		return "", fmt.Errorf("remote %q is not an s3 remote (%s)", arg, target)
	}
	host := strings.ToLower(strings.TrimSpace(arg))
	if host == "" || strings.ContainsAny(host, "/ ") {
		return "", fmt.Errorf("not a remote name or endpoint host: %q", arg)
	}
	return host, nil
}

// maskKey shortens a key for listing: first 4 chars plus an ellipsis.
func maskKey(key string) string {
	if len(key) <= 4 {
		return key
	}
	return key[:4] + "…"
}

func newCredentialsSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <remote-or-host>",
		Short: "Store an access/secret key pair for a remote's endpoint host",
		Long: `Store an S3 key pair for a remote (resolved to its endpoint host) or a bare
endpoint host. Reads two lines from stdin — the access key, then the secret
key — so it works both interactively and piped:

  gitsocial config credentials set r2
  printf '%s\n%s\n' "$ACCESS" "$SECRET" | gitsocial config credentials set r2

The file is written with 0600 permissions.`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			host, err := resolveCredentialHost(cfg.WorkDir, args[0])
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			// Prompts go to stderr so piped stdin and --json stdout stay clean.
			reader := bufio.NewReader(cmd.InOrStdin())
			readLine := func(prompt string) string {
				fmt.Fprint(os.Stderr, prompt)
				line, err := reader.ReadString('\n')
				if err != nil && line == "" {
					return ""
				}
				return strings.TrimSpace(line)
			}
			access := readLine("Access key: ")
			secret := readLine("Secret key: ")
			if access == "" || secret == "" {
				PrintError(cmd, "expected the access key and the secret key as two stdin lines")
				os.Exit(ExitError)
			}
			creds, err := objstore.ReadCredentialsFile()
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			creds[host] = objstore.Credential{AccessKey: access, SecretKey: secret}
			if err := objstore.WriteCredentialsFile(creds); err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(map[string]string{"host": host, "accessKey": maskKey(access)})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("Stored credentials for %s (%s)", host, maskKey(access)))
			}
		},
	}
}

func newCredentialsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List stored credential hosts with masked access keys",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			creds, err := objstore.ReadCredentialsFile()
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			hosts := make([]string, 0, len(creds))
			for host := range creds {
				hosts = append(hosts, host)
			}
			sort.Strings(hosts)
			if cfg.JSONOutput {
				masked := make(map[string]string, len(creds))
				for _, host := range hosts {
					masked[host] = maskKey(creds[host].AccessKey)
				}
				PrintJSON(masked)
				return
			}
			if len(hosts) == 0 {
				fmt.Println("No credentials stored")
				return
			}
			for _, host := range hosts {
				fmt.Printf("%s = %s\n", host, maskKey(creds[host].AccessKey))
			}
		},
	}
}

func newCredentialsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <host>",
		Short: "Remove the stored credentials for an endpoint host",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			host := strings.ToLower(strings.TrimSpace(args[0]))
			creds, err := objstore.ReadCredentialsFile()
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			if _, ok := creds[host]; !ok {
				PrintError(cmd, fmt.Sprintf("no credentials stored for %s", host))
				os.Exit(ExitError)
			}
			delete(creds, host)
			if err := objstore.WriteCredentialsFile(creds); err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(map[string]string{"removed": host})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("Removed credentials for %s", host))
			}
		},
	}
}
