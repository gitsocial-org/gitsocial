// provider.go - Review notification provider for fork PRs and feedback
package review

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

type reviewNotificationProvider struct{}

func init() {
	notifications.RegisterProvider("review", &reviewNotificationProvider{})
}

// notificationSelectFromView is baseSelectFromView with the read marker as a trailing column.
var notificationSelectFromView = cache.ResolvedSelect("review_items_resolved",
	reviewExtColumns+`,
       nr.repo_url`) + `
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
	forks := gitmsg.GetForks(workdir)

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

	drNotifs, err := getDraftReadyNotifications(workdir, workspaceURL, userEmail, forks, filter.UnreadOnly)
	if err == nil {
		result = append(result, drNotifs...)
	}

	result = append(result, getBranchStateNotifications(workdir, workspaceURL, userEmail, forks)...)

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
	forks := gitmsg.GetForks(workdir)

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
	drCount, err := countUnreadDraftReady(workdir, workspaceURL, userEmail, forks)
	if err != nil {
		drCount = 0
	}
	bsCount := len(getBranchStateNotifications(workdir, workspaceURL, userEmail, forks))
	return forkCount + fbCount + rrCount + scCount + drCount + bsCount, nil
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
	notifs, err := getFeedbackNotifications(workspaceURL, userEmail, true)
	if err != nil {
		return 0, err
	}
	return len(notifs), nil
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
	notifs, err := getPRStateChangeNotifications(userEmail, true)
	if err != nil {
		return 0, err
	}
	return len(notifs), nil
}

// getDraftReadyNotifications returns one notification per draft-to-ready edit on a fork pull request.
func getDraftReadyNotifications(workdir, workspaceURL, userEmail string, forkURLs []string, unreadOnly bool) ([]notifications.Notification, error) {
	if len(forkURLs) == 0 {
		return nil, nil
	}
	branch := gitmsg.GetExtBranch(workdir, "review")
	prRes := GetPullRequestsWithForks(workspaceURL, branch, forkURLs, nil, "", 0)
	if !prRes.Success {
		return nil, errors.New(prRes.Error.Message)
	}
	var result []notifications.Notification
	for _, pr := range prRes.Data {
		if pr.Repository == workspaceURL {
			continue
		}
		vRes := GetPRVersions(pr.ID, workspaceURL)
		if !vRes.Success {
			continue
		}
		for _, edit := range draftReadyEdits(vRes.Data) {
			notif, ok := draftReadyNotif(pr, edit, userEmail, unreadOnly)
			if ok {
				result = append(result, notif)
			}
		}
	}
	return result, nil
}

// draftReadyEdits returns the versions that clear draft where the version before them set it.
func draftReadyEdits(versions []PRVersion) []PRVersion {
	var out []PRVersion
	for i := 1; i < len(versions); i++ {
		if versions[i-1].Fields["draft"] == "true" && versions[i].Fields["draft"] != "true" && !versions[i].IsRetracted {
			out = append(out, versions[i])
		}
	}
	return out
}

// draftReadyNotif builds the pr-ready notification for one transition edit, keyed by the edit hash.
func draftReadyNotif(pr PullRequest, edit PRVersion, userEmail string, unreadOnly bool) (notifications.Notification, bool) {
	actorName, actorEmail := edit.AuthorName, edit.AuthorEmail
	if name := edit.Fields["origin-author-name"]; name != "" {
		actorName = name
	}
	if email := edit.Fields["origin-author-email"]; email != "" {
		actorEmail = email
	}
	if actorEmail == userEmail {
		return notifications.Notification{}, false
	}
	timestamp := edit.Timestamp
	if originTime, err := time.Parse(time.RFC3339, edit.Fields["origin-time"]); err == nil {
		timestamp = originTime
	}
	isRead := isNotificationRead(edit.RepoURL, edit.CommitHash, edit.Branch)
	if unreadOnly && isRead {
		return notifications.Notification{}, false
	}
	prParsed := protocol.ParseRef(pr.ID)
	rn := ReviewNotification{
		ID:         protocol.CreateRef(protocol.RefTypeCommit, edit.CommitHash, edit.RepoURL, edit.Branch),
		Type:       "pr-ready",
		RepoURL:    edit.RepoURL,
		Hash:       edit.CommitHash,
		Branch:     edit.Branch,
		PRSubject:  pr.Subject,
		PRRepoURL:  pr.Repository,
		PRHash:     prParsed.Value,
		PRBranch:   pr.Branch,
		ActorName:  actorName,
		ActorEmail: actorEmail,
		Timestamp:  timestamp,
		IsRead:     isRead,
	}
	return notifications.Notification{
		RepoURL:   edit.RepoURL,
		Hash:      edit.CommitHash,
		Branch:    edit.Branch,
		Type:      "pr-ready",
		Source:    "review",
		Item:      rn,
		Actor:     notifications.Actor{Name: actorName, Email: actorEmail},
		ActorRepo: edit.RepoURL,
		Timestamp: timestamp,
		IsRead:    isRead,
	}, true
}

// isNotificationRead reports whether a commit key carries a read marker.
func isNotificationRead(repoURL, hash, branch string) bool {
	read, err := cache.QueryLocked(func(db *sql.DB) (bool, error) {
		var count int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_notification_reads
			WHERE repo_url = ? AND hash = ? AND branch = ?`, repoURL, hash, branch).Scan(&count)
		return count > 0, err
	})
	return err == nil && read
}

// countUnreadDraftReady counts unread draft-ready notifications.
func countUnreadDraftReady(workdir, workspaceURL, userEmail string, forkURLs []string) (int, error) {
	notifs, err := getDraftReadyNotifications(workdir, workspaceURL, userEmail, forkURLs, true)
	if err != nil {
		return 0, err
	}
	return len(notifs), nil
}

// containsEmail reports whether email is an exact entry in a comma-separated list.
func containsEmail(list, email string) bool {
	return hasEmail(strings.Split(list, ","), email)
}

// hasEmail reports whether email is an exact entry, ignoring surrounding space.
func hasEmail(addresses []string, email string) bool {
	for _, a := range addresses {
		if strings.TrimSpace(a) == email {
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

// scanResolvedRowWithRead scans a notificationSelectFromView row and its read marker.
func scanResolvedRowWithRead(s cache.RowScanner) (*ReviewItem, bool, error) {
	var readRepoURL sql.NullString
	item, err := scanReviewRow(s, &readRepoURL)
	if err != nil {
		return nil, false, err
	}
	return item, readRepoURL.Valid, nil
}
