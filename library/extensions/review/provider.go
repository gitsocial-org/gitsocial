// provider.go - Review notification provider for fork PRs and feedback
package review

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

type reviewNotificationProvider struct{}

func init() {
	notifications.RegisterProvider("review", &reviewNotificationProvider{})
}

// notificationSelectFromView is baseSelectFromView + LEFT JOIN for read state, with nr.repo_url appended.
const notificationSelectFromView = `
	SELECT v.repo_url, v.hash, v.branch,
	       v.author_name, v.author_email, v.resolved_message, v.timestamp,
	       v.type, v.state, v.base, v.head, v.closes, v.reviewers,
	       v.pull_request_repo_url, v.pull_request_hash, v.pull_request_branch,
	       v.commit_ref, v.file, v.old_line, v.new_line, v.old_line_end, v.new_line_end,
	       v.review_state, v.suggestion,
	       v.edits, v.is_virtual, v.is_retracted, v.has_edits,
	       v.comments,
	       nr.repo_url
	FROM review_items_resolved v
	LEFT JOIN core_notification_reads nr ON v.repo_url = nr.repo_url AND v.hash = nr.hash AND v.branch = nr.branch
`

// ReviewNotification holds review-specific notification data.
type ReviewNotification struct {
	ID          string
	Type        string // "fork-pr", "feedback", "approved", "changes-requested", "review-requested", "pr-merged", "pr-closed", "pr-ready"
	RepoURL     string
	Hash        string
	Branch      string
	PRSubject   string
	PRRepoURL   string
	PRHash      string
	PRBranch    string
	ActorName   string
	ActorEmail  string
	Timestamp   time.Time
	IsRead      bool
	ReviewState string
	Content     string
}

// GetNotifications returns review notifications (fork PRs + feedback on workspace PRs).
func (p *reviewNotificationProvider) GetNotifications(workdir string, filter notifications.Filter) ([]notifications.Notification, error) {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	if workspaceURL == "" {
		return nil, nil
	}
	userEmail := git.GetUserEmail(workdir)
	forks := GetForks(workdir)

	var result []notifications.Notification

	forkNotifs, err := getForkPRNotifications(workspaceURL, userEmail, forks, filter.UnreadOnly)
	if err == nil {
		result = append(result, forkNotifs...)
	}

	fbNotifs, err := getFeedbackNotifications(workspaceURL, userEmail, filter.UnreadOnly)
	if err == nil {
		result = append(result, fbNotifs...)
	}

	rrNotifs, err := getReviewRequestedNotifications(userEmail, filter.UnreadOnly)
	if err == nil {
		result = append(result, rrNotifs...)
	}

	scNotifs, err := getPRStateChangeNotifications(userEmail, filter.UnreadOnly)
	if err == nil {
		result = append(result, scNotifs...)
	}

	drNotifs, err := getDraftReadyNotifications(workspaceURL, userEmail, forks, filter.UnreadOnly)
	if err == nil {
		result = append(result, drNotifs...)
	}

	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

// GetUnreadCount returns the total unread review notification count.
func (p *reviewNotificationProvider) GetUnreadCount(workdir string) (int, error) {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	if workspaceURL == "" {
		return 0, nil
	}
	userEmail := git.GetUserEmail(workdir)
	forks := GetForks(workdir)

	forkCount, err := countUnreadForkPRs(workspaceURL, userEmail, forks)
	if err != nil {
		forkCount = 0
	}
	fbCount, err := countUnreadFeedback(workspaceURL, userEmail)
	if err != nil {
		fbCount = 0
	}
	rrCount, err := countUnreadReviewRequested(userEmail)
	if err != nil {
		rrCount = 0
	}
	scCount, err := countUnreadPRStateChanges(userEmail)
	if err != nil {
		scCount = 0
	}
	drCount, err := countUnreadDraftReady(workspaceURL, userEmail, forks)
	if err != nil {
		drCount = 0
	}
	return forkCount + fbCount + rrCount + scCount + drCount, nil
}

// getForkPRNotifications returns notifications for PRs created on fork repos targeting the workspace.
func getForkPRNotifications(workspaceURL, userEmail string, forkURLs []string, unreadOnly bool) ([]notifications.Notification, error) {
	if len(forkURLs) == 0 {
		return nil, nil
	}
	return cache.QueryLocked(func(db *sql.DB) ([]notifications.Notification, error) {
		ph := strings.Repeat("?,", len(forkURLs))
		ph = ph[:len(ph)-1]
		query := notificationSelectFromView + `
			WHERE v.type = 'pull-request'
			  AND v.repo_url IN (` + ph + `)
			  AND v.author_email != ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted
			  AND COALESCE(v.draft, 0) = 0`
		args := make([]interface{}, 0, len(forkURLs)+2)
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
			item, isRead, err := scanResolvedRowWithRead(rows)
			if err != nil {
				return nil, err
			}
			if seen[item.Hash] {
				continue
			}
			if !forkPRTargetsWorkspace(*item, workspaceURL) {
				continue
			}
			seen[item.Hash] = true
			subject, _ := protocol.SplitSubjectBody(item.Content)
			rn := ReviewNotification{
				ID:         protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch),
				Type:       "fork-pr",
				RepoURL:    item.RepoURL,
				Hash:       item.Hash,
				Branch:     item.Branch,
				PRSubject:  subject,
				ActorName:  item.AuthorName,
				ActorEmail: item.AuthorEmail,
				Timestamp:  item.Timestamp,
				IsRead:     isRead,
			}
			result = append(result, notifications.Notification{
				RepoURL:   item.RepoURL,
				Hash:      item.Hash,
				Branch:    item.Branch,
				Type:      "fork-pr",
				Source:    "review",
				Item:      rn,
				Actor:     notifications.Actor{Name: item.AuthorName, Email: item.AuthorEmail},
				ActorRepo: item.RepoURL,
				Timestamp: item.Timestamp,
				IsRead:    isRead,
			})
		}
		return result, rows.Err()
	})
}

// getFeedbackNotifications returns notifications for feedback on the user's PRs (workspace or authored).
func getFeedbackNotifications(workspaceURL, userEmail string, unreadOnly bool) ([]notifications.Notification, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]notifications.Notification, error) {
		query := notificationSelectFromView + `
			WHERE v.type = 'feedback'
			  AND (
			    v.pull_request_repo_url = ?
			    OR EXISTS (
			      SELECT 1 FROM review_items_resolved pr
			      WHERE pr.repo_url = v.pull_request_repo_url
			        AND pr.hash = v.pull_request_hash
			        AND pr.branch = v.pull_request_branch
			        AND pr.type = 'pull-request'
			        AND pr.author_email = ?
			    )
			  )
			  AND v.author_email != ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted`
		args := []interface{}{workspaceURL, userEmail, userEmail}
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
		for rows.Next() {
			item, isRead, err := scanResolvedRowWithRead(rows)
			if err != nil {
				return nil, err
			}
			notifType := "feedback"
			reviewState := nullStr(item.ReviewStateField)
			switch ReviewState(reviewState) {
			case ReviewStateApproved:
				notifType = "approved"
			case ReviewStateChangesRequested:
				notifType = "changes-requested"
			}
			subject, _ := protocol.SplitSubjectBody(item.Content)
			rn := ReviewNotification{
				ID:          protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch),
				Type:        notifType,
				RepoURL:     item.RepoURL,
				Hash:        item.Hash,
				Branch:      item.Branch,
				Content:     subject,
				PRRepoURL:   nullStr(item.PullRequestRepoURL),
				PRHash:      nullStr(item.PullRequestHash),
				PRBranch:    nullStr(item.PullRequestBranch),
				ActorName:   item.AuthorName,
				ActorEmail:  item.AuthorEmail,
				Timestamp:   item.Timestamp,
				IsRead:      isRead,
				ReviewState: reviewState,
			}
			result = append(result, notifications.Notification{
				RepoURL:   item.RepoURL,
				Hash:      item.Hash,
				Branch:    item.Branch,
				Type:      notifType,
				Source:    "review",
				Item:      rn,
				Actor:     notifications.Actor{Name: item.AuthorName, Email: item.AuthorEmail},
				ActorRepo: item.RepoURL,
				Timestamp: item.Timestamp,
				IsRead:    isRead,
			})
		}
		return result, rows.Err()
	})
}

// countUnreadForkPRs counts unread fork PR notifications.
func countUnreadForkPRs(workspaceURL, userEmail string, forkURLs []string) (int, error) {
	if len(forkURLs) == 0 {
		return 0, nil
	}
	// Use query + post-filter approach since we need to check base ref targeting
	notifs, err := getForkPRNotifications(workspaceURL, userEmail, forkURLs, true)
	if err != nil {
		return 0, err
	}
	return len(notifs), nil
}

// countUnreadFeedback counts unread feedback notifications.
func countUnreadFeedback(workspaceURL, userEmail string) (int, error) {
	return cache.QueryLocked(func(db *sql.DB) (int, error) {
		var count int
		err := db.QueryRow(`
			SELECT COUNT(*) FROM review_items_resolved v
			LEFT JOIN core_notification_reads nr ON v.repo_url = nr.repo_url AND v.hash = nr.hash AND v.branch = nr.branch
			WHERE v.type = 'feedback'
			  AND (
			    v.pull_request_repo_url = ?
			    OR EXISTS (
			      SELECT 1 FROM review_items_resolved pr
			      WHERE pr.repo_url = v.pull_request_repo_url
			        AND pr.hash = v.pull_request_hash
			        AND pr.branch = v.pull_request_branch
			        AND pr.type = 'pull-request'
			        AND pr.author_email = ?
			    )
			  )
			  AND v.author_email != ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted
			  AND nr.repo_url IS NULL
		`, workspaceURL, userEmail, userEmail).Scan(&count)
		return count, err
	})
}

// getReviewRequestedNotifications returns notifications for PRs where the user is a requested reviewer.
func getReviewRequestedNotifications(userEmail string, unreadOnly bool) ([]notifications.Notification, error) {
	if userEmail == "" {
		return nil, nil
	}
	return cache.QueryLocked(func(db *sql.DB) ([]notifications.Notification, error) {
		query := notificationSelectFromView + `
			WHERE v.type = 'pull-request'
			  AND v.state = 'open'
			  AND COALESCE(v.draft, 0) = 0
			  AND v.reviewers LIKE '%' || ? || '%' ESCAPE '\'
			  AND v.author_email != ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted`
		args := []interface{}{escapeLike(userEmail), userEmail}
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
		for rows.Next() {
			item, isRead, err := scanResolvedRowWithRead(rows)
			if err != nil {
				return nil, err
			}
			if !containsEmail(nullStr(item.Reviewers), userEmail) {
				continue
			}
			subject, _ := protocol.SplitSubjectBody(item.Content)
			rn := ReviewNotification{
				ID:         protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch),
				Type:       "review-requested",
				RepoURL:    item.RepoURL,
				Hash:       item.Hash,
				Branch:     item.Branch,
				PRSubject:  subject,
				ActorName:  item.AuthorName,
				ActorEmail: item.AuthorEmail,
				Timestamp:  item.Timestamp,
				IsRead:     isRead,
			}
			result = append(result, notifications.Notification{
				RepoURL:   item.RepoURL,
				Hash:      item.Hash,
				Branch:    item.Branch,
				Type:      "review-requested",
				Source:    "review",
				Item:      rn,
				Actor:     notifications.Actor{Name: item.AuthorName, Email: item.AuthorEmail},
				ActorRepo: item.RepoURL,
				Timestamp: item.Timestamp,
				IsRead:    isRead,
			})
		}
		return result, rows.Err()
	})
}

// countUnreadReviewRequested counts unread review-requested notifications.
func countUnreadReviewRequested(userEmail string) (int, error) {
	if userEmail == "" {
		return 0, nil
	}
	notifs, err := getReviewRequestedNotifications(userEmail, true)
	if err != nil {
		return 0, err
	}
	return len(notifs), nil
}

// getPRStateChangeNotifications returns notifications for PRs authored by the user that were merged/closed.
func getPRStateChangeNotifications(userEmail string, unreadOnly bool) ([]notifications.Notification, error) {
	if userEmail == "" {
		return nil, nil
	}
	return cache.QueryLocked(func(db *sql.DB) ([]notifications.Notification, error) {
		query := `
			SELECT ec.repo_url, ec.hash, ec.branch,
			       COALESCE(ec.origin_author_name, ec.author_name),
			       COALESCE(ec.origin_author_email, ec.author_email),
			       COALESCE(ec.origin_time, ec.timestamp),
			       ri.state,
			       pr.resolved_message,
			       pr.repo_url, pr.hash, pr.branch,
			       CASE WHEN nr.repo_url IS NOT NULL THEN 1 ELSE 0 END
			FROM core_commits_version cv
			JOIN core_commits ec ON cv.edit_repo_url = ec.repo_url AND cv.edit_hash = ec.hash AND cv.edit_branch = ec.branch
			JOIN review_items ri ON cv.edit_repo_url = ri.repo_url AND cv.edit_hash = ri.hash AND cv.edit_branch = ri.branch
			JOIN review_items_resolved pr ON cv.canonical_repo_url = pr.repo_url AND cv.canonical_hash = pr.hash AND cv.canonical_branch = pr.branch
			LEFT JOIN core_notification_reads nr ON ec.repo_url = nr.repo_url AND ec.hash = nr.hash AND ec.branch = nr.branch
			WHERE ri.state IN ('merged', 'closed')
			  AND pr.type = 'pull-request'
			  AND pr.author_email = ?
			  AND COALESCE(ec.origin_author_email, ec.author_email) != ?
			  AND NOT pr.is_retracted`
		args := []interface{}{userEmail, userEmail}
		if unreadOnly {
			query += " AND nr.repo_url IS NULL"
		}
		query += " ORDER BY ec.timestamp DESC"
		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var result []notifications.Notification
		for rows.Next() {
			var ecRepoURL, ecHash, ecBranch, actorName, actorEmail string
			var ts, state sql.NullString
			var prMessage sql.NullString
			var prRepoURL, prHash, prBranch string
			var isRead int
			if err := rows.Scan(
				&ecRepoURL, &ecHash, &ecBranch,
				&actorName, &actorEmail, &ts,
				&state, &prMessage,
				&prRepoURL, &prHash, &prBranch,
				&isRead,
			); err != nil {
				return nil, err
			}
			var timestamp time.Time
			if ts.Valid {
				timestamp, _ = time.Parse(time.RFC3339, ts.String)
			}
			notifType := "pr-merged"
			if state.String == "closed" {
				notifType = "pr-closed"
			}
			subject := ""
			if prMessage.Valid {
				content := protocol.ExtractCleanContent(prMessage.String)
				subject, _ = protocol.SplitSubjectBody(content)
			}
			rn := ReviewNotification{
				ID:         protocol.CreateRef(protocol.RefTypeCommit, ecHash, ecRepoURL, ecBranch),
				Type:       notifType,
				RepoURL:    ecRepoURL,
				Hash:       ecHash,
				Branch:     ecBranch,
				PRSubject:  subject,
				PRRepoURL:  prRepoURL,
				PRHash:     prHash,
				PRBranch:   prBranch,
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
				Source:    "review",
				Item:      rn,
				Actor:     notifications.Actor{Name: actorName, Email: actorEmail},
				ActorRepo: ecRepoURL,
				Timestamp: timestamp,
				IsRead:    isRead == 1,
			})
		}
		return result, rows.Err()
	})
}

// countUnreadPRStateChanges counts unread pr-merged/pr-closed notifications.
func countUnreadPRStateChanges(userEmail string) (int, error) {
	if userEmail == "" {
		return 0, nil
	}
	return cache.QueryLocked(func(db *sql.DB) (int, error) {
		var count int
		err := db.QueryRow(`
			SELECT COUNT(*)
			FROM core_commits_version cv
			JOIN core_commits ec ON cv.edit_repo_url = ec.repo_url AND cv.edit_hash = ec.hash AND cv.edit_branch = ec.branch
			JOIN review_items ri ON cv.edit_repo_url = ri.repo_url AND cv.edit_hash = ri.hash AND cv.edit_branch = ri.branch
			JOIN review_items_resolved pr ON cv.canonical_repo_url = pr.repo_url AND cv.canonical_hash = pr.hash AND cv.canonical_branch = pr.branch
			LEFT JOIN core_notification_reads nr ON ec.repo_url = nr.repo_url AND ec.hash = nr.hash AND ec.branch = nr.branch
			WHERE ri.state IN ('merged', 'closed')
			  AND pr.type = 'pull-request'
			  AND pr.author_email = ?
			  AND COALESCE(ec.origin_author_email, ec.author_email) != ?
			  AND NOT pr.is_retracted
			  AND nr.repo_url IS NULL
		`, userEmail, userEmail).Scan(&count)
		return count, err
	})
}

// getDraftReadyNotifications returns notifications for fork PRs that transitioned from draft to ready.
func getDraftReadyNotifications(workspaceURL, userEmail string, forkURLs []string, unreadOnly bool) ([]notifications.Notification, error) {
	if len(forkURLs) == 0 {
		return nil, nil
	}
	return cache.QueryLocked(func(db *sql.DB) ([]notifications.Notification, error) {
		ph := strings.Repeat("?,", len(forkURLs))
		ph = ph[:len(ph)-1]
		// Detect draft→ready transition: the edit sets draft=0, and the original
		// canonical commit's message contains "Draft: true" in the header.
		// We check the raw commit message instead of review_items because
		// syncExtensionFields copies the latest draft value to the canonical row.
		query := `
			SELECT ec.repo_url, ec.hash, ec.branch,
			       COALESCE(ec.origin_author_name, ec.author_name),
			       COALESCE(ec.origin_author_email, ec.author_email),
			       COALESCE(ec.origin_time, ec.timestamp),
			       pr.resolved_message,
			       pr.repo_url, pr.hash, pr.branch, pr.base,
			       CASE WHEN nr.repo_url IS NOT NULL THEN 1 ELSE 0 END
			FROM core_commits_version cv
			JOIN core_commits ec ON cv.edit_repo_url = ec.repo_url AND cv.edit_hash = ec.hash AND cv.edit_branch = ec.branch
			JOIN review_items ri_edit ON cv.edit_repo_url = ri_edit.repo_url AND cv.edit_hash = ri_edit.hash AND cv.edit_branch = ri_edit.branch
			JOIN core_commits c_canon ON cv.canonical_repo_url = c_canon.repo_url AND cv.canonical_hash = c_canon.hash AND cv.canonical_branch = c_canon.branch
			JOIN review_items_resolved pr ON cv.canonical_repo_url = pr.repo_url AND cv.canonical_hash = pr.hash AND cv.canonical_branch = pr.branch
			LEFT JOIN core_notification_reads nr ON ec.repo_url = nr.repo_url AND ec.hash = nr.hash AND ec.branch = nr.branch
			WHERE c_canon.message LIKE '%' || char(10) || 'Draft: true' || '%'
			  AND ri_edit.draft = 0
			  AND pr.type = 'pull-request'
			  AND pr.repo_url IN (` + ph + `)
			  AND COALESCE(ec.origin_author_email, ec.author_email) != ?
			  AND NOT pr.is_retracted`
		args := make([]interface{}, 0, len(forkURLs)+2)
		for _, u := range forkURLs {
			args = append(args, u)
		}
		args = append(args, userEmail)
		if unreadOnly {
			query += " AND nr.repo_url IS NULL"
		}
		query += " ORDER BY ec.timestamp DESC"
		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var result []notifications.Notification
		for rows.Next() {
			var ecRepoURL, ecHash, ecBranch, actorName, actorEmail string
			var ts sql.NullString
			var prMessage sql.NullString
			var prRepoURL, prHash, prBranch string
			var base sql.NullString
			var isRead int
			if err := rows.Scan(
				&ecRepoURL, &ecHash, &ecBranch,
				&actorName, &actorEmail, &ts,
				&prMessage,
				&prRepoURL, &prHash, &prBranch, &base,
				&isRead,
			); err != nil {
				return nil, err
			}
			item := ReviewItem{RepoURL: prRepoURL, Base: base}
			if !forkPRTargetsWorkspace(item, workspaceURL) {
				continue
			}
			var timestamp time.Time
			if ts.Valid {
				timestamp, _ = time.Parse(time.RFC3339, ts.String)
			}
			subject := ""
			if prMessage.Valid {
				content := protocol.ExtractCleanContent(prMessage.String)
				subject, _ = protocol.SplitSubjectBody(content)
			}
			rn := ReviewNotification{
				ID:         protocol.CreateRef(protocol.RefTypeCommit, ecHash, ecRepoURL, ecBranch),
				Type:       "pr-ready",
				RepoURL:    ecRepoURL,
				Hash:       ecHash,
				Branch:     ecBranch,
				PRSubject:  subject,
				PRRepoURL:  prRepoURL,
				PRHash:     prHash,
				PRBranch:   prBranch,
				ActorName:  actorName,
				ActorEmail: actorEmail,
				Timestamp:  timestamp,
				IsRead:     isRead == 1,
			}
			result = append(result, notifications.Notification{
				RepoURL:   ecRepoURL,
				Hash:      ecHash,
				Branch:    ecBranch,
				Type:      "pr-ready",
				Source:    "review",
				Item:      rn,
				Actor:     notifications.Actor{Name: actorName, Email: actorEmail},
				ActorRepo: ecRepoURL,
				Timestamp: timestamp,
				IsRead:    isRead == 1,
			})
		}
		return result, rows.Err()
	})
}

// countUnreadDraftReady counts unread draft-ready notifications.
func countUnreadDraftReady(workspaceURL, userEmail string, forkURLs []string) (int, error) {
	if len(forkURLs) == 0 {
		return 0, nil
	}
	notifs, err := getDraftReadyNotifications(workspaceURL, userEmail, forkURLs, true)
	if err != nil {
		return 0, err
	}
	return len(notifs), nil
}

// containsEmail checks if email appears as an exact match in a comma-separated list.
func containsEmail(list, email string) bool {
	for _, e := range strings.Split(list, ",") {
		if strings.TrimSpace(e) == email {
			return true
		}
	}
	return false
}

// escapeLike escapes LIKE special characters.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}

// scanResolvedRowWithRead scans a review_items_resolved row that has a LEFT JOIN with core_notification_reads.
func scanResolvedRowWithRead(rows *sql.Rows) (*ReviewItem, bool, error) {
	var item ReviewItem
	var ts, message sql.NullString
	var isVirtual, isRetracted, hasEdits int
	var readRepoURL sql.NullString
	err := rows.Scan(
		&item.RepoURL, &item.Hash, &item.Branch,
		&item.AuthorName, &item.AuthorEmail, &message, &ts,
		&item.Type, &item.State, &item.Base, &item.Head, &item.Closes, &item.Reviewers,
		&item.PullRequestRepoURL, &item.PullRequestHash, &item.PullRequestBranch,
		&item.CommitRef, &item.File, &item.OldLine, &item.NewLine, &item.OldLineEnd, &item.NewLineEnd,
		&item.ReviewStateField, &item.Suggestion,
		&item.EditOf, &isVirtual, &isRetracted, &hasEdits,
		&item.Comments,
		&readRepoURL,
	)
	if err != nil {
		return nil, false, err
	}
	if message.Valid {
		item.Content = protocol.ExtractCleanContent(message.String)
	}
	if ts.Valid {
		item.Timestamp, _ = time.Parse(time.RFC3339, ts.String)
	}
	item.IsVirtual = isVirtual == 1
	item.IsRetracted = isRetracted == 1
	item.IsEdited = hasEdits == 1
	isRead := readRepoURL.Valid
	return &item, isRead, nil
}
