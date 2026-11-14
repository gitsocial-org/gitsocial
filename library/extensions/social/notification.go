// notification.go - Notification queries, read status, and filtering
package social

import (
	"database/sql"
	"sort"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// userThreadsCTE pre-computes the threads the user is in, each by repository and hash; parameters: userEmail, workspaceURL, workdir.
const userThreadsCTE = `
	user_threads AS (
		SELECT DISTINCT s.original_repo_url, s.original_hash
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

// followedReposCondition keeps thread notifications to followed repositories; parameter: workdir.
const followedReposCondition = `
	s.original_repo_url IN (
		SELECT lr.repo_url FROM core_list_repositories lr
		JOIN core_lists l ON lr.list_id = l.id WHERE l.workdir = ?)
`

// notifiableItems selects a notifiable interaction; parameters: workspaceURL, userEmail, workspaceURL, workdir.
const notifiableItems = `
	s.type IN ('comment', 'repost', 'quote')
	  AND s.repo_url != ?
	  AND COALESCE(c.origin_author_email, c.author_email) != ?
	  AND NOT c.is_edit_commit AND NOT c.is_retracted AND (c.stale_since IS NULL OR c.is_virtual = 1)
	  AND (
	    s.original_repo_url = ?
	    OR (EXISTS (
	      SELECT 1 FROM user_threads ut
	      WHERE ut.original_repo_url = s.original_repo_url
	        AND ut.original_hash = s.original_hash
	    ) AND ` + followedReposCondition + `)
	  )
`

// getNotifications retrieves notifications for interactions on workspace posts and threads the user participates in, the activity events and the follows.
func getNotifications(workdir string, filter notificationFilter) ([]Notification, error) {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	if workspaceURL == "" {
		return nil, nil
	}
	userEmail := git.GetUserEmail(workdir)

	items, err := cache.QueryLocked(func(db *sql.DB) ([]notificationRow, error) {
		// Drive from social_items, indexed by type; the view scans core_commits.
		query := `
			WITH ` + userThreadsCTE + `
			SELECT s.repo_url, s.hash, s.branch, s.type,
			       s.original_repo_url, s.original_hash, s.original_branch,
			       s.reply_to_repo_url, s.reply_to_hash, s.reply_to_branch,
			       COALESCE(c.resolved_message, c.message), c.message,
			       COALESCE(c.origin_author_name, c.author_name),
			       COALESCE(c.origin_author_email, c.author_email),
			       COALESCE(c.origin_time, c.timestamp),
			       c.is_virtual,
			       COALESCE(i.comments, 0), COALESCE(i.reposts, 0), COALESCE(i.quotes, 0),
			       CASE WHEN r.repo_url IS NOT NULL THEN 1 ELSE 0 END as is_read,
			       (SELECT COALESCE(o.resolved_message, o.message) FROM core_commits o WHERE o.repo_url = s.original_repo_url AND o.hash = s.original_hash
			        ORDER BY ` + cache.LiveFirstOrder("o") + `, o.branch LIMIT 1)
			FROM social_items s
			JOIN core_commits c ON s.repo_url = c.repo_url AND s.hash = c.hash AND s.branch = c.branch
			LEFT JOIN social_interactions i ON s.repo_url = i.repo_url AND s.hash = i.hash AND s.branch = i.branch
			` + cache.ReadStateJoin("r", "s.repo_url", "s.hash") + `
			WHERE ` + notifiableItems
		args := []interface{}{userEmail, workspaceURL, workdir, workspaceURL, userEmail, workspaceURL, workdir}

		if filter.UnreadOnly {
			query += " AND r.repo_url IS NULL"
		}

		query += " ORDER BY COALESCE(c.origin_time, c.timestamp) DESC"

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
			var message, originalMessage, ts, targetMessage sql.NullString
			var isVirtual int
			var isRead int
			err := rows.Scan(
				&row.item.RepoURL, &row.item.Hash, &row.item.Branch, &row.item.Type,
				&row.item.OriginalRepoURL, &row.item.OriginalHash, &row.item.OriginalBranch,
				&row.item.ReplyToRepoURL, &row.item.ReplyToHash, &row.item.ReplyToBranch,
				&message, &originalMessage, &row.item.AuthorName, &row.item.AuthorEmail, &ts,
				&isVirtual,
				&row.item.Comments, &row.item.Reposts, &row.item.Quotes, &isRead, &targetMessage,
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
			row.originalSubject = notifications.FirstLine(targetMessage.String)
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
			ID:              post.ID,
			Type:            NotificationType(row.item.Type),
			Item:            &post,
			TargetID:        targetID,
			Actor:           post.Author,
			ActorRepo:       row.item.RepoURL,
			Timestamp:       row.item.Timestamp,
			IsRead:          row.isRead,
			OriginalSubject: row.originalSubject,
		})
	}

	activity, err := getActivityNotifications(workdir, workspaceURL, userEmail, filter.UnreadOnly)
	if err != nil {
		log.Warn("get activity notifications failed, returning the rest", "error", err)
	}
	notifications = append(notifications, activity...)

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
			` + cache.ReadStateJoin("r", "f.repo_url", "'follow'") + `
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
	sort.SliceStable(notifications, func(i, j int) bool {
		return notifications[i].Timestamp.After(notifications[j].Timestamp)
	})
}

type notificationRow struct {
	item            SocialItem
	isRead          bool
	originalSubject string
}
