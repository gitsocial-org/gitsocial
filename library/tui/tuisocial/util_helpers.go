// util_helpers.go - Helper functions for follow status and list indicators
package tuisocial

import (
	"fmt"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// followStatus represents the follow relationship between a repo and the workspace
type followStatus int

const (
	followStatusNone       followStatus = iota // Neither follows the other
	followStatusFollowsYou                     // They follow us but we don't follow them
	followStatusFollowed                       // We follow them but they don't follow us
	followStatusMutual                         // Mutual follow (we follow them AND they follow us)
)

// getFollowStatus determines the follow relationship for a repo
func getFollowStatus(repoURL string, lists []social.List, followerSet map[string]bool) followStatus {
	normalizedURL := protocol.NormalizeURL(repoURL)
	theyFollowUs := followerSet[normalizedURL]
	weFollowThem := isRepoInAnyList(normalizedURL, lists)
	if weFollowThem && theyFollowUs {
		return followStatusMutual
	}
	if weFollowThem {
		return followStatusFollowed
	}
	if theyFollowUs {
		return followStatusFollowsYou
	}
	return followStatusNone
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

// getListNamesForRepo returns the list names that contain a specific repo
func getListNamesForRepo(repoURL string, lists []social.List, excludeListID string) []string {
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

// formatListIndicator formats list names as "[list1, list2]" or "[+N more]" if too many
func formatListIndicator(names []string, maxVisible int) string {
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

// renderFollowIndicator renders the follow status indicator with appropriate styling
// List names are shown in purple, status indicators in dim
func renderFollowIndicator(status followStatus, listNames []string, selected bool) string {
	switch status {
	case followStatusMutual, followStatusFollowed:
		indicator := formatListIndicator(listNames, 2)
		if indicator == "" {
			return ""
		}
		if selected {
			return tuicore.ListIndicatorSelected.Render(indicator)
		}
		return tuicore.ListIndicator.Render(indicator)
	case followStatusFollowsYou:
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
