// show.go - Top-level show command that auto-detects extension
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/extensions/release"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/extensions/social"
)

// newShowCmd creates the top-level show command that auto-detects the extension.
func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <ref>",
		Short: "Show item details (auto-detects extension)",
		Long: `Show full details for any GitSocial item by its ref.

Automatically detects whether the ref is an issue, pull request, release,
or social post and displays the appropriate detail view.

Examples:
  gitsocial show #commit:abc123
  gitsocial show https://github.com/user/repo#commit:abc123`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			ref := args[0]
			workspaceURL := gitmsg.ResolveRepoURL(cfg.WorkDir)

			// Strip #commit: prefix for compatibility — extension getters
			// handle bare hashes better (prefix scan fallback).
			bareRef := ref
			for _, prefix := range []string{"#commit:", "#tag:", "#branch:"} {
				if len(ref) > len(prefix) && ref[:len(prefix)] == prefix {
					bareRef = ref[len(prefix):]
					break
				}
			}

			// Fast dispatch: detect which extension owns this hash via raw table lookup,
			// then call only the matching getter instead of trying all 4 sequentially.
			if hits, err := cache.DetectExtension(bareRef); err == nil && len(hits) > 0 {
				if showByExtension(cmd, cfg, bareRef, workspaceURL, hits[0]) {
					return
				}
			}

			// Fallback: try each extension (handles refs that DetectExtension can't resolve,
			// e.g. full URL refs or non-hash ref types).
			if showReview(cmd, cfg, bareRef) {
				return
			}
			if showPM(cmd, cfg, bareRef, workspaceURL) {
				return
			}
			if showRelease(cmd, cfg, bareRef) {
				return
			}
			if showSocial(cmd, cfg, bareRef, workspaceURL) {
				return
			}

			slog.Debug("show: no extension matched", "ref", ref)
			PrintError(cmd, "item not found: "+ref)
			os.Exit(ExitError)
		},
	}
}

// showByExtension dispatches to the correct extension getter based on a DetectExtension hit.
func showByExtension(cmd *cobra.Command, cfg *Config, ref, workspaceURL string, hit cache.ExtensionHit) bool {
	switch hit.Extension {
	case "review":
		return showReview(cmd, cfg, ref)
	case "pm":
		return showPM(cmd, cfg, ref, workspaceURL)
	case "release":
		return showRelease(cmd, cfg, ref)
	case "social":
		return showSocial(cmd, cfg, ref, workspaceURL)
	}
	return false
}

// showReview displays a pull request if the ref matches.
func showReview(_ *cobra.Command, cfg *Config, ref string) bool {
	prResult := review.GetPR(ref)
	if !prResult.Success {
		return false
	}
	pr := prResult.Data
	pr.ReviewSummary = review.GetReviewSummary(pr.Repository, extractHash(pr.ID), pr.Branch, pr.Reviewers)
	if cfg.JSONOutput {
		PrintJSON(pr)
	} else {
		printPRDetails(cfg.WorkDir, pr)
	}
	return true
}

// showPM displays a PM item (issue/milestone/sprint) if the ref matches.
func showPM(_ *cobra.Command, cfg *Config, ref, workspaceURL string) bool {
	item, err := pm.GetPMItemByRef(ref, workspaceURL)
	if err != nil {
		return false
	}
	issue := pm.PMItemToIssue(*item)
	if cfg.JSONOutput {
		PrintJSON(issue)
	} else {
		printIssueDetails(issue)
	}
	return true
}

// showRelease displays a release if the ref matches.
func showRelease(_ *cobra.Command, cfg *Config, ref string) bool {
	relResult := release.GetSingleRelease(ref)
	if !relResult.Success {
		return false
	}
	if cfg.JSONOutput {
		PrintJSON(relResult.Data)
	} else {
		printReleaseDetails(relResult.Data)
	}
	return true
}

// showSocial displays a social post if the ref matches.
func showSocial(_ *cobra.Command, cfg *Config, ref, workspaceURL string) bool {
	item, err := social.GetSocialItemByRef(ref, workspaceURL)
	if err != nil {
		return false
	}
	post := social.SocialItemToPost(*item)
	if cfg.JSONOutput {
		PrintJSON(post)
	} else {
		fmt.Println(social.FormatPost(post))
	}
	return true
}
