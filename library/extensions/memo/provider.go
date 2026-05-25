// provider.go - Memo notification provider.
//
// Two notification flows:
//
//   - "memo-comment" — someone left a comment on a memo authored by the
//     current user. Fills the gap that social.GetNotifications leaves: the
//     social provider only surfaces comments on threads the user has already
//     participated in, so a fresh comment on an authored-but-unengaged memo
//     would otherwise pass silently.
//
//   - "inherited-policy" — an inherited source pushed a memo with
//     priority/critical. The whole point of binding inherited sources is that
//     their policies override local preferences; users should know when those
//     policies change without polling `memo list --tier inherited` by hand.
//
// Mentions in memo bodies are already handled by the core
// notifications.MentionProcessor (wired into fetch), so no memo-specific code
// is needed for "@you got mentioned in a memo."
package memo

import (
	"database/sql"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

type memoNotificationProvider struct{}

func init() {
	notifications.RegisterProvider("memo", &memoNotificationProvider{})
}

// MemoNotification holds memo-specific notification data, embedded in
// notifications.Notification.Item for consumers that want the full detail.
type MemoNotification struct {
	ID         string
	Type       string // "memo-comment" | "inherited-policy"
	RepoURL    string
	Hash       string
	Branch     string
	Subject    string
	Labels     []string
	ActorName  string
	ActorEmail string
	Timestamp  time.Time
	IsRead     bool
}

// GetNotifications returns memo notifications: comments on memos the user
// authored, plus new priority/critical memos on inherited sources.
func (p *memoNotificationProvider) GetNotifications(workdir string, filter notifications.Filter) ([]notifications.Notification, error) {
	userEmail := git.GetUserEmail(workdir)
	if userEmail == "" {
		return nil, nil
	}
	var all []notifications.Notification

	comments, err := getMemoCommentNotifications(userEmail, filter)
	if err == nil {
		all = append(all, comments...)
	}
	policies, err := getInheritedPolicyNotifications(workdir, userEmail, filter)
	if err == nil {
		all = append(all, policies...)
	}
	if filter.Limit > 0 && len(all) > filter.Limit {
		all = all[:filter.Limit]
	}
	return all, nil
}

// GetUnreadCount returns the unread count via the same query path.
func (p *memoNotificationProvider) GetUnreadCount(workdir string) (int, error) {
	items, err := p.GetNotifications(workdir, notifications.Filter{UnreadOnly: true})
	return len(items), err
}

// getMemoCommentNotifications surfaces comments where the original is a memo
// the current user authored. Drives off social_items joined to memo_items so
// the predicate stays selective on the (small) extension tables before
// pulling effective fields from core_commits.
func getMemoCommentNotifications(userEmail string, filter notifications.Filter) ([]notifications.Notification, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]notifications.Notification, error) {
		query := `
			SELECT s.repo_url, s.hash, s.branch,
			       COALESCE(c.origin_author_name, c.author_name),
			       COALESCE(c.origin_author_email, c.author_email),
			       COALESCE(c.resolved_message, c.message),
			       COALESCE(c.origin_time, c.timestamp),
			       s.original_repo_url, s.original_hash, s.original_branch,
			       CASE WHEN r.repo_url IS NOT NULL THEN 1 ELSE 0 END
			FROM social_items s
			JOIN memo_items m
			  ON s.original_repo_url = m.repo_url
			 AND s.original_hash = m.hash
			 AND s.original_branch = m.branch
			JOIN core_commits mc
			  ON m.repo_url = mc.repo_url
			 AND m.hash = mc.hash
			 AND m.branch = mc.branch
			JOIN core_commits c
			  ON s.repo_url = c.repo_url
			 AND s.hash = c.hash
			 AND s.branch = c.branch
			LEFT JOIN core_notification_reads r
			  ON s.repo_url = r.repo_url
			 AND s.hash = r.hash
			 AND s.branch = r.branch
			WHERE s.type = 'comment'
			  AND COALESCE(mc.origin_author_email, mc.author_email) = ?
			  AND COALESCE(c.origin_author_email, c.author_email) != ?
			  AND NOT c.is_edit_commit AND NOT c.is_retracted`
		args := []interface{}{userEmail, userEmail}
		if filter.UnreadOnly {
			query += " AND r.repo_url IS NULL"
		}
		query += " ORDER BY c.timestamp DESC"
		if filter.Limit > 0 {
			query += " LIMIT ?"
			args = append(args, filter.Limit)
		}
		return scanCommentRows(db, query, args)
	})
}

func scanCommentRows(db *sql.DB, query string, args []interface{}) ([]notifications.Notification, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []notifications.Notification
	for rows.Next() {
		var repoURL, hash, branch, actorName, actorEmail string
		var message, ts sql.NullString
		var origRepoURL, origHash, origBranch sql.NullString
		var isRead int
		if err := rows.Scan(
			&repoURL, &hash, &branch,
			&actorName, &actorEmail, &message, &ts,
			&origRepoURL, &origHash, &origBranch,
			&isRead,
		); err != nil {
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
		mn := MemoNotification{
			ID:         protocol.CreateRef(protocol.RefTypeCommit, hash, repoURL, branch),
			Type:       "memo-comment",
			RepoURL:    repoURL,
			Hash:       hash,
			Branch:     branch,
			Subject:    subject,
			ActorName:  actorName,
			ActorEmail: actorEmail,
			Timestamp:  timestamp,
			IsRead:     isRead == 1,
		}
		result = append(result, notifications.Notification{
			RepoURL:   repoURL,
			Hash:      hash,
			Branch:    branch,
			Type:      "memo-comment",
			Source:    "memo",
			Item:      mn,
			Actor:     notifications.Actor{Name: actorName, Email: actorEmail},
			ActorRepo: repoURL,
			Timestamp: timestamp,
			IsRead:    isRead == 1,
		})
	}
	return result, rows.Err()
}

// getInheritedPolicyNotifications surfaces priority/critical memos on inherited
// repos as binding-policy updates. The first run after `inherit add` will fire
// once per existing critical memo (read-tracking absorbs the noise); subsequent
// fires are only for newly-pushed criticals.
func getInheritedPolicyNotifications(workdir, userEmail string, filter notifications.Filter) ([]notifications.Notification, error) {
	inheritedURLs := ListInherits(workdir)
	if len(inheritedURLs) == 0 {
		return nil, nil
	}
	return cache.QueryLocked(func(db *sql.DB) ([]notifications.Notification, error) {
		ph := strings.Repeat("?,", len(inheritedURLs))
		ph = ph[:len(ph)-1]
		query := `
			SELECT v.repo_url, v.hash, v.branch,
			       v.author_name, v.author_email,
			       v.resolved_message, v.timestamp,
			       v.labels,
			       CASE WHEN r.repo_url IS NOT NULL THEN 1 ELSE 0 END
			FROM memo_items_resolved v
			LEFT JOIN core_notification_reads r
			  ON v.repo_url = r.repo_url
			 AND v.hash = r.hash
			 AND v.branch = r.branch
			WHERE v.repo_url IN (` + ph + `)
			  AND NOT v.is_edit_commit AND NOT v.is_retracted AND v.stale_since IS NULL
			  AND v.author_email != ?
			  AND ',' || COALESCE(v.labels, '') || ',' LIKE '%,priority/critical,%'`
		args := make([]interface{}, 0, len(inheritedURLs)+2)
		for _, u := range inheritedURLs {
			args = append(args, u)
		}
		args = append(args, userEmail)
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
		var result []notifications.Notification
		for rows.Next() {
			var repoURL, hash, branch, authorName, authorEmail string
			var message, ts, labels sql.NullString
			var isRead int
			if err := rows.Scan(
				&repoURL, &hash, &branch,
				&authorName, &authorEmail,
				&message, &ts, &labels, &isRead,
			); err != nil {
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
			var labelList []string
			if labels.Valid && labels.String != "" {
				for _, l := range strings.Split(labels.String, ",") {
					l = strings.TrimSpace(l)
					if l != "" {
						labelList = append(labelList, l)
					}
				}
			}
			mn := MemoNotification{
				ID:         protocol.CreateRef(protocol.RefTypeCommit, hash, repoURL, branch),
				Type:       "inherited-policy",
				RepoURL:    repoURL,
				Hash:       hash,
				Branch:     branch,
				Subject:    subject,
				Labels:     labelList,
				ActorName:  authorName,
				ActorEmail: authorEmail,
				Timestamp:  timestamp,
				IsRead:     isRead == 1,
			}
			result = append(result, notifications.Notification{
				RepoURL:   repoURL,
				Hash:      hash,
				Branch:    branch,
				Type:      "inherited-policy",
				Source:    "memo",
				Item:      mn,
				Actor:     notifications.Actor{Name: authorName, Email: authorEmail},
				ActorRepo: repoURL,
				Timestamp: timestamp,
				IsRead:    isRead == 1,
			})
		}
		return result, rows.Err()
	})
}
