// show.go - Top-level show command that auto-detects extension
package main

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/extensions/release"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
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
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
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

			// The hash alone picks the extension getter, because the branch of a ref is historical.
			hash, _, _ := strings.Cut(bareRef, "@")
			if hits, err := cache.DetectExtension(hash); err == nil && len(hits) > 0 {
				if first, other := ambiguousHashes(hits); other != "" {
					PrintError(cmd, fmt.Sprintf("hash %q is ambiguous between %s and %s: use a longer prefix", bareRef, first, other))
					return exit(ExitError)
				}
				if shown, err := showByExtension(cmd, cfg, workspaceURL, hits[0]); shown || err != nil {
					return err
				}
			}

			// Fallback: try each extension (handles refs that DetectExtension can't resolve,
			// e.g. full URL refs or non-hash ref types).
			if shown, err := showReview(cmd, cfg, bareRef); shown || err != nil {
				return err
			}
			if shown, err := showPM(cmd, cfg, bareRef, workspaceURL); shown || err != nil {
				return err
			}
			if shown, err := showRelease(cmd, cfg, bareRef); shown || err != nil {
				return err
			}
			if shown, err := showSocial(cmd, cfg, bareRef, workspaceURL); shown || err != nil {
				return err
			}

			slog.Debug("show: no extension matched", "ref", ref)
			PrintError(cmd, "item not found: "+ref)
			return exit(ExitError)
		},
	}
}

// ambiguousHashes returns the hashes of two hits of different items, or "" as the second when every hit is one item or its edits.
func ambiguousHashes(hits []cache.ExtensionHit) (string, string) {
	canonical := func(hit cache.ExtensionHit) string {
		if _, hash, _, err := cache.ResolveToCanonical(hit.RepoURL, hit.Hash, hit.Branch); err == nil {
			return hash
		}
		return hit.Hash
	}
	first := canonical(hits[0])
	for _, hit := range hits[1:] {
		if canonical(hit) != first {
			return hits[0].Hash, hit.Hash
		}
	}
	return hits[0].Hash, ""
}

// showByExtension dispatches to the correct extension getter using the full PK from DetectExtension.
func showByExtension(cmd *cobra.Command, cfg *Config, workspaceURL string, hit cache.ExtensionHit) (bool, error) {
	switch hit.Extension {
	case "review":
		item, err := review.GetReviewItem(hit.RepoURL, hit.Hash, hit.Branch)
		if err != nil || item.Type != string(review.ItemTypePullRequest) {
			return false, nil
		}
		pr := review.ReviewItemToPullRequest(*item)
		pr.ReviewSummary = review.GetReviewSummary(pr.Repository, extractHash(pr.ID), pr.Branch, pr.Reviewers)
		if cfg.JSONOutput {
			return true, PrintJSON(cmd, pr)
		}
		printPRDetails(cmd.OutOrStdout(), cfg.WorkDir, pr)
		return true, nil
	case "pm":
		return showPM(cmd, cfg, "#commit:"+hit.Hash, workspaceURL)
	case "release":
		item, err := release.GetReleaseItem(hit.RepoURL, hit.Hash, hit.Branch)
		if err != nil {
			return false, nil
		}
		rel := release.ReleaseItemToRelease(*item)
		if cfg.JSONOutput {
			return true, PrintJSON(cmd, rel)
		}
		printReleaseDetails(cmd.OutOrStdout(), rel)
		return true, nil
	case "social":
		item, err := social.GetSocialItem(hit.RepoURL, hit.Hash, hit.Branch, workspaceURL)
		if err != nil {
			return false, nil
		}
		post := social.SocialItemToPost(*item)
		if cfg.JSONOutput {
			return true, PrintJSON(cmd, post)
		}
		fmt.Fprintln(cmd.OutOrStdout(), social.FormatPost(post))
		return true, nil
	}
	return false, nil
}

// showReview displays a pull request if the ref matches.
func showReview(cmd *cobra.Command, cfg *Config, ref string) (bool, error) {
	prResult := review.GetPR(ref)
	if !prResult.Success {
		return false, nil
	}
	pr := prResult.Data
	pr.ReviewSummary = review.GetReviewSummary(pr.Repository, extractHash(pr.ID), pr.Branch, pr.Reviewers)
	if cfg.JSONOutput {
		return true, PrintJSON(cmd, pr)
	}
	printPRDetails(cmd.OutOrStdout(), cfg.WorkDir, pr)
	return true, nil
}

// showPM displays a PM item (issue/milestone/sprint) if the ref matches.
func showPM(cmd *cobra.Command, cfg *Config, ref, workspaceURL string) (bool, error) {
	item, err := pm.GetPMItemByRef(ref, workspaceURL)
	if err != nil {
		return false, nil
	}
	issue := pm.PMItemToIssue(*item)
	if cfg.JSONOutput {
		return true, PrintJSON(cmd, issue)
	}
	printIssueDetails(cmd.OutOrStdout(), issue)
	return true, nil
}

// showRelease displays a release if the ref matches.
func showRelease(cmd *cobra.Command, cfg *Config, ref string) (bool, error) {
	relResult := release.GetSingleRelease(ref)
	if !relResult.Success {
		return false, nil
	}
	if cfg.JSONOutput {
		return true, PrintJSON(cmd, relResult.Data)
	}
	printReleaseDetails(cmd.OutOrStdout(), relResult.Data)
	return true, nil
}

// showSocial displays a social post if the ref matches.
func showSocial(cmd *cobra.Command, cfg *Config, ref, workspaceURL string) (bool, error) {
	item, err := social.GetSocialItemByRef(ref, workspaceURL)
	if err != nil {
		return false, nil
	}
	post := social.SocialItemToPost(*item)
	if cfg.JSONOutput {
		return true, PrintJSON(cmd, post)
	}
	fmt.Fprintln(cmd.OutOrStdout(), social.FormatPost(post))
	return true, nil
}
