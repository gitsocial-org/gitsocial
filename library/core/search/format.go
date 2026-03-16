// format.go - Text formatting for search results
package search

import (
	"fmt"
	"strings"
	"time"
)

// FormatResult formats search results for CLI text display.
func FormatResult(result Result) string {
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
	lines := make([]string, 0, 2)

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

	return strings.Join(lines, "\n")
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
