// provider_mentions.go - Notification provider for @email mentions in commit messages
package notifications

import (
	"database/sql"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
)

type mentionProvider struct{}

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
			       c.author_name, c.author_email, c.timestamp,
			       CASE WHEN r.repo_url IS NOT NULL THEN 1 ELSE 0 END as is_read
			FROM core_mentions m
			JOIN core_commits c ON m.repo_url = c.repo_url AND m.hash = c.hash AND m.branch = c.branch
			LEFT JOIN core_notification_reads r ON m.repo_url = r.repo_url AND m.hash = r.hash AND m.branch = r.branch
			WHERE m.email = ?
			  AND c.author_email != ?
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
			var repoURL, hash, branch, authorName, authorEmail string
			var ts sql.NullString
			var isRead int
			if err := rows.Scan(&repoURL, &hash, &branch, &authorName, &authorEmail, &ts, &isRead); err != nil {
				continue
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
			})
		}
		return results, rows.Err()
	})
}

// GetUnreadCount returns the number of unread mention notifications.
func (p *mentionProvider) GetUnreadCount(workdir string) (int, error) {
	userEmail := strings.ToLower(git.GetUserEmail(workdir))
	if userEmail == "" {
		return 0, nil
	}

	return cache.QueryLocked(func(db *sql.DB) (int, error) {
		var count int
		err := db.QueryRow(`
			SELECT COUNT(*) FROM core_mentions m
			JOIN core_commits c ON m.repo_url = c.repo_url AND m.hash = c.hash AND m.branch = c.branch
			LEFT JOIN core_notification_reads r ON m.repo_url = r.repo_url AND m.hash = r.hash AND m.branch = r.branch
			WHERE m.email = ?
			  AND c.author_email != ?
			  AND r.repo_url IS NULL
		`, userEmail, userEmail).Scan(&count)
		return count, err
	})
}
