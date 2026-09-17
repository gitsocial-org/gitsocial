// status.go - CLI command for showing GitMsg status
package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

type statusData struct {
	Repository string             `json:"repository"`
	Thin       *thinStatus        `json:"thin,omitempty"`
	Cache      *cacheStatus       `json:"cache,omitempty"`
	Social     *social.StatusData `json:"social,omitempty"`
}

// thinStatus describes a thin fork push relationship: the bucket this repo
// publishes to carries only its own objects, and reading its code needs the
// upstream below.
type thinStatus struct {
	Remote   string `json:"remote"`
	Upstream string `json:"upstream"`
	Pins     int    `json:"pins"`
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
		Short: "Show GitSocial status for this repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			status := getStatusData(cfg)

			if cfg.JSONOutput {
				return PrintJSON(cmd, status)
			} else {
				printStatus(cmd.OutOrStdout(), status)
			}
			return nil
		},
	}
}

// getStatusData collects status information from cache and social extension.
func getStatusData(cfg *Config) statusData {
	status := statusData{
		Repository: getRemoteURL(cfg.WorkDir),
		Thin:       getThinStatus(cfg.WorkDir),
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

// getThinStatus reports the resolved push remote's thin fork relationship, so
// status says out loud that the published bucket's history is incomplete without
// upstream. nil when that remote is not thin. The pin count lives on the bucket
// (one GET); a read failure only omits it.
func getThinStatus(workdir string) *thinStatus {
	remote := git.PushRemote(workdir)
	if remote == "" {
		return nil
	}
	switch git.GetGitConfig(workdir, "remote."+remote+"."+objstore.ThinConfigKey) {
	case "true", "yes", "on", "1": // git's true spellings, as the helper reads them
	default:
		return nil
	}
	st := &thinStatus{Remote: remote, Upstream: git.GetGitConfig(workdir, "remote."+remote+"."+objstore.UpstreamConfigKey)}
	if url := git.RemoteURL(workdir, remote); strings.HasPrefix(url, "s3://") {
		if upstream, pins, err := objstore.ThinUpstream(url, objstore.HelperEnvFromOS()); err == nil && upstream != "" {
			st.Upstream, st.Pins = upstream, pins
		}
	}
	return st
}

// printStatus writes the status data to out.
func printStatus(out io.Writer, s statusData) {
	fmt.Fprintf(out, "Repository: %s\n", s.Repository)
	if s.Thin != nil {
		fmt.Fprintf(out, "Thin fork of %s, pinned at %d tips\n", s.Thin.Upstream, s.Thin.Pins)
	}

	if s.Cache != nil {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Cache:")
		fmt.Fprintf(out, "  Location: %s\n", s.Cache.Location)
		fmt.Fprintf(out, "  Size: %s\n", formatBytes(s.Cache.SizeBytes))
		fmt.Fprintf(out, "  Items: %d\n", s.Cache.Items)
		fmt.Fprintf(out, "  Repositories: %d\n", s.Cache.Repositories)
	}

	if s.Social != nil {
		fmt.Fprintln(out)
		printSocialSection(out, s.Social)
	}
}

// printSocialSection prints the social extension portion of status.
func printSocialSection(out io.Writer, s *social.StatusData) {
	fmt.Fprintln(out, "Social:")
	fmt.Fprintf(out, "  Branch: %s\n", s.Branch)

	if s.Unpushed != nil && (s.Unpushed.Posts > 0 || s.Unpushed.Lists > 0) {
		var parts []string
		if s.Unpushed.Posts > 0 {
			parts = append(parts, fmt.Sprintf("%d posts", s.Unpushed.Posts))
		}
		if s.Unpushed.Lists > 0 {
			parts = append(parts, fmt.Sprintf("%d lists", s.Unpushed.Lists))
		}
		fmt.Fprintf(out, "  ⇡ Unpushed: %s\n", joinParts(parts))
	}

	if !s.LastFetch.IsZero() {
		fmt.Fprintf(out, "  Fetched: %s\n", social.FormatRelativeTime(s.LastFetch))
	}

	if len(s.Lists) > 0 {
		fmt.Fprintf(out, "  Lists (%d):\n", len(s.Lists))
		for _, list := range s.Lists {
			fmt.Fprintf(out, "    - %s (%d repos)\n", list.ID, list.Repos)
		}
	} else {
		fmt.Fprintln(out, "  Lists: none")
	}

	fmt.Fprintf(out, "  Items: %d (%d list, %d workspace)\n", s.Items, s.FromLists, s.FromWorkspace)
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
