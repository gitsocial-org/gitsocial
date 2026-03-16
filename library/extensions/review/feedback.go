// feedback.go - Feedback (code review activity) creation and management
package review

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/result"
)

type CreateFeedbackOptions struct {
	PullRequest string
	Commit      string
	File        string
	OldLine     int
	NewLine     int
	OldLineEnd  int
	NewLineEnd  int
	ReviewState ReviewState
	Suggestion  bool
}

// CreateFeedback creates a new feedback item on the review branch.
func CreateFeedback(workdir, content string, opts CreateFeedbackOptions) Result[Feedback] {
	branch := gitmsg.GetExtBranch(workdir, "review")
	// Validate: must have code-location fields OR review-state
	hasLocation := opts.File != "" || opts.Commit != "" || opts.OldLine > 0 || opts.NewLine > 0
	hasState := opts.ReviewState != ""
	if !hasLocation && !hasState {
		return result.Err[Feedback]("VALIDATION_ERROR", "feedback must include code location fields or review-state")
	}
	// Inline feedback must include file, commit, and at least one of old-line/new-line
	if hasLocation && (opts.File == "" || opts.Commit == "" || (opts.OldLine == 0 && opts.NewLine == 0)) {
		return result.Err[Feedback]("VALIDATION_ERROR", "inline feedback must include file, commit, and at least one of old-line or new-line")
	}

	repoURL := gitmsg.ResolveRepoURL(workdir)
	opts.PullRequest = protocol.LocalizeRef(opts.PullRequest, repoURL)

	commitContent := buildFeedbackContent(content, opts, "")
	hash, err := git.CreateCommitOnBranch(workdir, branch, commitContent)
	if err != nil {
		return result.Err[Feedback]("COMMIT_FAILED", err.Error())
	}
	if err := cacheReviewFromCommit(workdir, repoURL, hash, branch); err != nil {
		return result.Err[Feedback]("CACHE_FAILED", err.Error())
	}

	item, err := GetReviewItem(repoURL, hash, branch)
	if err != nil {
		return result.Err[Feedback]("GET_FAILED", err.Error())
	}
	return result.Ok(ReviewItemToFeedback(*item))
}

// GetFeedback retrieves a single feedback item by reference.
func GetFeedback(feedbackRef string) Result[Feedback] {
	// Try direct indexed lookup first (covers full refs and full hashes)
	item, err := GetReviewItemByRef(feedbackRef, "")
	if err == nil && item.Type == string(ItemTypeFeedback) {
		return result.Ok(ReviewItemToFeedback(*item))
	}
	// Fall back to prefix scan for short hashes
	items, err := GetReviewItems(ReviewQuery{
		Types: []string{string(ItemTypeFeedback)},
		Limit: 1000,
	})
	if err != nil {
		return result.Err[Feedback]("QUERY_FAILED", err.Error())
	}
	for _, item := range items {
		r := ReviewItemToFeedback(item)
		if r.ID == feedbackRef || strings.HasPrefix(r.ID, feedbackRef) {
			return result.Ok(r)
		}
	}
	return result.Err[Feedback]("NOT_FOUND", "feedback not found: "+feedbackRef)
}

type UpdateFeedbackOptions struct {
	Content     *string
	ReviewState *ReviewState
}

// UpdateFeedback edits an existing feedback item using core versioning.
func UpdateFeedback(workdir, feedbackRef string, opts UpdateFeedbackOptions) Result[Feedback] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetReviewItemByRef(feedbackRef, repoURL)
	if err != nil {
		return result.Err[Feedback]("NOT_FOUND", "feedback not found")
	}

	branch := gitmsg.GetExtBranch(workdir, "review")

	rv := ReviewItemToFeedback(*existing)
	createOpts := CreateFeedbackOptions{
		PullRequest: protocol.LocalizeRef(protocol.CreateRef(protocol.RefTypeCommit, rv.PullRequest.Hash, rv.PullRequest.RepoURL, rv.PullRequest.Branch), repoURL),
		Commit:      rv.Commit,
		File:        rv.File,
		OldLine:     rv.OldLine,
		NewLine:     rv.NewLine,
		OldLineEnd:  rv.OldLineEnd,
		NewLineEnd:  rv.NewLineEnd,
		ReviewState: rv.ReviewState,
		Suggestion:  rv.Suggestion,
	}
	content := rv.Content
	if opts.Content != nil {
		content = *opts.Content
	}
	if opts.ReviewState != nil {
		createOpts.ReviewState = *opts.ReviewState
	}

	canonicalRef := protocol.LocalizeRef(
		protocol.CreateRef(protocol.RefTypeCommit, existing.Hash, existing.RepoURL, existing.Branch),
		repoURL,
	)
	commitContent := buildFeedbackContent(content, createOpts, canonicalRef)

	hash, err := git.CreateCommitOnBranch(workdir, branch, commitContent)
	if err != nil {
		return result.Err[Feedback]("COMMIT_FAILED", err.Error())
	}

	if err := cacheReviewFromCommit(workdir, repoURL, hash, branch); err != nil {
		return result.Err[Feedback]("CACHE_FAILED", err.Error())
	}

	item, err := GetReviewItem(existing.RepoURL, existing.Hash, existing.Branch)
	if err != nil {
		return result.Err[Feedback]("GET_FAILED", err.Error())
	}
	return result.Ok(ReviewItemToFeedback(*item))
}

// RetractFeedback marks a feedback item as retracted.
func RetractFeedback(workdir, feedbackRef string) Result[bool] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetReviewItemByRef(feedbackRef, repoURL)
	if err != nil {
		return result.Err[bool]("NOT_FOUND", "feedback not found")
	}

	branch := gitmsg.GetExtBranch(workdir, "review")

	canonicalRef := protocol.LocalizeRef(
		protocol.CreateRef(protocol.RefTypeCommit, existing.Hash, existing.RepoURL, existing.Branch),
		repoURL,
	)
	content := buildRetractContent(canonicalRef)

	_, err = git.CreateCommitOnBranch(workdir, branch, content)
	if err != nil {
		return result.Err[bool]("COMMIT_FAILED", err.Error())
	}
	return result.Ok(true)
}

// GetReviewSummary computes the aggregate review state for a pull request.
func GetReviewSummary(prRepoURL, prHash, prBranch string, reviewers []string) ReviewSummary {
	res := GetFeedbackForPR(prRepoURL, prHash, prBranch)
	if !res.Success {
		return ReviewSummary{}
	}
	return ComputeReviewSummary(res.Data, reviewers)
}

// ComputeReviewSummary builds a ReviewSummary from an already-fetched feedback slice.
func ComputeReviewSummary(feedback []Feedback, reviewers []string) ReviewSummary {
	latestByAuthor := map[string]ReviewState{}
	latestTimeByAuthor := map[string]int64{}
	for _, r := range feedback {
		if r.ReviewState == "" {
			continue
		}
		ts := r.Timestamp.Unix()
		if prev, ok := latestTimeByAuthor[r.Author.Email]; !ok || ts > prev {
			latestByAuthor[r.Author.Email] = r.ReviewState
			latestTimeByAuthor[r.Author.Email] = ts
		}
	}
	summary := ReviewSummary{}
	for _, state := range latestByAuthor {
		switch state {
		case ReviewStateApproved:
			summary.Approved++
		case ReviewStateChangesRequested:
			summary.ChangesRequested++
		}
	}
	for _, email := range reviewers {
		if _, reviewed := latestByAuthor[email]; !reviewed {
			summary.Pending++
		}
	}
	summary.IsBlocked = summary.ChangesRequested > 0
	summary.IsApproved = summary.Approved > 0 && summary.ChangesRequested == 0 && summary.Pending == 0
	return summary
}

// PRKey identifies a pull request for batch operations.
type PRKey struct {
	RepoURL   string
	Hash      string
	Branch    string
	Reviewers []string
}

// GetBatchReviewSummaries computes review summaries for multiple PRs in a single query.
func GetBatchReviewSummaries(keys []PRKey) map[string]ReviewSummary {
	result := make(map[string]ReviewSummary, len(keys))
	if len(keys) == 0 {
		return result
	}
	type feedbackRow struct {
		prHash      string
		authorEmail string
		reviewState string
		timestamp   int64
	}
	rows, err := cache.QueryLocked(func(db *sql.DB) ([]feedbackRow, error) {
		ph := strings.Repeat("?,", len(keys))
		ph = ph[:len(ph)-1]
		var args []interface{}
		for _, k := range keys {
			args = append(args, k.Hash)
		}
		query := `SELECT v.pull_request_hash, v.author_email, v.review_state, v.timestamp
			FROM review_items_resolved v
			WHERE v.type = 'feedback'
			  AND v.pull_request_hash IN (` + ph + `)
			  AND v.review_state IS NOT NULL AND v.review_state != ''
			  AND NOT v.is_edit_commit AND NOT v.is_retracted`
		dbRows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		defer dbRows.Close()
		var results []feedbackRow
		for dbRows.Next() {
			var r feedbackRow
			var ts sql.NullString
			if err := dbRows.Scan(&r.prHash, &r.authorEmail, &r.reviewState, &ts); err != nil {
				return nil, err
			}
			if ts.Valid {
				if t, err := time.Parse(time.RFC3339, ts.String); err == nil {
					r.timestamp = t.Unix()
				}
			}
			results = append(results, r)
		}
		return results, dbRows.Err()
	})
	if err != nil {
		return result
	}
	// Build reviewer map for quick lookup
	reviewersByHash := make(map[string][]string, len(keys))
	for _, k := range keys {
		reviewersByHash[k.Hash] = k.Reviewers
	}
	// Group feedback by PR hash, then by author email keeping latest state
	type authorState struct {
		state ReviewState
		ts    int64
	}
	byPR := map[string]map[string]authorState{}
	for _, r := range rows {
		authors, ok := byPR[r.prHash]
		if !ok {
			authors = map[string]authorState{}
			byPR[r.prHash] = authors
		}
		if prev, ok := authors[r.authorEmail]; !ok || r.timestamp > prev.ts {
			authors[r.authorEmail] = authorState{state: ReviewState(r.reviewState), ts: r.timestamp}
		}
	}
	// Compute summaries
	for _, k := range keys {
		summary := ReviewSummary{}
		authors := byPR[k.Hash]
		for _, as := range authors {
			switch as.state {
			case ReviewStateApproved:
				summary.Approved++
			case ReviewStateChangesRequested:
				summary.ChangesRequested++
			}
		}
		for _, email := range k.Reviewers {
			if _, reviewed := authors[email]; !reviewed {
				summary.Pending++
			}
		}
		summary.IsBlocked = summary.ChangesRequested > 0
		summary.IsApproved = summary.Approved > 0 && summary.ChangesRequested == 0 && summary.Pending == 0
		result[k.Hash] = summary
	}
	return result
}

func buildFeedbackContent(content string, opts CreateFeedbackOptions, editsRef string) string {
	fields := map[string]string{
		"type":         string(ItemTypeFeedback),
		"pull-request": opts.PullRequest,
	}
	if editsRef != "" {
		fields["edits"] = editsRef
	}
	if opts.Commit != "" {
		fields["commit"] = opts.Commit
	}
	if opts.File != "" {
		fields["file"] = opts.File
	}
	if opts.NewLine > 0 {
		fields["new-line"] = fmt.Sprintf("%d", opts.NewLine)
	}
	if opts.NewLineEnd > 0 {
		fields["new-line-end"] = fmt.Sprintf("%d", opts.NewLineEnd)
	}
	if opts.OldLine > 0 {
		fields["old-line"] = fmt.Sprintf("%d", opts.OldLine)
	}
	if opts.OldLineEnd > 0 {
		fields["old-line-end"] = fmt.Sprintf("%d", opts.OldLineEnd)
	}
	if opts.ReviewState != "" {
		fields["review-state"] = string(opts.ReviewState)
	}
	if opts.Suggestion {
		fields["suggestion"] = "true"
	}

	// Build GitMsg-Ref for the pull request
	var refs []protocol.Ref
	if opts.PullRequest != "" {
		parsed := protocol.ParseRef(opts.PullRequest)
		if parsed.Value != "" {
			prRepoURL := parsed.Repository
			// Try to look up PR metadata for the ref section
			if item, err := GetReviewItemByRef(opts.PullRequest, prRepoURL); err == nil {
				refs = append(refs, protocol.Ref{
					Ext:      "review",
					Ref:      opts.PullRequest,
					V:        "0.1.0",
					Author:   item.AuthorName,
					Email:    item.AuthorEmail,
					Time:     item.Timestamp.Format("2006-01-02T15:04:05Z"),
					Fields:   map[string]string{"type": string(ItemTypePullRequest)},
					Metadata: extractSubjectLine(item.Content),
				})
			}
		}
	}

	header := protocol.Header{
		Ext:        "review",
		V:          "0.1.0",
		Fields:     fields,
		FieldOrder: feedbackFieldOrder,
	}
	return protocol.FormatMessage(content, header, refs)
}

func extractSubjectLine(content string) string {
	content = strings.TrimSpace(content)
	if idx := strings.Index(content, "\n"); idx > 0 {
		return strings.TrimSpace(content[:idx])
	}
	return content
}
