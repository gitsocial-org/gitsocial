// release_artifacts_push.go - CLI command to push release artifacts to an s3 remote's bucket
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/extensions/release"
)

// newReleaseArtifactsPushCmd creates the `release artifacts push` command.
func newReleaseArtifactsPushCmd() *cobra.Command {
	var remote string

	cmd := &cobra.Command{
		Use:   "push <version> <file...>",
		Short: "Upload release artifacts to the s3 push remote (artifacts/<version>/)",
		Long: `Upload a release's artifact files as plain objects to the s3 push remote's
bucket at artifacts/<version>/<filename>, maintain artifacts/latest.txt (the
newest non-prerelease version pushed), and set artifact-url on the release
record when it exists without one. Re-pushing a version overwrites its objects.

The full download URL for each file is <site url>/artifacts/<version>/<filename>,
derived from the remote's effective site url (` + "`gitsocial config site set url ...`" + `,
overridable per remote).`,
		Args: cobra.MinimumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			if err := release.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "release", "error", err)
			}
			version := normalizeReleaseVersion(args[0])
			result := release.PushArtifacts(cfg.WorkDir, version, args[1:], remote)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(result.Data)
				return
			}
			PrintSuccess(cmd, fmt.Sprintf("Pushed %d artifact(s) for %s to %s", len(result.Data.Files), version, result.Data.Remote))
			for _, f := range result.Data.Files {
				fmt.Printf("  %s/%s  (%s)\n", result.Data.BaseURL, f.Filename, release.FormatSize(f.Size))
			}
			if result.Data.LatestAdvanced {
				fmt.Printf("latest.txt → %s\n", version)
			}
			if result.Data.RecordUpdated {
				fmt.Printf("Release record updated: artifact-url = %s\n", result.Data.BaseURL)
			}
		},
	}

	cmd.Flags().StringVar(&remote, "remote", "", "Target remote (default: the push remote)")
	return cmd
}

// normalizeReleaseVersion strips a conventional leading "v" from a version
// argument (v1.2.3 → 1.2.3).
func normalizeReleaseVersion(version string) string {
	if len(version) > 1 && version[0] == 'v' && version[1] >= '0' && version[1] <= '9' {
		return version[1:]
	}
	return version
}
