// format.go - Text formatting for search results
package search

import (
	"fmt"
	"strings"
	"time"
)

// FormatResult formats search results for CLI text display.
func FormatResult(result Result) string {
	if len(result.Groups) > 0 {
		return formatGroupedResult(result)
	}
	if len(result.Results) == 0 {
		return fmt.Sprintf("No results for '%s'.", result.Query)
	}
	var parts []string
	for _, item := range result.Results {
		parts = append(parts, formatItem(item))
	}
	parts = append(parts, fmt.Sprintf("\n%d results (%.2fms)", result.Total, float64(result.ExecutionTimeMs)))
	return strings.Join(parts, "\n\n---\n\n")
}

// formatItem formats a single search result item for text display.
func formatItem(item ScoredItem) string {
	lines := make([]string, 0, 3)

	header := fmt.Sprintf("%s · %s", item.AuthorName, formatDate(item.Timestamp))
	if item.Extension != "social" && item.Extension != "unknown" {
		header += fmt.Sprintf(" [%s/%s]", item.Extension, item.Type)
	}
	lines = append(lines, header)

	content := strings.TrimSpace(item.Content)
	if len(content) > 200 {
		content = content[:200] + "..."
	}
	lines = append(lines, content)

	// Extension-specific metadata
	if meta := formatItemMeta(item); meta != "" {
		lines = append(lines, meta)
	}

	return strings.Join(lines, "\n")
}

// formatItemMeta formats extension-specific metadata for a search result.
func formatItemMeta(item ScoredItem) string {
	var parts []string
	if item.State != "" {
		parts = append(parts, item.State)
	}
	if item.Draft {
		parts = append(parts, "draft")
	}
	if item.Base != "" && item.Head != "" {
		parts = append(parts, fmt.Sprintf("%s ← %s", item.Base, item.Head))
	}
	if item.Assignees != "" {
		parts = append(parts, "assigned:"+item.Assignees)
	}
	if item.Reviewers != "" {
		parts = append(parts, "reviewers:"+item.Reviewers)
	}
	if item.Labels != "" {
		parts = append(parts, "labels:"+item.Labels)
	}
	if item.Tag != "" {
		parts = append(parts, item.Tag)
	}
	if item.Version != "" && item.Version != item.Tag {
		parts = append(parts, item.Version)
	}
	if item.Prerelease {
		parts = append(parts, "pre-release")
	}
	if item.Due != "" {
		parts = append(parts, "due:"+item.Due)
	}
	if item.Comments > 0 {
		parts = append(parts, fmt.Sprintf("↩ %d", item.Comments))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

// formatGroupedResult formats grouped search results for CLI text display.
func formatGroupedResult(result Result) string {
	var parts []string
	for _, g := range result.Groups {
		header := fmt.Sprintf("## %s (%d)", g.Key, g.Count)
		parts = append(parts, header)
		if len(g.Items) > 0 {
			// Check if items have author info for sub-grouping
			hasAuthors := false
			for _, item := range g.Items {
				if item.Author != "" {
					hasAuthors = true
					break
				}
			}
			if hasAuthors {
				// Sub-group items by author for compact display
				authorItems := make(map[string][]string)
				var authorOrder []string
				for _, item := range g.Items {
					author := item.Author
					if author == "" {
						author = "(unknown)"
					}
					if _, exists := authorItems[author]; !exists {
						authorOrder = append(authorOrder, author)
					}
					authorItems[author] = append(authorItems[author], item.Subject)
				}
				for _, author := range authorOrder {
					subjects := authorItems[author]
					summary := strings.Join(truncateStrings(subjects, 3), ", ")
					if len(subjects) > 3 {
						summary += fmt.Sprintf(", ... +%d more", len(subjects)-3)
					}
					parts = append(parts, fmt.Sprintf("  %s (%d): %s", author, len(subjects), summary))
				}
			} else {
				// No author info (e.g., grouped by author) — list subjects directly
				subjects := make([]string, 0, len(g.Items))
				for _, item := range g.Items {
					subjects = append(subjects, item.Subject)
				}
				summary := strings.Join(truncateStrings(subjects, 5), ", ")
				if len(subjects) > 5 {
					summary += fmt.Sprintf(", ... +%d more", len(subjects)-5)
				}
				parts = append(parts, "  "+summary)
			}
		}
	}
	parts = append(parts, fmt.Sprintf("\nTotal: %d (grouped by %s)", result.Total, result.GroupBy))
	return strings.Join(parts, "\n")
}

// truncateStrings returns up to n strings, each truncated to 50 chars.
func truncateStrings(ss []string, n int) []string {
	limit := n
	if limit > len(ss) {
		limit = len(ss)
	}
	result := make([]string, limit)
	for i := 0; i < limit; i++ {
		s := ss[i]
		if len(s) > 50 {
			s = s[:50] + "..."
		}
		result[i] = s
	}
	return result
}

// formatDate formats a timestamp as a human-readable relative time string.
func formatDate(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	mins := int(diff.Minutes())
	hours := int(diff.Hours())
	days := int(diff.Hours() / 24)

	if mins < 1 {
		return "just now"
	}
	if mins < 60 {
		return fmt.Sprintf("%dm ago", mins)
	}
	if hours < 24 {
		return fmt.Sprintf("%dh ago", hours)
	}
	if days < 7 {
		return fmt.Sprintf("%dd ago", days)
	}
	return t.Format("Jan 2, 2006")
}
