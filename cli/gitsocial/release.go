// release.go - CLI commands for the release extension
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/client"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/text"
	"github.com/gitsocial-org/gitsocial/library/extensions/release"
)

const releaseExt = "release"

// init registers the release command tree.
func init() {
	RegisterExtension(ExtensionRegistration{
		Use:   "release",
		Short: "Manage releases and their artifacts",
		Register: func(cmd *cobra.Command) {
			cmd.AddCommand(
				newReleaseStatusCmd(),
				newReleaseInitCmd(),
				NewExtConfigCmd(releaseExt),
				newReleaseCreateCmd(),
				newReleaseListCmd(),
				newReleaseShowCmd(),
				newReleaseEditCmd(),
				newReleaseRetractCmd(),
				newReleaseArtifactsCmd(),
				newReleaseSBOMCmd(),
			)
		},
	})
}

// newReleaseStatusCmd builds the command that shows the extension's status.
func newReleaseStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show release extension status",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "error", err)
			}

			branch := release.ReleaseBranch
			res := release.GetReleases("", "", "", 0)
			count := 0
			if res.Success {
				count = len(res.Data)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]interface{}{
					"branch":   branch,
					"releases": count,
				})
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Release:")
				fmt.Fprintf(cmd.OutOrStdout(), "  Branch: %s\n", branch)
				fmt.Fprintf(cmd.OutOrStdout(), "  Releases: %d\n", count)
			}
			return nil
		},
	}
}

// newReleaseInitCmd builds the command that initializes GitRelease here.
func newReleaseInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize GitRelease in this repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)

			branch := release.ReleaseBranch
			relConfig := release.ReleaseConfig{
				Version: "0.1.0",
			}
			if err := release.SaveReleaseConfig(cfg.WorkDir, relConfig); err != nil {
				PrintError(cmd, "save release config: "+err.Error())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{
					"status": "initialized",
					"branch": branch,
				})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("GitRelease initialized (branch: %s)", branch))
			}
			return nil
		},
	}

	return cmd
}

// newReleaseCreateCmd builds the command that creates a release.
func newReleaseCreateCmd() *cobra.Command {
	var tag string
	var version string
	var prerelease bool
	var artifactsStr string
	var artifactURL string
	var checksums string
	var signedBy string
	var sbom string
	var labelsStr string
	var allowDuplicate bool

	cmd := &cobra.Command{
		Use:   "create <subject>",
		Short: "Create a new release",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			subject := args[0]
			body := ""

			if subject == "-" {
				scanner := bufio.NewScanner(cmd.InOrStdin())
				var lines []string
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
				}
				if err := scanner.Err(); err != nil {
					PrintError(cmd, "read stdin: "+err.Error())
					return exit(ExitError)
				}
				content := strings.Join(lines, "\n")
				parts := strings.SplitN(content, "\n\n", 2)
				subject = strings.TrimSpace(parts[0])
				if len(parts) > 1 {
					body = strings.TrimSpace(parts[1])
				}
			}

			if strings.TrimSpace(subject) == "" {
				PrintError(cmd, "release subject cannot be empty")
				return exit(ExitInvalidArgs)
			}

			opts := release.CreateReleaseOptions{
				Tag:            tag,
				Version:        version,
				Prerelease:     prerelease,
				Labels:         text.SplitCSV(labelsStr),
				ArtifactURL:    artifactURL,
				Checksums:      checksums,
				SignedBy:       signedBy,
				SBOM:           sbom,
				AllowDuplicate: allowDuplicate,
			}

			if artifactsStr != "" {
				for _, a := range strings.Split(artifactsStr, ",") {
					a = strings.TrimSpace(a)
					if a != "" {
						opts.Artifacts = append(opts.Artifacts, a)
					}
				}
			}

			result := release.CreateRelease(cfg.WorkDir, subject, body, opts)
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Release created")
				fmt.Fprintln(cmd.OutOrStdout())
				printReleaseDetails(cmd.OutOrStdout(), result.Data)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&tag, "tag", "t", "", "Git tag name, such as v1.0.0")
	cmd.Flags().StringVarP(&version, "version", "v", "", "Semver version, such as 1.0.0")
	cmd.Flags().BoolVar(&prerelease, "prerelease", false, "Mark as pre-release")
	cmd.Flags().StringVar(&artifactsStr, "artifacts", "", "Comma-separated artifact filenames")
	cmd.Flags().StringVar(&artifactURL, "artifact-url", "", "Base URL for externally hosted artifacts")
	cmd.Flags().StringVar(&checksums, "checksums", "", "Checksums filename, such as SHA256SUMS")
	cmd.Flags().StringVar(&signedBy, "signed-by", "", "Key fingerprint for release signature")
	cmd.Flags().StringVar(&sbom, "sbom", "", "SBOM filename, such as sbom.spdx.json")
	cmd.Flags().StringVarP(&labelsStr, "labels", "l", "", "Comma-separated labels, such as area/tui")
	cmd.Flags().BoolVar(&allowDuplicate, "allow-duplicate", false, "Allow creating a release with a tag that already exists")

	return cmd
}

// newReleaseListCmd builds the command that lists releases.
func newReleaseListCmd() *cobra.Command {
	var limit int
	var repoURL string
	var branch string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List releases",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GetConfig(cmd)
			if repoURL != "" {
				fetchResult := release.FetchRepository(cfg.CacheDir, repoURL, branch)
				if !fetchResult.Success {
					PrintError(cmd, fetchResult.Error.Text())
					return exit(ExitError)
				}
			} else {
				if !EnsureGitRepo(cmd) {
					return exit(ExitNotRepo)
				}
				if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
					slog.Debug("sync workspace", "error", err)
				}
			}

			result := release.GetReleases(repoURL, branch, "", limit)
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				if len(result.Data) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No releases found")
					return nil
				}
				for _, rel := range result.Data {
					printReleaseLine(cmd.OutOrStdout(), rel)
				}
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum number of releases")
	cmd.Flags().StringVarP(&repoURL, "repo", "r", "", "Repository URL, default the current workspace")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Branch name, default the release branch")

	return cmd
}

// newReleaseShowCmd builds the command that shows one release.
func newReleaseShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <release-ref>",
		Short: "Show release details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "error", err)
			}

			result := release.GetSingleRelease(args[0])
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				printReleaseDetails(cmd.OutOrStdout(), result.Data)
			}
			return nil
		},
	}
}

// newReleaseEditCmd builds the command that edits a release.
func newReleaseEditCmd() *cobra.Command {
	var body string
	var tag string
	var version string
	var sbom string
	var artifactsStr string
	var artifactURL string
	var checksums string
	var signedBy string
	var labelsStr string
	var prerelease bool

	cmd := &cobra.Command{
		Use:   "edit <release-ref>",
		Short: "Edit a release",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "error", err)
			}

			opts := release.EditReleaseOptions{}
			if cmd.Flags().Changed("body") {
				opts.Body = &body
			}
			if cmd.Flags().Changed("tag") {
				opts.Tag = &tag
			}
			if cmd.Flags().Changed("version") {
				opts.Version = &version
			}
			if cmd.Flags().Changed("sbom") {
				opts.SBOM = &sbom
			}
			if cmd.Flags().Changed("artifacts") {
				a := text.SplitCSV(artifactsStr)
				opts.Artifacts = &a
			}
			if cmd.Flags().Changed("artifact-url") {
				opts.ArtifactURL = &artifactURL
			}
			if cmd.Flags().Changed("checksums") {
				opts.Checksums = &checksums
			}
			if cmd.Flags().Changed("signed-by") {
				opts.SignedBy = &signedBy
			}
			if cmd.Flags().Changed("labels") {
				l := text.SplitCSV(labelsStr)
				opts.Labels = &l
			}
			if cmd.Flags().Changed("prerelease") {
				opts.Prerelease = &prerelease
			}

			result := release.EditRelease(cfg.WorkDir, args[0], opts)
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Release updated")
				fmt.Fprintln(cmd.OutOrStdout())
				printReleaseDetails(cmd.OutOrStdout(), result.Data)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&body, "body", "", "Updated release body")
	cmd.Flags().StringVarP(&tag, "tag", "t", "", "Updated git tag")
	cmd.Flags().StringVarP(&version, "version", "v", "", "Updated version")
	cmd.Flags().StringVar(&sbom, "sbom", "", "Updated SBOM filename")
	cmd.Flags().StringVar(&artifactsStr, "artifacts", "", "Comma-separated artifact filenames")
	cmd.Flags().StringVar(&artifactURL, "artifact-url", "", "Base URL for externally hosted artifacts")
	cmd.Flags().StringVar(&checksums, "checksums", "", "Checksums filename, such as SHA256SUMS")
	cmd.Flags().StringVar(&signedBy, "signed-by", "", "Key fingerprint for release signature")
	cmd.Flags().StringVarP(&labelsStr, "labels", "l", "", "Comma-separated labels, replacing any set")
	cmd.Flags().BoolVar(&prerelease, "prerelease", false, "Mark as pre-release; --prerelease=false clears it")

	return cmd
}

// newReleaseRetractCmd builds the command that retracts a release.
func newReleaseRetractCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "retract <release-ref>",
		Short: "Retract a release",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "error", err)
			}

			result := release.RetractRelease(cfg.WorkDir, args[0])
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]bool{"retracted": true})
			} else {
				PrintSuccess(cmd, "Release retracted")
			}
			return nil
		},
	}
}

// --- artifacts ---

func newReleaseArtifactsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "artifacts",
		Short: "Manage release artifacts",
	}
	cmd.AddCommand(
		newReleaseArtifactsRecordCmd(),
		newReleaseArtifactsListCmd(),
		newReleaseArtifactsExportCmd(),
		newReleaseArtifactsPushCmd(),
	)
	return cmd
}

// newReleaseArtifactsRecordCmd builds the command that records artifacts on a release.
func newReleaseArtifactsRecordCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "record <version> <file...>",
		Short: "Record artifacts on a release",
		Long: `Commit the given files and their SHA-256 checksums to the release's local
artifact ref (refs/gitmsg/release/<version>/artifacts). Nothing is uploaded:
"record" writes the release's artifact record, "release artifacts push"
uploads the files to the s3 push remote's bucket.`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			version := args[0]
			filePaths := args[1:]
			result := release.AddArtifacts(cfg.WorkDir, version, filePaths)
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}
			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, fmt.Sprintf("Recorded %d artifact(s) on %s", len(result.Data.Files), version))
				for _, f := range result.Data.Files {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s  %s  %d bytes\n", f.SHA256[:12], f.Filename, f.Size)
				}
			}
			return nil
		},
	}
}

// newReleaseArtifactsListCmd builds the command that lists a release's artifacts.
func newReleaseArtifactsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <version>",
		Short: "List artifacts for a release",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "error", err)
			}
			result := release.ListArtifacts(cfg.WorkDir, args[0])
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}
			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				if len(result.Data) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No artifacts found")
					return nil
				}
				for _, f := range result.Data {
					if f.SHA256 == "" {
						// Externally hosted artifact (artifact-url fallback): the
						// record carries only filenames.
						fmt.Fprintln(cmd.OutOrStdout(), f.Filename)
						continue
					}
					sha := f.SHA256
					if len(sha) > 12 {
						sha = sha[:12]
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %d bytes\n", sha, f.Filename, f.Size)
				}
			}
			return nil
		},
	}
}

// newReleaseArtifactsExportCmd builds the command that exports artifacts to disk.
func newReleaseArtifactsExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export <version> [filename...]",
		Short: "Export artifacts to downloads directory",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "error", err)
			}
			version := args[0]
			repoURL := gitmsg.ResolveRepoURL(cfg.WorkDir)
			destDir := git.DownloadsDir()
			filenames := args[1:]
			if len(filenames) == 0 {
				res := release.ListArtifacts(cfg.WorkDir, version)
				if !res.Success {
					PrintError(cmd, res.Error.Text())
					return exit(ExitError)
				}
				for _, info := range res.Data {
					filenames = append(filenames, info.Filename)
				}
				if len(filenames) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No artifacts found")
					return nil
				}
			}
			for _, filename := range filenames {
				destPath := filepath.Join(destDir, filename)
				res := release.ExportArtifact(cfg.WorkDir, repoURL, version, filename, destPath)
				if !res.Success {
					PrintError(cmd, fmt.Sprintf("%s: %s", filename, res.Error.Text()))
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Saved %s → %s\n", filename, res.Data)
			}
			return nil
		},
	}
}

// printReleaseLine prints one release as a list row.
func printReleaseLine(out io.Writer, rel release.Release) {
	icon := "⏏"
	if rel.Prerelease {
		icon = "◇"
	}

	versionStr := rel.Version
	if versionStr == "" {
		versionStr = rel.Tag
	}
	if versionStr == "" {
		versionStr = "(unversioned)"
	}

	dateStr := rel.Timestamp.Format("2006-01-02")
	fmt.Fprintf(out, "%s %s  %s  %s\n", icon, versionStr, rel.Subject, dateStr)
}

// printReleaseDetails prints a release's fields and body.
func printReleaseDetails(out io.Writer, rel release.Release) {
	fmt.Fprintf(out, "Release: %s\n", rel.ID)

	if rel.Version != "" {
		fmt.Fprintf(out, "Version: %s\n", rel.Version)
	}
	if rel.Tag != "" {
		fmt.Fprintf(out, "Tag: %s\n", rel.Tag)
	}
	if rel.Prerelease {
		fmt.Fprintln(out, "Pre-release: yes")
	}

	authorName, authorEmail, created := ResolveDisplayIdentity(rel.Author.Name, rel.Author.Email, rel.Timestamp, rel.Origin)
	fmt.Fprintf(out, "Author: %s\n", FormatAuthorWithVerification(authorName, authorEmail, rel.Repository, protocol.ParseRef(rel.ID).Value))
	fmt.Fprintf(out, "Created: %s\n", created.Format(time.RFC3339))

	if len(rel.Labels) > 0 {
		fmt.Fprintf(out, "Labels: %s\n", strings.Join(rel.Labels, ", "))
	}

	if len(rel.Artifacts) > 0 {
		fmt.Fprintf(out, "Artifacts: %s\n", strings.Join(rel.Artifacts, ", "))
	}
	if rel.ArtifactURL != "" {
		fmt.Fprintf(out, "Artifact URL: %s\n", rel.ArtifactURL)
	}
	if rel.Checksums != "" {
		fmt.Fprintf(out, "Checksums: %s\n", rel.Checksums)
	}
	if rel.SBOM != "" {
		sbomLine := rel.SBOM
		if rel.Version != "" {
			repoURL := rel.Repository
			if repoURL == "" {
				repoURL = "."
			}
			if summary, err := release.GetSBOMSummary(".", repoURL, rel.Version, rel.SBOM, rel.ArtifactURL); err == nil {
				sbomLine += fmt.Sprintf(" (%s) · %d packages", summary.Format, summary.Packages)
				fmt.Fprintf(out, "SBOM: %s\n", sbomLine)
				if summary.Generator != "" {
					fmt.Fprintf(out, "  Generator: %s\n", summary.Generator)
				}
				if len(summary.Licenses) > 0 {
					entries := release.SortedLicenses(summary.Licenses)
					parts := make([]string, 0, len(entries))
					for _, e := range entries {
						parts = append(parts, fmt.Sprintf("%d %s", e.Count, e.Name))
					}
					fmt.Fprintf(out, "  Licenses: %s\n", strings.Join(parts, " · "))
				}
			} else {
				fmt.Fprintf(out, "SBOM: %s\n", sbomLine)
			}
		} else {
			fmt.Fprintf(out, "SBOM: %s\n", sbomLine)
		}
	}
	if rel.SignedBy != "" {
		fmt.Fprintf(out, "Signed by: %s\n", rel.SignedBy)
	}
	if rel.IsEdited {
		fmt.Fprintln(out, "(edited)")
	}

	if rel.Body != "" {
		fmt.Fprintln(out)
		fmt.Fprintln(out, rel.Body)
	}
}

// newReleaseSBOMCmd builds the command that shows a release's SBOM.
func newReleaseSBOMCmd() *cobra.Command {
	var raw bool

	cmd := &cobra.Command{
		Use:   "sbom <release-ref>",
		Short: "Show SBOM details for a release",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "error", err)
			}

			res := release.GetSingleRelease(args[0])
			if !res.Success {
				PrintError(cmd, res.Error.Text())
				return exit(ExitError)
			}
			rel := res.Data

			if rel.SBOM == "" {
				PrintError(cmd, "release has no SBOM")
				return exit(ExitError)
			}
			if rel.Version == "" {
				PrintError(cmd, "release has no version: set it with gitsocial release edit --version")
				return exit(ExitError)
			}

			if raw {
				rawRes := release.GetSBOMRaw(cfg.WorkDir, rel.Version, rel.SBOM)
				if !rawRes.Success {
					PrintError(cmd, rawRes.Error.Text())
					return exit(ExitError)
				}
				if cfg.JSONOutput {
					if err := PrintJSON(cmd, json.RawMessage(rawRes.Data)); err != nil {
						return err
					}
				} else {
					fmt.Fprint(cmd.OutOrStdout(), rawRes.Data)
				}
				return nil
			}

			repoURL := rel.Repository
			if repoURL == "" {
				repoURL = "."
			}
			summary, err := release.GetSBOMSummary(cfg.WorkDir, repoURL, rel.Version, rel.SBOM, rel.ArtifactURL)
			if err != nil {
				PrintError(cmd, err.Error())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, summary)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "SBOM: %s\n", rel.SBOM)
			fmt.Fprintf(cmd.OutOrStdout(), "Format: %s\n", summary.Format)
			fmt.Fprintf(cmd.OutOrStdout(), "Packages: %d\n", summary.Packages)
			if summary.Generator != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Generator: %s\n", summary.Generator)
			}
			if len(summary.Licenses) > 0 {
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprintln(cmd.OutOrStdout(), "Licenses:")
				entries := release.SortedLicenses(summary.Licenses)
				for _, e := range entries {
					fmt.Fprintf(cmd.OutOrStdout(), "  %3d  %s\n", e.Count, e.Name)
				}
			}
			if len(summary.Items) > 0 {
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprintln(cmd.OutOrStdout(), "Packages:")
				nameW, verW := 20, 10
				for _, p := range summary.Items {
					if len(p.Name) > nameW {
						nameW = len(p.Name)
					}
					if len(p.Version) > verW {
						verW = len(p.Version)
					}
				}
				if nameW > 40 {
					nameW = 40
				}
				if verW > 20 {
					verW = 20
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %-*s  %-*s  %s\n", nameW, "NAME", verW, "VERSION", "LICENSE")
				for _, p := range summary.Items {
					name := p.Name
					if len(name) > nameW {
						name = name[:nameW-1] + "…"
					}
					fmt.Fprintf(cmd.OutOrStdout(), "  %-*s  %-*s  %s\n", nameW, name, verW, p.Version, p.License)
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&raw, "raw", false, "Dump raw SBOM JSON")
	return cmd
}
