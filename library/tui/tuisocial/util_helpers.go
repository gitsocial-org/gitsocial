// util_helpers.go - Helper functions for follow status and list indicators
package tuisocial

import (
	"fmt"
	"strings"

	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/social"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// FollowStatus represents the follow relationship between a repo and the workspace
type FollowStatus int

const (
	FollowStatusNone       FollowStatus = iota // Neither follows the other
	FollowStatusFollowsYou                     // They follow us but we don't follow them
	FollowStatusFollowed                       // We follow them but they don't follow us
	FollowStatusMutual                         // Mutual follow (we follow them AND they follow us)
)

// GetFollowStatus determines the follow relationship for a repo
func GetFollowStatus(repoURL string, lists []social.List, followerSet map[string]bool) FollowStatus {
	normalizedURL := protocol.NormalizeURL(repoURL)
	theyFollowUs := followerSet[normalizedURL]
	weFollowThem := isRepoInAnyList(normalizedURL, lists)
	if weFollowThem && theyFollowUs {
		return FollowStatusMutual
	}
	if weFollowThem {
		return FollowStatusFollowed
	}
	if theyFollowUs {
		return FollowStatusFollowsYou
	}
	return FollowStatusNone
}

// isRepoInAnyList checks if a repo URL is in any of the given lists
func isRepoInAnyList(repoURL string, lists []social.List) bool {
	for _, list := range lists {
		for _, repo := range list.Repositories {
			id := protocol.ParseRepositoryID(repo)
			if id.Repository == repoURL {
				return true
			}
		}
	}
	return false
}

// GetListNamesForRepo returns the list names that contain a specific repo
func GetListNamesForRepo(repoURL string, lists []social.List, excludeListID string) []string {
	normalizedURL := protocol.NormalizeURL(repoURL)
	var names []string
	for _, list := range lists {
		if list.ID == excludeListID {
			continue
		}
		for _, repo := range list.Repositories {
			id := protocol.ParseRepositoryID(repo)
			if id.Repository == normalizedURL {
				names = append(names, list.Name)
				break
			}
		}
	}
	return names
}

// FormatListIndicator formats list names as "[list1, list2]" or "[+N more]" if too many
func FormatListIndicator(names []string, maxVisible int) string {
	if len(names) == 0 {
		return ""
	}
	if len(names) <= maxVisible {
		return "[" + strings.Join(names, ", ") + "]"
	}
	visible := names[:maxVisible]
	extra := len(names) - maxVisible
	return "[" + strings.Join(visible, ", ") + fmt.Sprintf(" +%d more]", extra)
}

// parseRepoInput splits "url [branch|*]" into URL, branch, and allBranches flag.
func parseRepoInput(raw string) (url, branch string, allBranches bool) {
	parts := strings.Fields(raw)
	if len(parts) < 2 {
		return raw, "", false
	}
	suffix := parts[len(parts)-1]
	base := strings.Join(parts[:len(parts)-1], " ")
	if suffix == "*" {
		return base, "", true
	}
	// Treat suffix as branch name if it doesn't look like a URL
	if !strings.Contains(suffix, "/") || strings.Contains(suffix, "://") {
		return raw, "", false
	}
	return base, suffix, false
}

// RenderFollowIndicator renders the follow status indicator with appropriate styling
// List names are shown in purple, status indicators in dim
func RenderFollowIndicator(status FollowStatus, listNames []string, selected bool) string {
	switch status {
	case FollowStatusMutual, FollowStatusFollowed:
		indicator := FormatListIndicator(listNames, 2)
		if indicator == "" {
			return ""
		}
		if selected {
			return tuicore.ListIndicatorSelected.Render(indicator)
		}
		return tuicore.ListIndicator.Render(indicator)
	case FollowStatusFollowsYou:
		if selected {
			return tuicore.DimSelected.Render("[follows you]")
		}
		return tuicore.Dim.Render("[follows you]")
	default:
		if selected {
			return tuicore.DimSelected.Render("[not followed]")
		}
		return tuicore.Dim.Render("[not followed]")
	}
}
