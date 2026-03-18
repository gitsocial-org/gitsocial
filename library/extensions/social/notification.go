// notification.go - Notification queries, read status, and filtering
package social

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// userThreadsCTE pre-computes threads the user participates in, scoped to repos
// the workspace follows. Queries base tables directly for performance.
// Parameters: userEmail, workspaceURL, workdir
const userThreadsCTE = `
	user_threads AS (
		SELECT DISTINCT s.original_repo_url, s.original_hash, s.original_branch
		FROM social_items s
		JOIN core_commits c ON s.repo_url = c.repo_url AND s.hash = c.hash AND s.branch = c.branch
		WHERE s.type IN ('comment', 'repost', 'quote')
		  AND COALESCE(c.origin_author_email, c.author_email) = ?
		  AND s.original_repo_url IS NOT NULL
		  AND (s.original_repo_url = ?
		       OR s.original_repo_url IN (
		           SELECT lr.repo_url FROM core_list_repositories lr
		           JOIN core_lists l ON lr.list_id = l.id WHERE l.workdir = ?))
	)
`

// followedReposCondition filters thread-participation notifications to repos the workspace follows.
// Parameter: workdir
const followedReposCondition = `
	v.original_repo_url IN (
		SELECT lr.repo_url FROM core_list_repositories lr
		JOIN core_lists l ON lr.list_id = l.id WHERE l.workdir = ?)
`

// GetNotifications retrieves notifications for interactions on workspace posts and threads the user participates in.
func GetNotifications(workdir string, filter NotificationFilter) ([]Notification, error) {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	if workspaceURL == "" {
		return nil, nil
	}
	userEmail := git.GetUserEmail(workdir)

	items, err := cache.QueryLocked(func(db *sql.DB) ([]notificationRow, error) {
		query := `
			WITH ` + userThreadsCTE + `
			SELECT v.repo_url, v.hash, v.branch, v.type,
			       v.original_repo_url, v.original_hash, v.original_branch,
			       v.reply_to_repo_url, v.reply_to_hash, v.reply_to_branch,
			       v.resolved_message, v.original_message, v.author_name, v.author_email, v.timestamp,
			       v.is_virtual,
			       v.comments, v.reposts, v.quotes,
			       CASE WHEN r.repo_url IS NOT NULL THEN 1 ELSE 0 END as is_read
			FROM social_items_resolved v
			LEFT JOIN core_notification_reads r ON v.repo_url = r.repo_url AND v.hash = r.hash AND v.branch = r.branch
			WHERE v.type IN ('comment', 'repost', 'quote')
			  AND v.repo_url != ?
			  AND v.author_email != ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted
			  AND (
			    v.original_repo_url = ?
			    OR (EXISTS (
			      SELECT 1 FROM user_threads ut
			      WHERE ut.original_repo_url = v.original_repo_url
			        AND ut.original_hash = v.original_hash
			        AND ut.original_branch = v.original_branch
			    ) AND ` + followedReposCondition + `)
			  )
		`
		args := []interface{}{userEmail, workspaceURL, workdir, workspaceURL, userEmail, workspaceURL, workdir}

		if filter.UnreadOnly {
			query += " AND r.repo_url IS NULL"
		}

		query += " ORDER BY v.timestamp DESC"

		if filter.Limit > 0 {
			query += " LIMIT ?"
			args = append(args, filter.Limit)
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var results []notificationRow
		for rows.Next() {
			var row notificationRow
			var message, originalMessage, ts sql.NullString
			var isVirtual int
			var isRead int
			err := rows.Scan(
				&row.item.RepoURL, &row.item.Hash, &row.item.Branch, &row.item.Type,
				&row.item.OriginalRepoURL, &row.item.OriginalHash, &row.item.OriginalBranch,
				&row.item.ReplyToRepoURL, &row.item.ReplyToHash, &row.item.ReplyToBranch,
				&message, &originalMessage, &row.item.AuthorName, &row.item.AuthorEmail, &ts,
				&isVirtual,
				&row.item.Comments, &row.item.Reposts, &row.item.Quotes, &isRead,
			)
			if err != nil {
				return nil, err
			}
			if message.Valid {
				row.item.Content = protocol.ExtractCleanContent(message.String)
				row.item.OriginalExtension, row.item.OriginalType = extractOriginalExtType(message.String)
				row.item.HeaderExt, row.item.HeaderType, row.item.HeaderState = extractHeaderFields(message.String)
			}
			if originalMessage.Valid {
				if msg := protocol.ParseMessage(originalMessage.String); msg != nil {
					row.item.Origin = protocol.ExtractOrigin(&msg.Header)
				}
			}
			if ts.Valid {
				row.item.Timestamp, _ = time.Parse(time.RFC3339, ts.String)
			}
			row.item.IsVirtual = isVirtual == 1
			row.isRead = isRead == 1
			results = append(results, row)
		}
		return results, rows.Err()
	})
	if err != nil {
		return nil, err
	}

	notifications := make([]Notification, 0, len(items))
	for _, row := range items {
		post := SocialItemToPost(row.item)
		targetID := ""
		if row.item.OriginalRepoURL.Valid && row.item.OriginalHash.Valid {
			targetID = row.item.OriginalRepoURL.String + "#commit:" + row.item.OriginalHash.String
		}
		notifications = append(notifications, Notification{
			ID:        post.ID,
			Type:      NotificationType(row.item.Type),
			Item:      &post,
			TargetID:  targetID,
			Actor:     post.Author,
			ActorRepo: row.item.RepoURL,
			Timestamp: row.item.Timestamp,
			IsRead:    row.isRead,
		})
	}

	followNotifications, err := getFollowNotifications(workspaceURL, filter.UnreadOnly)
	if err != nil {
		log.Warn("get follow notifications failed, returning partial results", "error", err)
		return notifications, nil
	}
	notifications = append(notifications, followNotifications...)
	sortNotificationsByTime(notifications)

	if filter.Limit > 0 && len(notifications) > filter.Limit {
		notifications = notifications[:filter.Limit]
	}

	return notifications, nil
}

// getFollowNotifications retrieves notifications for new followers.
func getFollowNotifications(workspaceURL string, unreadOnly bool) ([]Notification, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]Notification, error) {
		query := `
			SELECT f.repo_url, f.detected_at, COALESCE(f.list_id, ''), COALESCE(f.commit_hash, ''),
			       COALESCE((SELECT author_name FROM core_commits WHERE repo_url = f.repo_url ORDER BY timestamp DESC LIMIT 1), ''),
			       COALESCE((SELECT author_email FROM core_commits WHERE repo_url = f.repo_url ORDER BY timestamp DESC LIMIT 1), ''),
			       CASE WHEN r.repo_url IS NOT NULL THEN 1 ELSE 0 END as is_read,
			       COALESCE((SELECT branch FROM core_list_repositories WHERE list_id = f.list_id AND repo_url = f.repo_url LIMIT 1), 'main')
			FROM social_followers f
			LEFT JOIN core_notification_reads r ON f.repo_url = r.repo_url AND r.hash = 'follow' AND r.branch = ''
			WHERE f.workspace_url = ?
		`
		if unreadOnly {
			query += " AND r.repo_url IS NULL"
		}
		query += " ORDER BY f.detected_at DESC"

		rows, err := db.Query(query, workspaceURL)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var notifications []Notification
		for rows.Next() {
			var repoURL, listID, commitHash, authorName, authorEmail, branch string
			var detectedAt sql.NullString
			var isRead int
			if err := rows.Scan(&repoURL, &detectedAt, &listID, &commitHash, &authorName, &authorEmail, &isRead, &branch); err != nil {
				continue
			}
			var ts time.Time
			if detectedAt.Valid {
				ts, _ = time.Parse(time.RFC3339, detectedAt.String)
			}
			if authorName == "" {
				authorName = protocol.GetFullDisplayName(repoURL)
			}
			notifications = append(notifications, Notification{
				ID:         repoURL + "#follow",
				Type:       NotificationTypeFollow,
				ActorRepo:  repoURL,
				Branch:     branch,
				ListID:     listID,
				CommitHash: commitHash,
				Actor:      Author{Name: authorName, Email: authorEmail},
				Timestamp:  ts,
				IsRead:     isRead == 1,
			})
		}
		return notifications, rows.Err()
	})
}

// sortNotificationsByTime sorts notifications by timestamp descending.
func sortNotificationsByTime(notifications []Notification) {
	for i := 1; i < len(notifications); i++ {
		for j := i; j > 0 && notifications[j].Timestamp.After(notifications[j-1].Timestamp); j-- {
			notifications[j], notifications[j-1] = notifications[j-1], notifications[j]
		}
	}
}

// GetUnreadCount returns the count of unread notifications.
func GetUnreadCount(workdir string) (int, error) {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	if workspaceURL == "" {
		return 0, nil
	}
	userEmail := git.GetUserEmail(workdir)

	return cache.QueryLocked(func(db *sql.DB) (int, error) {
		var itemCount, followCount int
		if err := db.QueryRow(`
			WITH `+userThreadsCTE+`
			SELECT COUNT(*) FROM social_items_resolved v
			LEFT JOIN core_notification_reads r ON v.repo_url = r.repo_url AND v.hash = r.hash AND v.branch = r.branch
			WHERE v.type IN ('comment', 'repost', 'quote')
			  AND v.repo_url != ?
			  AND v.author_email != ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted
			  AND (
			    v.original_repo_url = ?
			    OR (EXISTS (
			      SELECT 1 FROM user_threads ut
			      WHERE ut.original_repo_url = v.original_repo_url
			        AND ut.original_hash = v.original_hash
			        AND ut.original_branch = v.original_branch
			    ) AND `+followedReposCondition+`)
			  )
			  AND r.repo_url IS NULL
		`, userEmail, workspaceURL, workdir, workspaceURL, userEmail, workspaceURL, workdir).Scan(&itemCount); err != nil {
			return 0, fmt.Errorf("count unread items: %w", err)
		}
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM social_followers f
			LEFT JOIN core_notification_reads r ON f.repo_url = r.repo_url AND r.hash = 'follow' AND r.branch = ''
			WHERE f.workspace_url = ? AND r.repo_url IS NULL
		`, workspaceURL).Scan(&followCount); err != nil {
			return 0, fmt.Errorf("count unread followers: %w", err)
		}
		return itemCount + followCount, nil
	})
}

// MarkAllAsRead marks all social notifications for the workspace as read.
func MarkAllAsRead(workdir string) error {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	if workspaceURL == "" {
		return nil
	}
	userEmail := git.GetUserEmail(workdir)

	now := time.Now().UTC().Format(time.RFC3339)
	return cache.ExecLocked(func(db *sql.DB) error {
		if _, err := db.Exec(`
			WITH `+userThreadsCTE+`
			INSERT INTO core_notification_reads (repo_url, hash, branch, read_at)
			SELECT v.repo_url, v.hash, v.branch, ? FROM social_items_resolved v
			LEFT JOIN core_notification_reads r ON v.repo_url = r.repo_url AND v.hash = r.hash AND v.branch = r.branch
			WHERE v.type IN ('comment', 'repost', 'quote')
			  AND v.repo_url != ?
			  AND v.author_email != ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted
			  AND (
			    v.original_repo_url = ?
			    OR (EXISTS (
			      SELECT 1 FROM user_threads ut
			      WHERE ut.original_repo_url = v.original_repo_url
			        AND ut.original_hash = v.original_hash
			        AND ut.original_branch = v.original_branch
			    ) AND `+followedReposCondition+`)
			  )
			  AND r.repo_url IS NULL
			ON CONFLICT(repo_url, hash, branch) DO NOTHING
		`, userEmail, workspaceURL, workdir, now, workspaceURL, userEmail, workspaceURL, workdir); err != nil {
			return fmt.Errorf("mark items as read: %w", err)
		}
		if _, err := db.Exec(`
			INSERT INTO core_notification_reads (repo_url, hash, branch, read_at)
			SELECT f.repo_url, 'follow', '', ? FROM social_followers f
			LEFT JOIN core_notification_reads r ON f.repo_url = r.repo_url AND r.hash = 'follow' AND r.branch = ''
			WHERE f.workspace_url = ? AND r.repo_url IS NULL
			ON CONFLICT(repo_url, hash, branch) DO NOTHING
		`, now, workspaceURL); err != nil {
			return fmt.Errorf("mark followers as read: %w", err)
		}
		return nil
	})
}

// MarkAllAsUnread marks all notifications for the workspace as unread.
func MarkAllAsUnread(workdir string) error {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	if workspaceURL == "" {
		return nil
	}
	userEmail := git.GetUserEmail(workdir)
	return cache.ExecLocked(func(db *sql.DB) error {
		if _, err := db.Exec(`
			WITH `+userThreadsCTE+`
			DELETE FROM core_notification_reads
			WHERE (repo_url, hash, branch) IN (
				SELECT v.repo_url, v.hash, v.branch FROM social_items_resolved v
				WHERE v.type IN ('comment', 'repost', 'quote')
				  AND v.repo_url != ?
				  AND v.author_email != ?
				  AND NOT v.is_edit_commit AND NOT v.is_retracted
				  AND (
				    v.original_repo_url = ?
				    OR (EXISTS (
				      SELECT 1 FROM user_threads ut
				      WHERE ut.original_repo_url = v.original_repo_url
				        AND ut.original_hash = v.original_hash
				        AND ut.original_branch = v.original_branch
				    ) AND `+followedReposCondition+`)
				  )
			)
		`, userEmail, workspaceURL, workdir, workspaceURL, userEmail, workspaceURL, workdir); err != nil {
			return fmt.Errorf("unmark items as read: %w", err)
		}
		if _, err := db.Exec(`
			DELETE FROM core_notification_reads
			WHERE (repo_url, hash, branch) IN (
				SELECT f.repo_url, 'follow', '' FROM social_followers f
				WHERE f.workspace_url = ?
			)
		`, workspaceURL); err != nil {
			return fmt.Errorf("unmark followers as read: %w", err)
		}
		return nil
	})
}

type notificationRow struct {
	item   SocialItem
	isRead bool
}
