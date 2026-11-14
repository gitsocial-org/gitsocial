// provider_mentions.go - Notification provider for @email mentions in commit messages
package notifications

import (
	"database/sql"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
)

type mentionProvider struct{}

// init registers the mention notification provider.
func init() {
	RegisterProvider("core", &mentionProvider{})
}

// GetNotifications returns mention notifications for the current user.
func (p *mentionProvider) GetNotifications(workdir string, filter Filter) ([]Notification, error) {
	userEmail := strings.ToLower(git.GetUserEmail(workdir))
	if userEmail == "" {
		return nil, nil
	}

	return cache.QueryLocked(func(db *sql.DB) ([]Notification, error) {
		query := `
			SELECT m.repo_url, m.hash, m.branch,
			       c.author_name, c.author_email, c.timestamp, c.message,
			       CASE WHEN r.repo_url IS NOT NULL THEN 1 ELSE 0 END as is_read
			FROM core_mentions m
			JOIN core_commits c ON m.repo_url = c.repo_url AND m.hash = c.hash AND m.branch = c.branch
			` + cache.ReadStateJoin("r", "m.repo_url", "m.hash") + `
			WHERE m.email = ?
			  AND c.author_email != ?
			  AND c.stale_since IS NULL
		`
		args := []interface{}{userEmail, userEmail}

		if filter.UnreadOnly {
			query += " AND r.repo_url IS NULL"
		}

		query += " ORDER BY c.timestamp DESC"

		if filter.Limit > 0 {
			query += " LIMIT ?"
			args = append(args, filter.Limit)
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var results []Notification
		for rows.Next() {
			var repoURL, hash, branch, authorName, authorEmail, message string
			var ts sql.NullString
			var isRead int
			if err := rows.Scan(&repoURL, &hash, &branch, &authorName, &authorEmail, &ts, &message, &isRead); err != nil {
				return nil, err
			}
			var timestamp time.Time
			if ts.Valid {
				if t, err := time.Parse(time.RFC3339, ts.String); err == nil {
					timestamp = t
				}
			}
			results = append(results, Notification{
				RepoURL:   repoURL,
				Hash:      hash,
				Branch:    branch,
				Type:      "mention",
				Source:    "core",
				Actor:     Actor{Name: authorName, Email: authorEmail},
				ActorRepo: repoURL,
				Timestamp: timestamp,
				IsRead:    isRead == 1,
				Excerpt:   FirstLine(message),
				Glyph:     commitGlyph(message, branch),
			})
		}
		return results, rows.Err()
	})
}
