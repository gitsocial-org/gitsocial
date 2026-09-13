// format.go - Text formatting for posts, timelines, and lists
package social

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/text"
)

var mdImageRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)(?:\{[^}]*\})?`)

// FormatPost formats a post for text display with author, content, and stats.
func FormatPost(post Post) string {
	var lines []string

	displayName, displayTime := post.Author.Name, post.Timestamp
	if a := protocol.OriginDisplayAuthor(post.Origin); a != "" {
		displayName = a
		if post.Origin.Time != "" {
			if t, err := time.Parse(time.RFC3339, post.Origin.Time); err == nil {
				displayTime = t
			}
		}
	}
	header := fmt.Sprintf("%s · %s", displayName, relativeTime(displayTime, true))
	if post.IsEdited {
		header += " (edited)"
	}
	lines = append(lines, header)

	if post.IsRetracted {
		lines = append(lines, "[retracted]")
		return strings.Join(lines, "\n")
	}

	content := strings.TrimSpace(post.Content)
	content = mdImageRe.ReplaceAllStringFunc(content, func(match string) string {
		subs := mdImageRe.FindStringSubmatch(match)
		label := "IMAGE"
		if subs[1] != "" {
			label = "IMAGE: " + subs[1]
		}
		return "[" + label + "] (" + subs[2] + ")"
	})
	content = text.Truncate(content, 200)
	lines = append(lines, content)

	if len(post.Labels) > 0 {
		lines = append(lines, "  "+strings.Join(post.Labels, " · "))
	}

	var stats []string
	if post.Interactions.Comments > 0 {
		stats = append(stats, fmt.Sprintf("%d comments", post.Interactions.Comments))
	}
	if post.Interactions.Reposts > 0 {
		stats = append(stats, fmt.Sprintf("%d reposts", post.Interactions.Reposts))
	}
	if post.Interactions.Quotes > 0 {
		stats = append(stats, fmt.Sprintf("%d quotes", post.Interactions.Quotes))
	}
	if len(stats) > 0 {
		lines = append(lines, "  "+strings.Join(stats, " · "))
	}

	repoName := post.Display.RepositoryName
	if repoName == "" {
		repoName = post.Repository
	}
	hash := post.Display.CommitHash
	if hash == "" {
		hash = post.ID
	}
	lines = append(lines, fmt.Sprintf("  %s · %s", repoName, hash))

	return strings.Join(lines, "\n")
}

// FormatTimeline formats a list of posts as a separated timeline.
func FormatTimeline(posts []Post) string {
	if len(posts) == 0 {
		return "No posts found."
	}

	parts := make([]string, 0, len(posts))
	for _, post := range posts {
		parts = append(parts, FormatPost(post))
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// FormatList formats a single list with its ID, name, and repository count.
func FormatList(list List) string {
	var lines []string

	lines = append(lines, fmt.Sprintf("%s (%s)", list.ID, list.Name))
	lines = append(lines, fmt.Sprintf("  %d repositories", len(list.Repositories)))

	if list.IsFollowedLocally {
		lines = append(lines, "  [followed]")
	}
	if list.IsUnpushed {
		lines = append(lines, "  [⇡]")
	}

	return strings.Join(lines, "\n")
}

// FormatLists formats multiple lists for text display.
func FormatLists(lists []List) string {
	if len(lists) == 0 {
		return "No lists found."
	}

	parts := make([]string, 0, len(lists))
	for _, list := range lists {
		parts = append(parts, FormatList(list))
	}
	return strings.Join(parts, "\n\n")
}

// formatRepository formats a repository with its URL, branch, and lists.
func formatRepository(repo Repository) string {
	var lines []string
	lines = append(lines, repo.Name)
	lines = append(lines, fmt.Sprintf("  %s", repo.URL))
	if repo.Branch != "" {
		lines = append(lines, fmt.Sprintf("  branch: %s", repo.Branch))
	}
	if len(repo.Lists) > 0 {
		lines = append(lines, fmt.Sprintf("  lists: %s", strings.Join(repo.Lists, ", ")))
	}
	return strings.Join(lines, "\n")
}

// FormatRepositories formats multiple repositories for text display.
func FormatRepositories(repos []Repository) string {
	if len(repos) == 0 {
		return "No repositories found."
	}
	parts := make([]string, 0, len(repos))
	for _, repo := range repos {
		parts = append(parts, formatRepository(repo))
	}
	return strings.Join(parts, "\n\n")
}

// formatRelatedRepository formats a related repository with its relationships.
func formatRelatedRepository(repo RelatedRepository) string {
	var lines []string
	lines = append(lines, repo.Name)
	lines = append(lines, fmt.Sprintf("  %s", repo.URL))
	if len(repo.Relationships.SharedLists) > 0 {
		lines = append(lines, fmt.Sprintf("  shared lists: %s", strings.Join(repo.Relationships.SharedLists, ", ")))
	}
	if len(repo.Relationships.SharedAuthors) > 0 {
		lines = append(lines, fmt.Sprintf("  shared authors: %s", strings.Join(repo.Relationships.SharedAuthors, ", ")))
	}
	return strings.Join(lines, "\n")
}

// FormatRelatedRepositories formats multiple related repositories for text display.
func FormatRelatedRepositories(repos []RelatedRepository) string {
	if len(repos) == 0 {
		return "No related repositories found."
	}
	var parts []string
	for _, repo := range repos {
		parts = append(parts, formatRelatedRepository(repo))
	}
	return strings.Join(parts, "\n\n")
}

// formatLogEntry formats a single log entry as a compact line.
func formatLogEntry(entry LogEntry) string {
	hash := entry.Hash
	if len(hash) > 7 {
		hash = hash[:7]
	}
	date := relativeTime(entry.Timestamp, true)
	return fmt.Sprintf("%s %s %-18s %s: %s", hash, date, entry.Type, entry.Author.Name, entry.Details)
}

// FormatLogs formats multiple log entries for text display.
func FormatLogs(entries []LogEntry) string {
	if len(entries) == 0 {
		return "No activity found."
	}
	var lines []string
	for _, entry := range entries {
		lines = append(lines, formatLogEntry(entry))
	}
	return strings.Join(lines, "\n")
}
