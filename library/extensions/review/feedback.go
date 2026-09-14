// feedback.go - Feedback (code review activity) creation and management
package review

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
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
	// GITREVIEW.md 1.4: a suggestion carries its replacement in a suggestion fence.
	if opts.Suggestion && !fencePattern.MatchString(content) {
		return result.Err[Feedback]("VALIDATION_ERROR", "a suggestion must include a suggestion fenced code block")
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
	return result.Ok(reviewItemToFeedback(*item))
}

// GetFeedback retrieves a single feedback item by reference, full ref or bare hash.
func GetFeedback(feedbackRef string) Result[Feedback] {
	item, err := GetReviewItemByRef(feedbackRef, "")
	if err != nil {
		return result.Err[Feedback]("NOT_FOUND", notFoundMessage("feedback", feedbackRef, err))
	}
	if item.Type != string(itemTypeFeedback) {
		return result.Err[Feedback]("NOT_FOUND", "not feedback: "+feedbackRef)
	}
	return result.Ok(reviewItemToFeedback(*item))
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

	rv := reviewItemToFeedback(*existing)
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
	return result.Ok(reviewItemToFeedback(*item))
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

	hash, err := git.CreateCommitOnBranch(workdir, branch, content)
	if err != nil {
		return result.Err[bool]("COMMIT_FAILED", err.Error())
	}
	if err := cacheReviewFromCommit(workdir, repoURL, hash, branch); err != nil {
		return result.Err[bool]("CACHE_FAILED", err.Error())
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

// latestVerdicts keeps each author's newest feedback that carries a review state (GITREVIEW.md 1.8).
func latestVerdicts(feedback []Feedback) map[string]Feedback {
	latest := map[string]Feedback{}
	for _, r := range feedback {
		if r.ReviewState == "" {
			continue
		}
		if prev, ok := latest[r.Author.Email]; !ok || r.Timestamp.After(prev.Timestamp) {
			latest[r.Author.Email] = r
		}
	}
	return latest
}

// ComputeReviewSummary builds a ReviewSummary from an already-fetched feedback slice.
func ComputeReviewSummary(feedback []Feedback, reviewers []string) ReviewSummary {
	latestByAuthor := latestVerdicts(feedback)
	summary := ReviewSummary{}
	for _, r := range latestByAuthor {
		switch r.ReviewState {
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
	byPR, err := cache.QueryLocked(func(db *sql.DB) (map[string][]Feedback, error) {
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
		grouped := map[string][]Feedback{}
		for dbRows.Next() {
			var prHash, authorEmail, reviewState string
			var ts sql.NullString
			if err := dbRows.Scan(&prHash, &authorEmail, &reviewState, &ts); err != nil {
				return nil, err
			}
			f := Feedback{Author: Author{Email: authorEmail}, ReviewState: ReviewState(reviewState)}
			if ts.Valid {
				f.Timestamp, _ = time.Parse(time.RFC3339, ts.String)
			}
			grouped[prHash] = append(grouped[prHash], f)
		}
		return grouped, dbRows.Err()
	})
	if err != nil {
		return result
	}
	for _, k := range keys {
		result[k.Hash] = ComputeReviewSummary(byPR[k.Hash], k.Reviewers)
	}
	return result
}

// buildFeedbackContent builds a feedback commit's message from its options.
func buildFeedbackContent(content string, opts CreateFeedbackOptions, editsRef string) string {
	fields := map[string]string{
		"type":         string(itemTypeFeedback),
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
					Metadata: subjectOf(item.Content),
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

// subjectOf returns a message's first line.
func subjectOf(content string) string {
	subject, _ := protocol.SplitSubjectBody(content)
	return subject
}
