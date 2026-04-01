// show.go - Top-level show command that auto-detects extension
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

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

			// Try review (PR)
			if prResult := review.GetPR(bareRef); prResult.Success {
				pr := prResult.Data
				pr.ReviewSummary = review.GetReviewSummary(pr.Repository, extractHash(pr.ID), pr.Branch, pr.Reviewers)
				if cfg.JSONOutput {
					PrintJSON(pr)
				} else {
					printPRDetails(cfg.WorkDir, pr)
				}
				return
			}

			// Try PM (issue/milestone/sprint)
			if item, err := pm.GetPMItemByRef(bareRef, workspaceURL); err == nil {
				issue := pm.PMItemToIssue(*item)
				if cfg.JSONOutput {
					PrintJSON(issue)
				} else {
					printIssueDetails(issue)
				}
				return
			}

			// Try release
			if relResult := release.GetSingleRelease(bareRef); relResult.Success {
				if cfg.JSONOutput {
					PrintJSON(relResult.Data)
				} else {
					printReleaseDetails(relResult.Data)
				}
				return
			}

			// Try social
			if item, err := social.GetSocialItemByRef(bareRef, workspaceURL); err == nil {
				post := social.SocialItemToPost(*item)
				if cfg.JSONOutput {
					PrintJSON(post)
				} else {
					fmt.Println(social.FormatPost(post))
				}
				return
			}

			slog.Debug("show: no extension matched", "ref", ref)
			PrintError(cmd, "item not found: "+ref)
			os.Exit(ExitError)
		},
	}
}
