// release.go - CLI commands for the release extension
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/release"
)

const releaseExt = "release"

func init() {
	RegisterExtension(ExtensionRegistration{
		Use:   "release",
		Short: "Release management (versions, artifacts, changelogs)",
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

func newReleaseStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show release extension status",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := release.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "release", "error", err)
			}
			relConfig := release.GetReleaseConfig(cfg.WorkDir)

			branch := relConfig.Branch
			if branch == "" {
				branch = "(not configured)"
			}

			res := release.GetReleases("", "", "", 0)
			count := 0
			if res.Success {
				count = len(res.Data)
			}

			if cfg.JSONOutput {
				PrintJSON(map[string]interface{}{
					"branch":   branch,
					"releases": count,
				})
			} else {
				fmt.Println("Release:")
				fmt.Printf("  Branch: %s\n", branch)
				fmt.Printf("  Releases: %d\n", count)
			}
		},
	}
}

func newReleaseInitCmd() *cobra.Command {
	var branch string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize GitRelease in the current repository",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if branch == "" {
				branch = "gitmsg/release"
			}

			relConfig := release.ReleaseConfig{
				Version: "0.1.0",
				Branch:  branch,
			}
			if err := release.SaveReleaseConfig(cfg.WorkDir, relConfig); err != nil {
				PrintError(cmd, "failed to initialize: "+err.Error())
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(map[string]string{
					"status": "initialized",
					"branch": branch,
				})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("GitRelease initialized (branch: %s)", branch))
			}
		},
	}

	cmd.Flags().StringVarP(&branch, "branch", "b", "gitmsg/release", "Branch to use for release content")
	return cmd
}

func newReleaseCreateCmd() *cobra.Command {
	var tag string
	var version string
	var prerelease bool
	var artifactsStr string
	var artifactURL string
	var checksums string
	var signedBy string
	var sbom string
	var allowDuplicate bool

	cmd := &cobra.Command{
		Use:   "create <subject>",
		Short: "Create a new release",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			subject := args[0]
			body := ""

			if subject == "-" {
				scanner := bufio.NewScanner(os.Stdin)
				var lines []string
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
				}
				if err := scanner.Err(); err != nil {
					PrintError(cmd, "failed to read from stdin: "+err.Error())
					os.Exit(ExitError)
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
				os.Exit(ExitInvalidArgs)
			}

			opts := release.CreateReleaseOptions{
				Tag:            tag,
				Version:        version,
				Prerelease:     prerelease,
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
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Release created")
				fmt.Println()
				printReleaseDetails(result.Data)
			}
		},
	}

	cmd.Flags().StringVarP(&tag, "tag", "t", "", "Git tag name (e.g., v1.0.0)")
	cmd.Flags().StringVarP(&version, "version", "v", "", "Semver version (e.g., 1.0.0)")
	cmd.Flags().BoolVar(&prerelease, "prerelease", false, "Mark as pre-release")
	cmd.Flags().StringVar(&artifactsStr, "artifacts", "", "Artifact filenames (comma-separated)")
	cmd.Flags().StringVar(&artifactURL, "artifact-url", "", "Base URL for externally hosted artifacts")
	cmd.Flags().StringVar(&checksums, "checksums", "", "Checksums filename (e.g., SHA256SUMS)")
	cmd.Flags().StringVar(&signedBy, "signed-by", "", "Key fingerprint for release signature")
	cmd.Flags().StringVar(&sbom, "sbom", "", "SBOM filename (e.g., sbom.spdx.json)")
	cmd.Flags().BoolVar(&allowDuplicate, "allow-duplicate", false, "Allow creating a release with a tag that already exists")

	return cmd
}

func newReleaseListCmd() *cobra.Command {
	var limit int
	var repoURL string
	var branch string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List releases",
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			if repoURL != "" {
				fetchResult := release.FetchRepository(cfg.CacheDir, repoURL, branch)
				if !fetchResult.Success {
					PrintError(cmd, fetchResult.Error.Message)
					os.Exit(ExitError)
				}
			} else {
				if !EnsureGitRepo(cmd) {
					os.Exit(ExitNotRepo)
				}
				if err := release.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
					slog.Debug("sync workspace", "ext", "release", "error", err)
				}
			}

			result := release.GetReleases(repoURL, branch, "", limit)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				if len(result.Data) == 0 {
					fmt.Println("No releases found")
					return
				}
				for _, rel := range result.Data {
					printReleaseLine(rel)
				}
			}
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum number of releases")
	cmd.Flags().StringVarP(&repoURL, "repo", "r", "", "Repository URL (default: current workspace)")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Branch name (default: configured release branch)")

	return cmd
}

func newReleaseShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <release-ref>",
		Short: "Show release details",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := release.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "release", "error", err)
			}

			result := release.GetSingleRelease(args[0])
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				printReleaseDetails(result.Data)
			}
		},
	}
}

func newReleaseEditCmd() *cobra.Command {
	var body string
	var tag string
	var version string
	var sbom string
	var artifactsStr string
	var artifactURL string
	var checksums string
	var signedBy string

	cmd := &cobra.Command{
		Use:   "edit <release-ref>",
		Short: "Edit a release",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := release.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "release", "error", err)
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
				a := splitCSV(artifactsStr)
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

			result := release.EditRelease(cfg.WorkDir, args[0], opts)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Release updated")
				fmt.Println()
				printReleaseDetails(result.Data)
			}
		},
	}

	cmd.Flags().StringVar(&body, "body", "", "Updated release body")
	cmd.Flags().StringVarP(&tag, "tag", "t", "", "Updated git tag")
	cmd.Flags().StringVarP(&version, "version", "v", "", "Updated version")
	cmd.Flags().StringVar(&sbom, "sbom", "", "Updated SBOM filename")
	cmd.Flags().StringVar(&artifactsStr, "artifacts", "", "Artifact filenames (comma-separated)")
	cmd.Flags().StringVar(&artifactURL, "artifact-url", "", "Base URL for externally hosted artifacts")
	cmd.Flags().StringVar(&checksums, "checksums", "", "Checksums filename (e.g., SHA256SUMS)")
	cmd.Flags().StringVar(&signedBy, "signed-by", "", "Key fingerprint for release signature")

	return cmd
}

func newReleaseRetractCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "retract <release-ref>",
		Short: "Retract a release",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := release.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "release", "error", err)
			}

			result := release.RetractRelease(cfg.WorkDir, args[0])
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(map[string]bool{"retracted": true})
			} else {
				PrintSuccess(cmd, "Release retracted")
			}
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
		newReleaseArtifactsAddCmd(),
		newReleaseArtifactsListCmd(),
		newReleaseArtifactsExportCmd(),
	)
	return cmd
}

func newReleaseArtifactsAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <version> <file...>",
		Short: "Add artifacts to a release",
		Args:  cobra.MinimumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			version := args[0]
			filePaths := args[1:]
			result := release.AddArtifacts(cfg.WorkDir, version, filePaths)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, fmt.Sprintf("Added %d artifact(s) to %s", len(result.Data.Files), version))
				for _, f := range result.Data.Files {
					fmt.Printf("  %s  %s  %d bytes\n", f.SHA256[:12], f.Filename, f.Size)
				}
			}
		},
	}
}

func newReleaseArtifactsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <version>",
		Short: "List artifacts for a release",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			result := release.ListArtifacts(cfg.WorkDir, args[0])
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				if len(result.Data) == 0 {
					fmt.Println("No artifacts found")
					return
				}
				for _, f := range result.Data {
					sha := f.SHA256
					if len(sha) > 12 {
						sha = sha[:12]
					}
					fmt.Printf("%s  %s  %d bytes\n", sha, f.Filename, f.Size)
				}
			}
		},
	}
}

func newReleaseArtifactsExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export <version> [filename...]",
		Short: "Export artifacts to downloads directory",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			version := args[0]
			repoURL := gitmsg.ResolveRepoURL(cfg.WorkDir)
			destDir := git.DownloadsDir()
			filenames := args[1:]
			if len(filenames) == 0 {
				res := release.ListArtifacts(cfg.WorkDir, version)
				if !res.Success {
					PrintError(cmd, res.Error.Message)
					os.Exit(ExitError)
				}
				for _, info := range res.Data {
					filenames = append(filenames, info.Filename)
				}
				if len(filenames) == 0 {
					fmt.Println("No artifacts found")
					return
				}
			}
			for _, filename := range filenames {
				destPath := filepath.Join(destDir, filename)
				res := release.ExportArtifact(cfg.WorkDir, repoURL, version, filename, destPath)
				if !res.Success {
					PrintError(cmd, fmt.Sprintf("%s: %s", filename, res.Error.Message))
					continue
				}
				fmt.Printf("Saved %s → %s\n", filename, res.Data)
			}
		},
	}
}

func printReleaseLine(rel release.Release) {
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
	fmt.Printf("%s %s  %s  %s\n", icon, versionStr, rel.Subject, dateStr)
}

func printReleaseDetails(rel release.Release) {
	fmt.Printf("Release: %s\n", rel.ID)

	if rel.Version != "" {
		fmt.Printf("Version: %s\n", rel.Version)
	}
	if rel.Tag != "" {
		fmt.Printf("Tag: %s\n", rel.Tag)
	}
	if rel.Prerelease {
		fmt.Println("Pre-release: yes")
	}

	fmt.Printf("Author: %s\n", FormatAuthorWithVerification(rel.Author.Name, rel.Author.Email, rel.Repository, protocol.ParseRef(rel.ID).Value))
	fmt.Printf("Created: %s\n", rel.Timestamp.Format(time.RFC3339))

	if len(rel.Artifacts) > 0 {
		fmt.Printf("Artifacts: %s\n", strings.Join(rel.Artifacts, ", "))
	}
	if rel.ArtifactURL != "" {
		fmt.Printf("Artifact URL: %s\n", rel.ArtifactURL)
	}
	if rel.Checksums != "" {
		fmt.Printf("Checksums: %s\n", rel.Checksums)
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
				fmt.Printf("SBOM: %s\n", sbomLine)
				if summary.Generator != "" {
					fmt.Printf("  Generator: %s\n", summary.Generator)
				}
				if len(summary.Licenses) > 0 {
					entries := release.SortedLicenses(summary.Licenses)
					parts := make([]string, 0, len(entries))
					for _, e := range entries {
						parts = append(parts, fmt.Sprintf("%d %s", e.Count, e.Name))
					}
					fmt.Printf("  Licenses: %s\n", strings.Join(parts, " · "))
				}
			} else {
				fmt.Printf("SBOM: %s\n", sbomLine)
			}
		} else {
			fmt.Printf("SBOM: %s\n", sbomLine)
		}
	}
	if rel.SignedBy != "" {
		fmt.Printf("Signed by: %s\n", rel.SignedBy)
	}
	if rel.IsEdited {
		fmt.Println("(edited)")
	}

	if rel.Body != "" {
		fmt.Println()
		fmt.Println(rel.Body)
	}
}

func newReleaseSBOMCmd() *cobra.Command {
	var raw bool

	cmd := &cobra.Command{
		Use:   "sbom <release-ref>",
		Short: "Show SBOM details for a release",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			if err := release.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "release", "error", err)
			}

			res := release.GetSingleRelease(args[0])
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(ExitError)
			}
			rel := res.Data

			if rel.SBOM == "" {
				PrintError(cmd, "release has no SBOM")
				os.Exit(ExitError)
			}
			if rel.Version == "" {
				PrintError(cmd, "release has no version (required for artifact ref)")
				os.Exit(ExitError)
			}

			if raw {
				rawRes := release.GetSBOMRaw(cfg.WorkDir, rel.Version, rel.SBOM)
				if !rawRes.Success {
					PrintError(cmd, rawRes.Error.Message)
					os.Exit(ExitError)
				}
				if cfg.JSONOutput {
					PrintJSON(json.RawMessage(rawRes.Data))
				} else {
					fmt.Print(rawRes.Data)
				}
				return
			}

			repoURL := rel.Repository
			if repoURL == "" {
				repoURL = "."
			}
			summary, err := release.GetSBOMSummary(cfg.WorkDir, repoURL, rel.Version, rel.SBOM, rel.ArtifactURL)
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(summary)
				return
			}

			fmt.Printf("SBOM: %s\n", rel.SBOM)
			fmt.Printf("Format: %s\n", summary.Format)
			fmt.Printf("Packages: %d\n", summary.Packages)
			if summary.Generator != "" {
				fmt.Printf("Generator: %s\n", summary.Generator)
			}
			if len(summary.Licenses) > 0 {
				fmt.Println()
				fmt.Println("Licenses:")
				entries := release.SortedLicenses(summary.Licenses)
				for _, e := range entries {
					fmt.Printf("  %3d  %s\n", e.Count, e.Name)
				}
			}
			if len(summary.Items) > 0 {
				fmt.Println()
				fmt.Println("Packages:")
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
				fmt.Printf("  %-*s  %-*s  %s\n", nameW, "NAME", verW, "VERSION", "LICENSE")
				for _, p := range summary.Items {
					name := p.Name
					if len(name) > nameW {
						name = name[:nameW-1] + "…"
					}
					fmt.Printf("  %-*s  %-*s  %s\n", nameW, name, verW, p.Version, p.License)
				}
			}
		},
	}

	cmd.Flags().BoolVar(&raw, "raw", false, "Dump raw SBOM JSON")
	return cmd
}
