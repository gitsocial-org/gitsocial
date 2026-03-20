// provider.go - PM notification provider for issue assignments
package pm

import (
	"database/sql"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/notifications"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

type pmNotificationProvider struct{}

func init() {
	notifications.RegisterProvider("pm", &pmNotificationProvider{})
}

// PMNotification holds PM-specific notification data.
type PMNotification struct {
	ID         string
	Type       string // "issue-assigned", "fork-issue", etc.
	RepoURL    string
	Hash       string
	Branch     string
	Subject    string
	State      string
	ActorName  string
	ActorEmail string
	Timestamp  time.Time
	IsRead     bool
}

// GetNotifications returns PM notifications (issues assigned to current user).
func (p *pmNotificationProvider) GetNotifications(workdir string, filter notifications.Filter) ([]notifications.Notification, error) {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	if workspaceURL == "" {
		return nil, nil
	}
	userEmail := git.GetUserEmail(workdir)
	if userEmail == "" {
		return nil, nil
	}
	forks := gitmsg.GetForks(workdir)
	var result []notifications.Notification
	forkIssues, err := getForkIssueNotifications(userEmail, forks, filter.UnreadOnly)
	if err == nil {
		result = append(result, forkIssues...)
	}
	assigned, err := getAssignedIssueNotifications(userEmail, filter.UnreadOnly, filter.Limit)
	if err == nil {
		result = append(result, assigned...)
	}
	stateChanges, err := getIssueStateChangeNotifications(userEmail, filter.UnreadOnly, filter.Limit)
	if err == nil {
		result = append(result, stateChanges...)
	}
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

// GetUnreadCount returns the total unread PM notification count.
func (p *pmNotificationProvider) GetUnreadCount(workdir string) (int, error) {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	if workspaceURL == "" {
		return 0, nil
	}
	userEmail := git.GetUserEmail(workdir)
	if userEmail == "" {
		return 0, nil
	}
	forks := gitmsg.GetForks(workdir)
	forkIssues, _ := getForkIssueNotifications(userEmail, forks, true)
	assigned, err := getAssignedIssueNotifications(userEmail, true, 0)
	if err != nil {
		return 0, err
	}
	stateChanges, err := getIssueStateChangeNotifications(userEmail, true, 0)
	if err != nil {
		return 0, err
	}
	return len(forkIssues) + len(assigned) + len(stateChanges), nil
}

// getForkIssueNotifications returns notifications for new issues filed from registered forks.
func getForkIssueNotifications(userEmail string, forkURLs []string, unreadOnly bool) ([]notifications.Notification, error) {
	if len(forkURLs) == 0 {
		return nil, nil
	}
	return cache.QueryLocked(func(db *sql.DB) ([]notifications.Notification, error) {
		ph := strings.Repeat("?,", len(forkURLs))
		ph = ph[:len(ph)-1]
		query := `
			SELECT v.repo_url, v.hash, v.branch,
			       v.author_name, v.author_email, v.resolved_message, v.timestamp, v.state,
			       nr.repo_url
			FROM pm_items_resolved v
			LEFT JOIN core_notification_reads nr ON v.repo_url = nr.repo_url AND v.hash = nr.hash AND v.branch = nr.branch
			WHERE v.type = 'issue'
			  AND v.repo_url IN (` + ph + `)
			  AND v.author_email != ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted`
		args := make([]interface{}, 0, len(forkURLs)+1)
		for _, u := range forkURLs {
			args = append(args, u)
		}
		args = append(args, userEmail)
		if unreadOnly {
			query += " AND nr.repo_url IS NULL"
		}
		query += " ORDER BY v.timestamp DESC"
		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var result []notifications.Notification
		seen := map[string]bool{}
		for rows.Next() {
			var repoURL, hash, branch, authorName, authorEmail string
			var message, ts, state sql.NullString
			var readRepoURL sql.NullString
			if err := rows.Scan(
				&repoURL, &hash, &branch,
				&authorName, &authorEmail, &message, &ts, &state,
				&readRepoURL,
			); err != nil {
				return nil, err
			}
			if seen[hash] {
				continue
			}
			seen[hash] = true
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
			pn := PMNotification{
				ID:         protocol.CreateRef(protocol.RefTypeCommit, hash, repoURL, branch),
				Type:       "fork-issue",
				RepoURL:    repoURL,
				Hash:       hash,
				Branch:     branch,
				Subject:    subject,
				State:      state.String,
				ActorName:  authorName,
				ActorEmail: authorEmail,
				Timestamp:  timestamp,
				IsRead:     isRead,
			}
			result = append(result, notifications.Notification{
				RepoURL:   repoURL,
				Hash:      hash,
				Branch:    branch,
				Type:      "fork-issue",
				Source:    "pm",
				Item:      pn,
				Actor:     notifications.Actor{Name: authorName, Email: authorEmail},
				ActorRepo: repoURL,
				Timestamp: timestamp,
				IsRead:    isRead,
			})
		}
		return result, rows.Err()
	})
}

// getAssignedIssueNotifications returns notifications for issues assigned to the user.
func getAssignedIssueNotifications(userEmail string, unreadOnly bool, limit int) ([]notifications.Notification, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]notifications.Notification, error) {
		query := `
			SELECT v.repo_url, v.hash, v.branch,
			       v.author_name, v.author_email, v.resolved_message, v.timestamp,
			       v.state, v.assignees,
			       nr.repo_url
			FROM pm_items_resolved v
			LEFT JOIN core_notification_reads nr ON v.repo_url = nr.repo_url AND v.hash = nr.hash AND v.branch = nr.branch
			WHERE v.type = 'issue'
			  AND v.assignees LIKE '%' || ? || '%' ESCAPE '\'
			  AND v.author_email != ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted`
		args := []interface{}{escapeLike(userEmail), userEmail}
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
			var message, ts, state, assignees sql.NullString
			var readRepoURL sql.NullString
			err := rows.Scan(
				&repoURL, &hash, &branch,
				&authorName, &authorEmail, &message, &ts,
				&state, &assignees,
				&readRepoURL,
			)
			if err != nil {
				return nil, err
			}
			// Post-filter: exact email match in comma-separated assignees
			if !containsEmail(assignees.String, userEmail) {
				continue
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
			pn := PMNotification{
				ID:         protocol.CreateRef(protocol.RefTypeCommit, hash, repoURL, branch),
				Type:       "issue-assigned",
				RepoURL:    repoURL,
				Hash:       hash,
				Branch:     branch,
				Subject:    subject,
				State:      state.String,
				ActorName:  authorName,
				ActorEmail: authorEmail,
				Timestamp:  timestamp,
				IsRead:     isRead,
			}
			result = append(result, notifications.Notification{
				RepoURL:   repoURL,
				Hash:      hash,
				Branch:    branch,
				Type:      "issue-assigned",
				Source:    "pm",
				Item:      pn,
				Actor:     notifications.Actor{Name: authorName, Email: authorEmail},
				ActorRepo: repoURL,
				Timestamp: timestamp,
				IsRead:    isRead,
			})
		}
		return result, rows.Err()
	})
}

// getIssueStateChangeNotifications returns notifications for state changes on issues assigned to the user.
func getIssueStateChangeNotifications(userEmail string, unreadOnly bool, limit int) ([]notifications.Notification, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]notifications.Notification, error) {
		query := `
			SELECT ec.repo_url, ec.hash, ec.branch,
			       COALESCE(ec.origin_author_name, ec.author_name),
			       COALESCE(ec.origin_author_email, ec.author_email),
			       COALESCE(ec.origin_time, ec.timestamp),
			       pi.state,
			       pr.resolved_message, pr.assignees,
			       CASE WHEN nr.repo_url IS NOT NULL THEN 1 ELSE 0 END
			FROM core_commits_version cv
			JOIN core_commits ec ON cv.edit_repo_url = ec.repo_url AND cv.edit_hash = ec.hash AND cv.edit_branch = ec.branch
			JOIN pm_items pi ON cv.edit_repo_url = pi.repo_url AND cv.edit_hash = pi.hash AND cv.edit_branch = pi.branch
			JOIN pm_items_resolved pr ON cv.canonical_repo_url = pr.repo_url AND cv.canonical_hash = pr.hash AND cv.canonical_branch = pr.branch
			LEFT JOIN core_notification_reads nr ON ec.repo_url = nr.repo_url AND ec.hash = nr.hash AND ec.branch = nr.branch
			WHERE pi.state IN ('closed', 'canceled', 'open')
			  AND pr.type = 'issue'
			  AND pr.assignees LIKE '%' || ? || '%' ESCAPE '\'
			  AND COALESCE(ec.origin_author_email, ec.author_email) != ?
			  AND NOT pr.is_retracted`
		args := []interface{}{escapeLike(userEmail), userEmail}
		if unreadOnly {
			query += " AND nr.repo_url IS NULL"
		}
		query += " ORDER BY ec.timestamp DESC"
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
			var ecRepoURL, ecHash, ecBranch, actorName, actorEmail string
			var ts, state, message, assignees sql.NullString
			var isRead int
			if err := rows.Scan(
				&ecRepoURL, &ecHash, &ecBranch,
				&actorName, &actorEmail, &ts,
				&state, &message, &assignees,
				&isRead,
			); err != nil {
				return nil, err
			}
			if !containsEmail(assignees.String, userEmail) {
				continue
			}
			var timestamp time.Time
			if ts.Valid {
				timestamp, _ = time.Parse(time.RFC3339, ts.String)
			}
			notifType := "issue-closed"
			if state.String == "open" {
				notifType = "issue-reopened"
			}
			subject := ""
			if message.Valid {
				content := protocol.ExtractCleanContent(message.String)
				subject, _ = protocol.SplitSubjectBody(content)
			}
			pn := PMNotification{
				ID:         protocol.CreateRef(protocol.RefTypeCommit, ecHash, ecRepoURL, ecBranch),
				Type:       notifType,
				RepoURL:    ecRepoURL,
				Hash:       ecHash,
				Branch:     ecBranch,
				Subject:    subject,
				State:      state.String,
				ActorName:  actorName,
				ActorEmail: actorEmail,
				Timestamp:  timestamp,
				IsRead:     isRead == 1,
			}
			result = append(result, notifications.Notification{
				RepoURL:   ecRepoURL,
				Hash:      ecHash,
				Branch:    ecBranch,
				Type:      notifType,
				Source:    "pm",
				Item:      pn,
				Actor:     notifications.Actor{Name: actorName, Email: actorEmail},
				ActorRepo: ecRepoURL,
				Timestamp: timestamp,
				IsRead:    isRead == 1,
			})
		}
		return result, rows.Err()
	})
}

// containsEmail checks if email appears as an exact match in a comma-separated list.
func containsEmail(assignees, email string) bool {
	for _, a := range strings.Split(assignees, ",") {
		if strings.TrimSpace(a) == email {
			return true
		}
	}
	return false
}
