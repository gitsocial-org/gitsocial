// provider.go - Release notification provider for new releases from followed repos
package release

import (
	"database/sql"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/notifications"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

type releaseNotificationProvider struct{}

func init() {
	notifications.RegisterProvider("release", &releaseNotificationProvider{})
}

// ReleaseNotification holds release-specific notification data.
type ReleaseNotification struct {
	ID         string
	Type       string // "new-release"
	RepoURL    string
	Hash       string
	Branch     string
	Version    string
	Tag        string
	Prerelease bool
	Subject    string
	ActorName  string
	ActorEmail string
	Timestamp  time.Time
	IsRead     bool
}

// GetNotifications returns release notifications (new releases from followed repos).
func (p *releaseNotificationProvider) GetNotifications(workdir string, filter notifications.Filter) ([]notifications.Notification, error) {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	if workspaceURL == "" {
		return nil, nil
	}
	return getNewReleaseNotifications(workspaceURL, workdir, filter.UnreadOnly, filter.Limit)
}

// GetUnreadCount returns the total unread release notification count.
func (p *releaseNotificationProvider) GetUnreadCount(workdir string) (int, error) {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	if workspaceURL == "" {
		return 0, nil
	}
	return countUnreadReleases(workspaceURL, workdir)
}

// getNewReleaseNotifications returns notifications for releases from repos in user's lists.
func getNewReleaseNotifications(workspaceURL, workdir string, unreadOnly bool, limit int) ([]notifications.Notification, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]notifications.Notification, error) {
		query := `
			SELECT v.repo_url, v.hash, v.branch,
			       v.author_name, v.author_email, v.resolved_message, v.timestamp,
			       v.tag, v.version, v.prerelease,
			       nr.repo_url
			FROM release_items_resolved v
			LEFT JOIN core_notification_reads nr ON v.repo_url = nr.repo_url AND v.hash = nr.hash AND v.branch = nr.branch
			WHERE v.repo_url != ?
			  AND v.repo_url IN (SELECT lr.repo_url FROM core_list_repositories lr
			                     JOIN core_lists l ON lr.list_id = l.id WHERE l.workdir = ?)
			  AND NOT v.is_edit_commit AND NOT v.is_retracted`
		args := []interface{}{workspaceURL, workdir}
		if unreadOnly {
			query += " AND nr.repo_url IS NULL"
		}
		query += " ORDER BY v.timestamp DESC"
		if limit > 0 {
			query += " LIMIT ?"
			args = append(args, limit)
		}
		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var result []notifications.Notification
		for rows.Next() {
			var repoURL, hash, branch, authorName, authorEmail string
			var message, ts, tag, version sql.NullString
			var prerelease int
			var readRepoURL sql.NullString
			err := rows.Scan(
				&repoURL, &hash, &branch,
				&authorName, &authorEmail, &message, &ts,
				&tag, &version, &prerelease,
				&readRepoURL,
			)
			if err != nil {
				return nil, err
			}
			var timestamp time.Time
			if ts.Valid {
				timestamp, _ = time.Parse(time.RFC3339, ts.String)
			}
			subject := ""
			if message.Valid {
				content := protocol.ExtractCleanContent(message.String)
				subject, _ = protocol.SplitSubjectBody(content)
			}
			isRead := readRepoURL.Valid
			rn := ReleaseNotification{
				ID:         protocol.CreateRef(protocol.RefTypeCommit, hash, repoURL, branch),
				Type:       "new-release",
				RepoURL:    repoURL,
				Hash:       hash,
				Branch:     branch,
				Version:    version.String,
				Tag:        tag.String,
				Prerelease: prerelease == 1,
				Subject:    subject,
				ActorName:  authorName,
				ActorEmail: authorEmail,
				Timestamp:  timestamp,
				IsRead:     isRead,
			}
			result = append(result, notifications.Notification{
				RepoURL:   repoURL,
				Hash:      hash,
				Branch:    branch,
				Type:      "new-release",
				Source:    "release",
				Item:      rn,
				Actor:     notifications.Actor{Name: authorName, Email: authorEmail},
				ActorRepo: repoURL,
				Timestamp: timestamp,
				IsRead:    isRead,
			})
		}
		return result, rows.Err()
	})
}

// countUnreadReleases counts unread release notifications.
func countUnreadReleases(workspaceURL, workdir string) (int, error) {
	return cache.QueryLocked(func(db *sql.DB) (int, error) {
		var count int
		err := db.QueryRow(`
			SELECT COUNT(*) FROM release_items_resolved v
			LEFT JOIN core_notification_reads nr ON v.repo_url = nr.repo_url AND v.hash = nr.hash AND v.branch = nr.branch
			WHERE v.repo_url != ?
			  AND v.repo_url IN (SELECT lr.repo_url FROM core_list_repositories lr
			                     JOIN core_lists l ON lr.list_id = l.id WHERE l.workdir = ?)
			  AND NOT v.is_edit_commit AND NOT v.is_retracted
			  AND nr.repo_url IS NULL
		`, workspaceURL, workdir).Scan(&count)
		return count, err
	})
}
