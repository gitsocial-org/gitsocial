// items.go - Review item queries and cache operations
package review

import (
	"database/sql"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/result"
)

type ReviewItem struct {
	RepoURL            string
	Hash               string
	Branch             string
	Type               string
	State              sql.NullString
	Draft              int
	Base               sql.NullString
	BaseTip            sql.NullString
	Head               sql.NullString
	HeadTip            sql.NullString
	Closes             sql.NullString
	Reviewers          sql.NullString
	PullRequestRepoURL sql.NullString
	PullRequestHash    sql.NullString
	PullRequestBranch  sql.NullString
	CommitRef          sql.NullString
	File               sql.NullString
	OldLine            sql.NullInt64
	NewLine            sql.NullInt64
	OldLineEnd         sql.NullInt64
	NewLineEnd         sql.NullInt64
	ReviewStateField   sql.NullString
	Suggestion         int
	Labels             sql.NullString
	// Derived from core_commits via JOIN
	Origin      *protocol.Origin
	Content     string
	AuthorName  string
	AuthorEmail string
	Timestamp   time.Time
	EditOf      sql.NullString
	IsRetracted bool
	IsEdited    bool
	IsVirtual   bool
	// Derived from social_interactions
	Comments int
	// Parsed from commit message GitMsg-Ref sections
	References []protocol.Ref
}

const baseSelectFromView = `
	SELECT v.repo_url, v.hash, v.branch,
	       v.author_name, v.author_email, v.resolved_message, v.original_message, v.timestamp,
	       v.type, v.state, v.draft, v.base, v.base_tip, v.head, v.head_tip, v.closes, v.reviewers,
	       v.pull_request_repo_url, v.pull_request_hash, v.pull_request_branch,
	       v.commit_ref, v.file, v.old_line, v.new_line, v.old_line_end, v.new_line_end,
	       v.review_state, v.suggestion,
	       v.labels,
	       v.edits, v.is_virtual, v.is_retracted, v.has_edits,
	       v.comments
	FROM review_items_resolved v
`

// InsertReviewItem inserts or updates a review item in the cache database.
func InsertReviewItem(item ReviewItem) error {
	return cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`
			INSERT INTO review_items
			(repo_url, hash, branch, type, state, draft, base, base_tip, head, head_tip, closes, reviewers,
			 pull_request_repo_url, pull_request_hash, pull_request_branch,
			 commit_ref, file, old_line, new_line, old_line_end, new_line_end, review_state, suggestion)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(repo_url, hash, branch) DO UPDATE SET
				type = excluded.type,
				state = excluded.state,
				draft = excluded.draft,
				base = excluded.base,
				base_tip = excluded.base_tip,
				head = excluded.head,
				head_tip = excluded.head_tip,
				closes = excluded.closes,
				reviewers = excluded.reviewers,
				pull_request_repo_url = excluded.pull_request_repo_url,
				pull_request_hash = excluded.pull_request_hash,
				pull_request_branch = excluded.pull_request_branch,
				commit_ref = excluded.commit_ref,
				file = excluded.file,
				old_line = excluded.old_line,
				new_line = excluded.new_line,
				old_line_end = excluded.old_line_end,
				new_line_end = excluded.new_line_end,
				review_state = excluded.review_state,
				suggestion = excluded.suggestion`,
			item.RepoURL, item.Hash, item.Branch,
			item.Type, item.State, item.Draft, item.Base, item.BaseTip, item.Head, item.HeadTip, item.Closes, item.Reviewers,
			item.PullRequestRepoURL, item.PullRequestHash, item.PullRequestBranch,
			item.CommitRef, item.File, item.OldLine, item.NewLine, item.OldLineEnd, item.NewLineEnd,
			item.ReviewStateField, item.Suggestion,
		)
		return err
	})
}

// InsertReviewItems batch-inserts multiple review items in a single transaction.
func InsertReviewItems(items []ReviewItem) error {
	if len(items) == 0 {
		return nil
	}
	return cache.ExecLocked(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		stmt, err := tx.Prepare(`
			INSERT INTO review_items
			(repo_url, hash, branch, type, state, draft, base, base_tip, head, head_tip, closes, reviewers,
			 pull_request_repo_url, pull_request_hash, pull_request_branch,
			 commit_ref, file, old_line, new_line, old_line_end, new_line_end, review_state, suggestion)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(repo_url, hash, branch) DO UPDATE SET
				type = excluded.type,
				state = excluded.state,
				draft = excluded.draft,
				base = excluded.base,
				base_tip = excluded.base_tip,
				head = excluded.head,
				head_tip = excluded.head_tip,
				closes = excluded.closes,
				reviewers = excluded.reviewers,
				pull_request_repo_url = excluded.pull_request_repo_url,
				pull_request_hash = excluded.pull_request_hash,
				pull_request_branch = excluded.pull_request_branch,
				commit_ref = excluded.commit_ref,
				file = excluded.file,
				old_line = excluded.old_line,
				new_line = excluded.new_line,
				old_line_end = excluded.old_line_end,
				new_line_end = excluded.new_line_end,
				review_state = excluded.review_state,
				suggestion = excluded.suggestion`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, item := range items {
			_, err := stmt.Exec(
				item.RepoURL, item.Hash, item.Branch,
				item.Type, item.State, item.Draft, item.Base, item.BaseTip, item.Head, item.HeadTip, item.Closes, item.Reviewers,
				item.PullRequestRepoURL, item.PullRequestHash, item.PullRequestBranch,
				item.CommitRef, item.File, item.OldLine, item.NewLine, item.OldLineEnd, item.NewLineEnd,
				item.ReviewStateField, item.Suggestion,
			)
			if err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

// GetReviewItem retrieves a single review item by its composite key.
func GetReviewItem(repoURL, hash, branch string) (*ReviewItem, error) {
	return cache.QueryLocked(func(db *sql.DB) (*ReviewItem, error) {
		query := baseSelectFromView + `
			WHERE v.repo_url = ? AND v.hash = ? AND v.branch = ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted`
		row := db.QueryRow(query, repoURL, hash, branch)
		return scanResolvedRow(row)
	})
}

// GetReviewItemByRef looks up a review item by its ref string.
func GetReviewItemByRef(refStr string, defaultRepoURL string) (*ReviewItem, error) {
	ref := protocol.ResolveRefWithDefaults(refStr, defaultRepoURL, "gitmsg/review")
	if ref.Hash == "" {
		return nil, sql.ErrNoRows
	}
	return GetReviewItem(ref.RepoURL, ref.Hash, ref.Branch)
}

type ReviewQuery struct {
	Types     []string
	States    []string
	RepoURL   string
	Branch    string
	Reviewer  string
	PRRef     string // filter feedback by pull-request composite key
	PRRepoURL string
	PRHash    string
	PRBranch  string
	Limit     int
	Offset    int
	Cursor    string // RFC3339 timestamp — items older than this (keyset pagination)
	SortField string
	SortOrder string
}

// GetReviewItems queries review items with filtering and pagination.
func GetReviewItems(q ReviewQuery) ([]ReviewItem, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]ReviewItem, error) {
		var args []interface{}
		var where []string

		if len(q.Types) > 0 {
			ph := strings.Repeat("?,", len(q.Types))
			ph = ph[:len(ph)-1]
			where = append(where, "v.type IN ("+ph+")")
			for _, t := range q.Types {
				args = append(args, t)
			}
		}

		if len(q.States) > 0 {
			ph := strings.Repeat("?,", len(q.States))
			ph = ph[:len(ph)-1]
			where = append(where, "v.state IN ("+ph+")")
			for _, s := range q.States {
				args = append(args, s)
			}
		}

		if q.RepoURL != "" {
			where = append(where, "v.repo_url = ?")
			args = append(args, q.RepoURL)
		}

		if q.Branch != "" {
			where = append(where, "v.branch = ?")
			args = append(args, q.Branch)
		}

		if q.Reviewer != "" {
			where = append(where, "v.reviewers LIKE ?")
			args = append(args, "%"+q.Reviewer+"%")
		}

		if q.PRRepoURL != "" && q.PRHash != "" {
			where = append(where, "v.pull_request_repo_url = ? AND v.pull_request_hash = ?")
			args = append(args, q.PRRepoURL, q.PRHash)
			if q.PRBranch != "" {
				where = append(where, "v.pull_request_branch = ?")
				args = append(args, q.PRBranch)
			}
		}

		if q.Cursor != "" {
			where = append(where, "v.timestamp < ?")
			args = append(args, q.Cursor)
		}

		where = append(where, "NOT v.is_edit_commit")
		where = append(where, "NOT v.is_retracted")

		sqlQuery := baseSelectFromView
		if len(where) > 0 {
			sqlQuery += " WHERE " + strings.Join(where, " AND ")
		}

		order := "v.timestamp DESC"
		if q.SortOrder == "asc" {
			order = "v.timestamp ASC"
		}
		sqlQuery += " ORDER BY " + order

		if q.Limit > 0 {
			sqlQuery += " LIMIT ?"
			args = append(args, q.Limit)
		}
		if q.Offset > 0 && q.Cursor == "" {
			sqlQuery += " OFFSET ?"
			args = append(args, q.Offset)
		}

		rows, err := db.Query(sqlQuery, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var items []ReviewItem
		for rows.Next() {
			item, err := scanResolvedRows(rows)
			if err != nil {
				return nil, err
			}
			items = append(items, *item)
		}
		return items, rows.Err()
	})
}

// CountPullRequests returns the number of pull requests matching the given states.
func CountPullRequests(states []string) (int, error) {
	return cache.QueryLocked(func(db *sql.DB) (int, error) {
		query := `SELECT COUNT(*) FROM review_items_resolved v
			WHERE v.type = 'pull-request' AND NOT v.is_edit_commit AND NOT v.is_retracted`
		var args []interface{}
		if len(states) > 0 {
			ph := strings.Repeat("?,", len(states))
			query += " AND v.state IN (" + ph[:len(ph)-1] + ")"
			for _, s := range states {
				args = append(args, s)
			}
		}
		var count int
		err := db.QueryRow(query, args...).Scan(&count)
		return count, err
	})
}

// GetPullRequests retrieves pull requests with optional filtering.
func GetPullRequests(repoURL, branch string, states []string, cursor string, limit int) Result[[]PullRequest] {
	q := ReviewQuery{
		Types:   []string{string(ItemTypePullRequest)},
		States:  states,
		RepoURL: repoURL,
		Branch:  branch,
		Cursor:  cursor,
		Limit:   limit,
	}
	items, err := GetReviewItems(q)
	if err != nil {
		return result.Err[[]PullRequest]("QUERY_FAILED", err.Error())
	}
	prs := make([]PullRequest, len(items))
	for i, item := range items {
		prs[i] = ReviewItemToPullRequest(item)
	}
	return result.Ok(prs)
}

// CountPRsWithForks counts PRs from workspace and forks (matches GetPullRequestsWithForks logic).
func CountPRsWithForks(workspaceURL, workspaceBranch string, forkURLs, states []string) int {
	if len(forkURLs) == 0 {
		count, _ := CountPullRequests(states)
		return count
	}
	// With forks, we need to query+filter like GetPullRequestsWithForks does
	res := GetPullRequestsWithForks(workspaceURL, workspaceBranch, forkURLs, states, "", 0)
	if !res.Success {
		return 0
	}
	return len(res.Data)
}

// GetPullRequestsWithForks retrieves PRs from the workspace and registered forks.
// Fork PRs are post-filtered to only include those targeting the workspace.
func GetPullRequestsWithForks(workspaceURL, workspaceBranch string, forkURLs, states []string, cursor string, limit int) Result[[]PullRequest] {
	if len(forkURLs) == 0 {
		return GetPullRequests(workspaceURL, workspaceBranch, states, cursor, limit)
	}
	repoURLs := append([]string{workspaceURL}, forkURLs...)
	items, err := cache.QueryLocked(func(db *sql.DB) ([]ReviewItem, error) {
		ph := strings.Repeat("?,", len(repoURLs))
		ph = ph[:len(ph)-1]
		var args []interface{}
		var where []string
		where = append(where, "v.type = ?")
		args = append(args, string(ItemTypePullRequest))
		where = append(where, "v.repo_url IN ("+ph+")")
		for _, u := range repoURLs {
			args = append(args, u)
		}
		if len(states) > 0 {
			sph := strings.Repeat("?,", len(states))
			sph = sph[:len(sph)-1]
			where = append(where, "v.state IN ("+sph+")")
			for _, s := range states {
				args = append(args, s)
			}
		}
		if cursor != "" {
			where = append(where, "v.timestamp < ?")
			args = append(args, cursor)
		}
		where = append(where, "NOT v.is_edit_commit")
		where = append(where, "NOT v.is_retracted")
		sqlQuery := baseSelectFromView + " WHERE " + strings.Join(where, " AND ") + " ORDER BY v.timestamp DESC"
		if limit > 0 {
			sqlQuery += " LIMIT ?"
			args = append(args, limit)
		}
		rows, err := db.Query(sqlQuery, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var result []ReviewItem
		for rows.Next() {
			item, err := scanResolvedRows(rows)
			if err != nil {
				return nil, err
			}
			result = append(result, *item)
		}
		return result, rows.Err()
	})
	if err != nil {
		return result.Err[[]PullRequest]("QUERY_FAILED", err.Error())
	}
	prs := make([]PullRequest, 0, len(items))
	for _, item := range items {
		if item.RepoURL != workspaceURL && !forkPRTargetsWorkspace(item, workspaceURL) {
			continue
		}
		prs = append(prs, ReviewItemToPullRequest(item))
	}
	return result.Ok(prs)
}

// forkPRTargetsWorkspace checks if a fork PR's base ref targets the workspace.
// A local ref (#branch:main) means it targets the upstream workspace.
// An explicit repo ref is checked against the workspace URL.
func forkPRTargetsWorkspace(item ReviewItem, workspaceURL string) bool {
	base := nullStr(item.Base)
	if base == "" {
		return false
	}
	parsed := protocol.ParseRef(base)
	if parsed.Repository == "" {
		return true
	}
	return parsed.Repository == workspaceURL
}

// GetFeedbackForPR retrieves all feedback for a specific pull request.
func GetFeedbackForPR(prRepoURL, prHash, prBranch string) Result[[]Feedback] {
	q := ReviewQuery{
		Types:     []string{string(ItemTypeFeedback)},
		PRRepoURL: prRepoURL,
		PRHash:    prHash,
		PRBranch:  prBranch,
	}
	items, err := GetReviewItems(q)
	if err != nil {
		return result.Err[[]Feedback]("QUERY_FAILED", err.Error())
	}
	feedback := make([]Feedback, len(items))
	for i, item := range items {
		feedback[i] = ReviewItemToFeedback(item)
	}
	return result.Ok(feedback)
}

// StateChangeInfo holds author, timestamp, and metadata for a PR state transition.
type StateChangeInfo struct {
	AuthorName  string
	AuthorEmail string
	Timestamp   time.Time
	MergeBase   string
	MergeHead   string
}

// GetStateChangeInfo finds who triggered a state change (merged/closed) for a PR.
func GetStateChangeInfo(repoURL, hash, branch string, state PRState) (*StateChangeInfo, error) {
	return cache.QueryLocked(func(db *sql.DB) (*StateChangeInfo, error) {
		var name, email, ts, message string
		err := db.QueryRow(`
			SELECT c.author_name, c.author_email, c.timestamp, c.message
			FROM core_commits_version v
			JOIN core_commits c ON v.edit_repo_url = c.repo_url AND v.edit_hash = c.hash AND v.edit_branch = c.branch
			JOIN review_items ri ON v.edit_repo_url = ri.repo_url AND v.edit_hash = ri.hash AND v.edit_branch = ri.branch
			WHERE v.canonical_repo_url = ? AND v.canonical_hash = ? AND v.canonical_branch = ?
			AND ri.state = ?
			ORDER BY c.timestamp DESC
			LIMIT 1`,
			repoURL, hash, branch, string(state),
		).Scan(&name, &email, &ts, &message)
		if err != nil {
			return nil, err
		}
		t, _ := time.Parse(time.RFC3339, ts)
		info := &StateChangeInfo{AuthorName: name, AuthorEmail: email, Timestamp: t}
		if msg := protocol.ParseMessage(message); msg != nil {
			info.MergeBase = msg.Header.Fields["merge-base"]
			info.MergeHead = msg.Header.Fields["merge-head"]
			if originName := msg.Header.Fields["origin-author-name"]; originName != "" {
				info.AuthorName = originName
			}
			if originEmail := msg.Header.Fields["origin-author-email"]; originEmail != "" {
				info.AuthorEmail = originEmail
			}
			if originTime := msg.Header.Fields["origin-time"]; originTime != "" {
				if ot, err := time.Parse(time.RFC3339, originTime); err == nil {
					info.Timestamp = ot
				}
			}
		}
		return info, nil
	})
}

func scanResolvedRow(row *sql.Row) (*ReviewItem, error) {
	var item ReviewItem
	var ts, message, originalMessage sql.NullString
	var isVirtual, isRetracted, hasEdits int
	err := row.Scan(
		&item.RepoURL, &item.Hash, &item.Branch,
		&item.AuthorName, &item.AuthorEmail, &message, &originalMessage, &ts,
		&item.Type, &item.State, &item.Draft, &item.Base, &item.BaseTip, &item.Head, &item.HeadTip, &item.Closes, &item.Reviewers,
		&item.PullRequestRepoURL, &item.PullRequestHash, &item.PullRequestBranch,
		&item.CommitRef, &item.File, &item.OldLine, &item.NewLine, &item.OldLineEnd, &item.NewLineEnd,
		&item.ReviewStateField, &item.Suggestion,
		&item.Labels,
		&item.EditOf, &isVirtual, &isRetracted, &hasEdits,
		&item.Comments,
	)
	if err != nil {
		return nil, err
	}
	populateFromMessages(&item, message, originalMessage, ts, isVirtual, isRetracted, hasEdits)
	return &item, nil
}

func scanResolvedRows(rows *sql.Rows) (*ReviewItem, error) {
	var item ReviewItem
	var ts, message, originalMessage sql.NullString
	var isVirtual, isRetracted, hasEdits int
	err := rows.Scan(
		&item.RepoURL, &item.Hash, &item.Branch,
		&item.AuthorName, &item.AuthorEmail, &message, &originalMessage, &ts,
		&item.Type, &item.State, &item.Draft, &item.Base, &item.BaseTip, &item.Head, &item.HeadTip, &item.Closes, &item.Reviewers,
		&item.PullRequestRepoURL, &item.PullRequestHash, &item.PullRequestBranch,
		&item.CommitRef, &item.File, &item.OldLine, &item.NewLine, &item.OldLineEnd, &item.NewLineEnd,
		&item.ReviewStateField, &item.Suggestion,
		&item.Labels,
		&item.EditOf, &isVirtual, &isRetracted, &hasEdits,
		&item.Comments,
	)
	if err != nil {
		return nil, err
	}
	populateFromMessages(&item, message, originalMessage, ts, isVirtual, isRetracted, hasEdits)
	return &item, nil
}

func populateFromMessages(item *ReviewItem, message, originalMessage, ts sql.NullString, isVirtual, isRetracted, hasEdits int) {
	if message.Valid {
		if msg := protocol.ParseMessage(message.String); msg != nil {
			item.Content = msg.Content
			item.References = msg.References
		} else {
			item.Content = protocol.ExtractCleanContent(message.String)
		}
	}
	if originalMessage.Valid {
		if msg := protocol.ParseMessage(originalMessage.String); msg != nil {
			item.Origin = protocol.ExtractOrigin(&msg.Header)
		}
	}
	if ts.Valid {
		item.Timestamp, _ = time.Parse(time.RFC3339, ts.String)
	}
	item.IsVirtual = isVirtual == 1
	item.IsRetracted = isRetracted == 1
	item.IsEdited = hasEdits == 1
}

// ReviewItemToPullRequest converts a ReviewItem to a PullRequest.
func ReviewItemToPullRequest(item ReviewItem) PullRequest {
	subject, body := protocol.SplitSubjectBody(item.Content)
	id := protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch)
	pr := PullRequest{
		ID:         id,
		Repository: item.RepoURL,
		Branch:     item.Branch,
		Author: Author{
			Name:  item.AuthorName,
			Email: item.AuthorEmail,
		},
		Timestamp:   item.Timestamp,
		Subject:     subject,
		Body:        body,
		State:       PRState(nullStr(item.State)),
		IsDraft:     item.Draft == 1,
		Base:        nullStr(item.Base),
		BaseTip:     nullStr(item.BaseTip),
		Head:        nullStr(item.Head),
		HeadTip:     nullStr(item.HeadTip),
		Closes:      parseCSV(nullStr(item.Closes)),
		Reviewers:   parseCSV(nullStr(item.Reviewers)),
		Labels:      parseCSV(nullStr(item.Labels)),
		IsEdited:    item.IsEdited,
		IsRetracted: item.IsRetracted,
		Comments:    item.Comments,
	}
	pr.Origin = item.Origin
	for _, ref := range item.References {
		if ref.Ext == "review" && ref.Fields["type"] == string(ItemTypePullRequest) {
			pr.OriginalAuthor = &Author{Name: ref.Author, Email: ref.Email}
			if t, err := time.Parse(time.RFC3339, ref.Time); err == nil {
				pr.OriginalTime = t
			}
			break
		}
	}
	return pr
}

// ReviewItemToFeedback converts a ReviewItem to a Feedback.
func ReviewItemToFeedback(item ReviewItem) Feedback {
	subject, body := protocol.SplitSubjectBody(item.Content)
	content := subject
	if body != "" {
		content = subject + "\n\n" + body
	}
	id := protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch)
	var oldLine, newLine, oldLineEnd, newLineEnd int
	if item.OldLine.Valid {
		oldLine = int(item.OldLine.Int64)
	}
	if item.NewLine.Valid {
		newLine = int(item.NewLine.Int64)
	}
	if item.OldLineEnd.Valid {
		oldLineEnd = int(item.OldLineEnd.Int64)
	}
	if item.NewLineEnd.Valid {
		newLineEnd = int(item.NewLineEnd.Int64)
	}
	return Feedback{
		ID:         id,
		Repository: item.RepoURL,
		Branch:     item.Branch,
		Author: Author{
			Name:  item.AuthorName,
			Email: item.AuthorEmail,
		},
		Timestamp: item.Timestamp,
		Content:   content,
		PullRequest: Ref{
			RepoURL: nullStr(item.PullRequestRepoURL),
			Hash:    nullStr(item.PullRequestHash),
			Branch:  nullStr(item.PullRequestBranch),
		},
		Commit:      nullStr(item.CommitRef),
		File:        nullStr(item.File),
		OldLine:     oldLine,
		NewLine:     newLine,
		OldLineEnd:  oldLineEnd,
		NewLineEnd:  newLineEnd,
		ReviewState: ReviewState(nullStr(item.ReviewStateField)),
		Suggestion:  item.Suggestion == 1,
		IsEdited:    item.IsEdited,
		IsRetracted: item.IsRetracted,
		Comments:    item.Comments,
	}
}

func joinSubjectBody(subject, body string) string {
	if body != "" {
		return subject + "\n\n" + body
	}
	return subject
}

func nullStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func parseCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
