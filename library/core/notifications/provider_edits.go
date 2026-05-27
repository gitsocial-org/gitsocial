// provider_edits.go - Notification provider for edits to items authored by the current user
package notifications

import (
	"database/sql"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
)

type editProvider struct{}

func init() {
	RegisterProvider("core", &editProvider{})
}

// EditNotification carries the canonical ref alongside the edit commit so
// display layers can render "X edited your <item>" without a follow-up query.
type EditNotification struct {
	CanonicalRepoURL string
	CanonicalHash    string
	CanonicalBranch  string
	IsRetracted      bool
}

// GetNotifications returns notifications for edit commits authored by someone
// other than the current user, where the canonical item being edited was
// authored by the current user.
func (p *editProvider) GetNotifications(workdir string, filter Filter) ([]Notification, error) {
	userEmail := strings.ToLower(git.GetUserEmail(workdir))
	if userEmail == "" {
		return nil, nil
	}
	return cache.QueryLocked(func(db *sql.DB) ([]Notification, error) {
		query := `
			SELECT v.edit_repo_url, v.edit_hash, v.edit_branch,
			       v.canonical_repo_url, v.canonical_hash, v.canonical_branch,
			       v.is_retracted,
			       ec.author_name, ec.author_email, ec.timestamp,
			       CASE WHEN r.repo_url IS NOT NULL THEN 1 ELSE 0 END as is_read
			FROM core_commits_version v
			JOIN core_commits ec ON v.edit_repo_url = ec.repo_url
			                    AND v.edit_hash = ec.hash
			                    AND v.edit_branch = ec.branch
			JOIN core_commits cc ON v.canonical_repo_url = cc.repo_url
			                    AND v.canonical_hash = cc.hash
			                    AND v.canonical_branch = cc.branch
			LEFT JOIN core_notification_reads r ON v.edit_repo_url = r.repo_url
			                                   AND v.edit_hash = r.hash
			                                   AND v.edit_branch = r.branch
			WHERE cc.author_email = ?
			  AND ec.author_email != ?
		`
		args := []interface{}{userEmail, userEmail}
		if filter.UnreadOnly {
			query += " AND r.repo_url IS NULL"
		}
		query += " ORDER BY ec.timestamp DESC"
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
			var editRepoURL, editHash, editBranch string
			var canonicalRepoURL, canonicalHash, canonicalBranch string
			var isRetracted int
			var authorName, authorEmail string
			var ts sql.NullString
			var isRead int
			if err := rows.Scan(
				&editRepoURL, &editHash, &editBranch,
				&canonicalRepoURL, &canonicalHash, &canonicalBranch,
				&isRetracted,
				&authorName, &authorEmail, &ts,
				&isRead,
			); err != nil {
				continue
			}
			var timestamp time.Time
			if ts.Valid {
				if t, err := time.Parse(time.RFC3339, ts.String); err == nil {
					timestamp = t
				}
			}
			results = append(results, Notification{
				RepoURL: editRepoURL,
				Hash:    editHash,
				Branch:  editBranch,
				Type:    "edit",
				Source:  "core",
				Item: EditNotification{
					CanonicalRepoURL: canonicalRepoURL,
					CanonicalHash:    canonicalHash,
					CanonicalBranch:  canonicalBranch,
					IsRetracted:      isRetracted == 1,
				},
				Actor:     Actor{Name: authorName, Email: authorEmail},
				ActorRepo: editRepoURL,
				Timestamp: timestamp,
				IsRead:    isRead == 1,
			})
		}
		return results, rows.Err()
	})
}

// GetUnreadCount returns the number of unread edit notifications.
func (p *editProvider) GetUnreadCount(workdir string) (int, error) {
	userEmail := strings.ToLower(git.GetUserEmail(workdir))
	if userEmail == "" {
		return 0, nil
	}
	return cache.QueryLocked(func(db *sql.DB) (int, error) {
		var count int
		err := db.QueryRow(`
			SELECT COUNT(*) FROM core_commits_version v
			JOIN core_commits ec ON v.edit_repo_url = ec.repo_url
			                    AND v.edit_hash = ec.hash
			                    AND v.edit_branch = ec.branch
			JOIN core_commits cc ON v.canonical_repo_url = cc.repo_url
			                    AND v.canonical_hash = cc.hash
			                    AND v.canonical_branch = cc.branch
			LEFT JOIN core_notification_reads r ON v.edit_repo_url = r.repo_url
			                                   AND v.edit_hash = r.hash
			                                   AND v.edit_branch = r.branch
			WHERE cc.author_email = ?
			  AND ec.author_email != ?
			  AND r.repo_url IS NULL
		`, userEmail, userEmail).Scan(&count)
		return count, err
	})
}
