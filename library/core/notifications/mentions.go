// mentions.go - Mention extraction from commit messages and CommitProcessor
package notifications

import (
	"database/sql"
	"log/slog"
	"regexp"
	"strings"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/fetch"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

var mentionPattern = regexp.MustCompile(`(?:^|[^@])@([a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,})`)

// ExtractMentions returns deduplicated email addresses mentioned in content.
func ExtractMentions(content string) []string {
	matches := mentionPattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	var emails []string
	for _, m := range matches {
		email := strings.ToLower(m[1])
		if !seen[email] {
			seen[email] = true
			emails = append(emails, email)
		}
	}
	return emails
}

// MentionProcessor returns a CommitProcessor that extracts mentions and inserts into core_mentions.
func MentionProcessor() fetch.CommitProcessor {
	return func(commit git.Commit, msg *protocol.Message, repoURL, branch string) {
		var content string
		if msg != nil {
			content = msg.Content
		} else {
			content = commit.Message
		}
		emails := ExtractMentions(content)
		if len(emails) == 0 {
			return
		}
		if err := cache.ExecLocked(func(db *sql.DB) error {
			for _, email := range emails {
				if _, err := db.Exec(`
					INSERT INTO core_mentions (repo_url, hash, branch, email) VALUES (?, ?, ?, ?)
					ON CONFLICT(repo_url, hash, branch, email) DO NOTHING
				`, repoURL, commit.Hash, branch, email); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			slog.Warn("insert mentions", "error", err, "repo", repoURL, "hash", commit.Hash)
		}
	}
}
