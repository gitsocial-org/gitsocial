// status.go - CLI command for showing GitMsg status
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/extensions/social"
)

type statusData struct {
	Repository string             `json:"repository"`
	Cache      *cacheStatus       `json:"cache,omitempty"`
	Social     *social.StatusData `json:"social,omitempty"`
}

type cacheStatus struct {
	Location     string `json:"location"`
	SizeBytes    int64  `json:"sizeBytes"`
	Items        int    `json:"items"`
	Repositories int    `json:"repositories"`
}

// newStatusCmd creates the command to show GitMsg status for the current repository.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show GitSocial status for current repository",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			status := getStatusData(cfg)

			if cfg.JSONOutput {
				PrintJSON(status)
			} else {
				printStatus(status)
			}
		},
	}
}

// getStatusData collects status information from cache and social extension.
func getStatusData(cfg *Config) statusData {
	status := statusData{
		Repository: getRemoteURL(cfg.WorkDir),
	}

	if cacheStats, err := cache.GetStatsLite(cfg.CacheDir); err == nil && cacheStats != nil {
		status.Cache = &cacheStatus{
			Location:     cacheStats.Location,
			SizeBytes:    cacheStats.TotalBytes,
			Items:        cacheStats.Items,
			Repositories: cacheStats.Repositories,
		}
	}

	if gitmsg.IsExtInitialized(cfg.WorkDir, "social") {
		result := social.Status(cfg.WorkDir, cfg.CacheDir)
		if result.Success {
			status.Social = &result.Data
		}
	}

	return status
}

// getRemoteURL returns the origin remote URL or a placeholder if not set.
func getRemoteURL(workdir string) string {
	result, err := git.ExecGit(workdir, []string{"remote", "get-url", "origin"})
	if err != nil {
		return "(no remote)"
	}
	return result.Stdout
}

// printStatus prints the status data to stdout.
func printStatus(s statusData) {
	fmt.Printf("Repository: %s\n", s.Repository)

	if s.Cache != nil {
		fmt.Println()
		fmt.Println("Cache:")
		fmt.Printf("  Location: %s\n", s.Cache.Location)
		fmt.Printf("  Size: %s\n", formatBytes(s.Cache.SizeBytes))
		fmt.Printf("  Items: %d\n", s.Cache.Items)
		fmt.Printf("  Repositories: %d\n", s.Cache.Repositories)
	}

	if s.Social != nil {
		fmt.Println()
		printSocialSection(s.Social)
	}
}

// printSocialSection prints the social extension portion of status.
func printSocialSection(s *social.StatusData) {
	fmt.Println("Social:")
	fmt.Printf("  Branch: %s\n", s.Branch)

	if s.Unpushed != nil && (s.Unpushed.Posts > 0 || s.Unpushed.Lists > 0) {
		var parts []string
		if s.Unpushed.Posts > 0 {
			parts = append(parts, fmt.Sprintf("%d posts", s.Unpushed.Posts))
		}
		if s.Unpushed.Lists > 0 {
			parts = append(parts, fmt.Sprintf("%d lists", s.Unpushed.Lists))
		}
		fmt.Printf("  ⇡ Unpushed: %s\n", joinParts(parts))
	}

	if !s.LastFetch.IsZero() {
		fmt.Printf("  Fetched: %s\n", social.FormatRelativeTime(s.LastFetch))
	}

	if len(s.Lists) > 0 {
		fmt.Printf("  Lists (%d):\n", len(s.Lists))
		for _, list := range s.Lists {
			fmt.Printf("    - %s (%d repos)\n", list.ID, list.Repos)
		}
	} else {
		fmt.Println("  Lists: none")
	}

	fmt.Printf("  Items: %d (%d list, %d workspace)\n", s.Items, s.FromLists, s.FromWorkspace)
}

// joinParts joins string parts with commas.
func joinParts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += ", " + parts[i]
	}
	return result
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
