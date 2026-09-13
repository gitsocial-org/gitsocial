// items.go - Review item queries and cache operations
package review

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
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
	DependsOn          sql.NullString
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
	Origin           *protocol.Origin
	Content          string
	AuthorName       string
	AuthorEmail      string
	Timestamp        time.Time
	EditOf           sql.NullString
	IsRetracted      bool
	IsEdited         bool
	HasProposedEdits bool
	IsVirtual        bool
	// Derived from social_interactions
	Comments int
	// Parsed from commit message GitMsg-Ref sections
	References []protocol.Ref
	Adopts     string // the fork pull request ref this copy homes, read from the canonical's adopts header
}

const reviewExtColumns = `v.type, v.state, v.draft, v.base, v.base_tip, v.head, v.head_tip, v.depends_on, v.closes, v.reviewers,
       v.pull_request_repo_url, v.pull_request_hash, v.pull_request_branch,
       v.commit_ref, v.file, v.old_line, v.new_line, v.old_line_end, v.new_line_end,
       v.review_state, v.suggestion,
       v.labels`

var baseSelectFromView = cache.ResolvedSelect("review_items_resolved", reviewExtColumns)

// InsertReviewItem inserts or updates one review item in the cache database.
func InsertReviewItem(item ReviewItem) error {
	return InsertReviewItems([]ReviewItem{item})
}

// InsertReviewItems inserts or updates review items and rebuilds review_reviewers in one transaction, so search reads no half-linked state.
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
			(repo_url, hash, branch, type, state, draft, base, base_tip, head, head_tip, depends_on, closes, reviewers,
			 pull_request_repo_url, pull_request_hash, pull_request_branch,
			 commit_ref, file, old_line, new_line, old_line_end, new_line_end, review_state, suggestion)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(repo_url, hash, branch) DO UPDATE SET
				type = excluded.type,
				state = excluded.state,
				draft = excluded.draft,
				base = excluded.base,
				base_tip = excluded.base_tip,
				head = excluded.head,
				head_tip = excluded.head_tip,
				depends_on = excluded.depends_on,
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
			if _, err := stmt.Exec(
				item.RepoURL, item.Hash, item.Branch,
				item.Type, item.State, item.Draft, item.Base, item.BaseTip, item.Head, item.HeadTip, item.DependsOn, item.Closes, item.Reviewers,
				item.PullRequestRepoURL, item.PullRequestHash, item.PullRequestBranch,
				item.CommitRef, item.File, item.OldLine, item.NewLine, item.OldLineEnd, item.NewLineEnd,
				item.ReviewStateField, item.Suggestion,
			); err != nil {
				return err
			}
			if err := cache.RebuildCSVLinkingTable(tx, "review_reviewers", "email",
				item.RepoURL, item.Hash, item.Branch, nullStr(item.Reviewers)); err != nil {
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

// GetReviewItemByRef looks up a review item by its ref string; a ref without a branch resolves by hash, not by a review-branch default.
func GetReviewItemByRef(refStr string, defaultRepoURL string) (*ReviewItem, error) {
	parsed := protocol.ParseRef(refStr)
	if parsed.Value == "" {
		return nil, sql.ErrNoRows
	}
	if parsed.Branch == "" {
		return findByHash(parsed.Repository, parsed.Value)
	}
	repoURL := parsed.Repository
	if repoURL == "" {
		repoURL = defaultRepoURL
	}
	return GetReviewItem(repoURL, parsed.Value, parsed.Branch)
}

// findByHash resolves a full or short commit hash to one review item, refusing an ambiguous prefix.
func findByHash(repoURL, prefix string) (*ReviewItem, error) {
	hashes, err := cache.QueryLocked(func(db *sql.DB) ([]string, error) {
		query := `SELECT DISTINCT hash FROM review_items_resolved
			WHERE hash LIKE ? ESCAPE '\' AND NOT is_edit_commit AND NOT is_retracted`
		args := []interface{}{escapeLike(prefix) + "%"}
		if repoURL != "" {
			query += " AND repo_url = ?"
			args = append(args, repoURL)
		}
		rows, err := db.Query(query+" LIMIT 2", args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var hash string
			if err := rows.Scan(&hash); err != nil {
				return nil, err
			}
			out = append(out, hash)
		}
		return out, rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(hashes) == 0 {
		return nil, sql.ErrNoRows
	}
	if len(hashes) > 1 {
		return nil, fmt.Errorf("hash %q is ambiguous: %s and %s both match", prefix, hashes[0], hashes[1])
	}
	return cache.QueryLocked(func(db *sql.DB) (*ReviewItem, error) {
		query := baseSelectFromView + `
			WHERE v.hash = ? AND NOT v.is_edit_commit AND NOT v.is_retracted`
		args := []interface{}{hashes[0]}
		if repoURL != "" {
			query += " AND v.repo_url = ?"
			args = append(args, repoURL)
		}
		return scanResolvedRow(db.QueryRow(query+" ORDER BY v.timestamp DESC LIMIT 1", args...))
	})
}

type ReviewQuery struct {
	Types     []string
	States    []string
	RepoURL   string
	Branch    string
	PRRepoURL string
	PRHash    string
	PRBranch  string
	Limit     int
	Cursor    string // RFC3339 timestamp; items older than this, for keyset paging
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

		sqlQuery += " ORDER BY v.timestamp DESC"

		if q.Limit > 0 {
			sqlQuery += " LIMIT ?"
			args = append(args, q.Limit)
		}

		rows, err := db.Query(sqlQuery, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var items []ReviewItem
		for rows.Next() {
			item, err := scanResolvedRow(rows)
			if err != nil {
				return nil, err
			}
			items = append(items, *item)
		}
		return items, rows.Err()
	})
}

// CountPullRequests returns the number of pull requests in one repository matching the states, the set GetPullRequests lists.
func CountPullRequests(repoURL string, states []string) (int, error) {
	return cache.QueryLocked(func(db *sql.DB) (int, error) {
		query := `SELECT COUNT(*) FROM review_items_resolved v
			WHERE v.type = 'pull-request' AND NOT v.is_edit_commit AND NOT v.is_retracted`
		var args []interface{}
		if repoURL != "" {
			query += " AND v.repo_url = ?"
			args = append(args, repoURL)
		}
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
		count, _ := CountPullRequests(workspaceURL, states)
		return count
	}
	// With forks, we need to query+filter like GetPullRequestsWithForks does
	res := GetPullRequestsWithForks(workspaceURL, workspaceBranch, forkURLs, states, "", 0)
	if !res.Success {
		return 0
	}
	return len(res.Data)
}

// GetPullRequestsWithForks lists the workspace's pull requests and the registered forks' ones that target the workspace.
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
			item, err := scanResolvedRow(rows)
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
	// A homed copy carries the terminal state, so its fork original collapses into it.
	adopted := make(map[string]bool)
	for _, item := range items {
		if item.RepoURL != workspaceURL || item.Adopts == "" {
			continue
		}
		if parsed := protocol.ParseRef(item.Adopts); parsed.Type == protocol.RefTypeCommit && parsed.Value != "" {
			adopted[parsed.Repository+"#"+parsed.Value] = true
		}
	}
	// Deduplicate by hash: a mirror clone carries the same commit under two URLs.
	seen := make(map[string]bool, len(items))
	prs := make([]PullRequest, 0, len(items))
	for _, item := range items {
		if seen[item.Hash] {
			continue
		}
		if item.RepoURL != workspaceURL && !forkPRTargetsWorkspace(item, workspaceURL) {
			continue
		}
		if item.RepoURL != workspaceURL && adopted[item.RepoURL+"#"+item.Hash] {
			continue
		}
		seen[item.Hash] = true
		prs = append(prs, ReviewItemToPullRequest(item))
	}
	return result.Ok(prs)
}

// forkPRTargetsWorkspace reports whether a fork pull request's base names the workspace.
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

// scanResolvedRow scans a baseSelectFromView row (single- or multi-row query).
func scanResolvedRow(s cache.RowScanner) (*ReviewItem, error) {
	return scanReviewRow(s, nil)
}

// scanReviewRow scans a resolved review row; readDest takes the read marker when the query has one.
func scanReviewRow(s cache.RowScanner, readDest *sql.NullString) (*ReviewItem, error) {
	var item ReviewItem
	dest := []any{
		&item.Type, &item.State, &item.Draft, &item.Base, &item.BaseTip, &item.Head, &item.HeadTip, &item.DependsOn, &item.Closes, &item.Reviewers,
		&item.PullRequestRepoURL, &item.PullRequestHash, &item.PullRequestBranch,
		&item.CommitRef, &item.File, &item.OldLine, &item.NewLine, &item.OldLineEnd, &item.NewLineEnd,
		&item.ReviewStateField, &item.Suggestion,
		&item.Labels,
	}
	if readDest != nil {
		dest = append(dest, readDest)
	}
	meta, err := cache.ScanResolved(s, dest...)
	if err != nil {
		return nil, err
	}
	item.RepoURL, item.Hash, item.Branch = meta.RepoURL, meta.Hash, meta.Branch
	item.AuthorName, item.AuthorEmail = meta.AuthorName, meta.AuthorEmail
	item.Origin, item.Timestamp = meta.Origin, meta.Timestamp
	item.EditOf, item.Comments = meta.EditOf, meta.Comments
	item.IsVirtual, item.IsRetracted = meta.IsVirtual, meta.IsRetracted
	item.IsEdited, item.HasProposedEdits = meta.IsEdited, meta.HasProposed
	if meta.Message.Valid {
		if msg := protocol.ParseMessage(meta.Message.String); msg != nil {
			item.Content = msg.Content
			item.References = msg.References
		} else {
			item.Content = meta.Content
		}
	}
	if meta.OriginalMessage.Valid {
		if msg := protocol.ParseMessage(meta.OriginalMessage.String); msg != nil {
			item.Adopts = msg.Header.Fields["adopts"]
		}
	}
	return &item, nil
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
		Timestamp:        item.Timestamp,
		Subject:          subject,
		Body:             body,
		State:            PRState(nullStr(item.State)),
		IsDraft:          item.Draft == 1,
		Base:             nullStr(item.Base),
		BaseTip:          nullStr(item.BaseTip),
		Head:             nullStr(item.Head),
		HeadTip:          nullStr(item.HeadTip),
		DependsOn:        parseCSV(nullStr(item.DependsOn)),
		Closes:           parseCSV(nullStr(item.Closes)),
		Reviewers:        parseCSV(nullStr(item.Reviewers)),
		Labels:           parseCSV(nullStr(item.Labels)),
		IsEdited:         item.IsEdited,
		HasProposedEdits: item.HasProposedEdits,
		IsRetracted:      item.IsRetracted,
		Comments:         item.Comments,
	}
	pr.Origin = item.Origin
	if item.Adopts != "" {
		for _, ref := range item.References {
			if ref.Ext == "review" && ref.Ref == item.Adopts {
				pr.OriginalAuthor = &Author{Name: ref.Author, Email: ref.Email}
				if t, err := time.Parse(time.RFC3339, ref.Time); err == nil {
					pr.OriginalTime = t
				}
				break
			}
		}
	}
	return pr
}

// ReviewItemToFeedback converts a ReviewItem to a Feedback.
func ReviewItemToFeedback(item ReviewItem) Feedback {
	content := joinSubjectBody(protocol.SplitSubjectBody(item.Content))
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

// joinSubjectBody joins a subject and an optional body with a blank line.
func joinSubjectBody(subject, body string) string {
	if body != "" {
		return subject + "\n\n" + body
	}
	return subject
}

// nullStr returns a nullable string's value, or empty when it is NULL.
func nullStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// parseCSV splits a comma-separated column into its values, nil when empty.
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
