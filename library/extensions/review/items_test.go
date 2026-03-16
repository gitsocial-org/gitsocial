// items_test.go - Tests for review item conversion functions
package review

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/protocol"
)

func TestReviewItemToPullRequest(t *testing.T) {
	item := ReviewItem{
		RepoURL:     "https://github.com/user/repo",
		Hash:        "abc123def456",
		Branch:      "gitmsg/review",
		Type:        string(ItemTypePullRequest),
		State:       sql.NullString{String: "open", Valid: true},
		Base:        sql.NullString{String: "#branch:main", Valid: true},
		Head:        sql.NullString{String: "#branch:feature", Valid: true},
		Closes:      sql.NullString{String: "#commit:issue1,#commit:issue2", Valid: true},
		Reviewers:   sql.NullString{String: "bob@example.com,carol@example.com", Valid: true},
		Content:     "Add new feature\n\nImplements the login page",
		AuthorName:  "Alice",
		AuthorEmail: "alice@example.com",
		Timestamp:   time.Date(2025, 10, 15, 12, 0, 0, 0, time.UTC),
		IsEdited:    true,
		Comments:    5,
	}

	pr := ReviewItemToPullRequest(item)
	if pr.Subject != "Add new feature" {
		t.Errorf("Subject = %q", pr.Subject)
	}
	if pr.Body != "Implements the login page" {
		t.Errorf("Body = %q", pr.Body)
	}
	if pr.State != PRStateOpen {
		t.Errorf("State = %q", pr.State)
	}
	if pr.Base != "#branch:main" {
		t.Errorf("Base = %q", pr.Base)
	}
	if pr.Head != "#branch:feature" {
		t.Errorf("Head = %q", pr.Head)
	}
	if len(pr.Closes) != 2 {
		t.Errorf("len(Closes) = %d, want 2", len(pr.Closes))
	}
	if len(pr.Reviewers) != 2 {
		t.Errorf("len(Reviewers) = %d, want 2", len(pr.Reviewers))
	}
	if pr.Author.Name != "Alice" {
		t.Errorf("Author.Name = %q", pr.Author.Name)
	}
	if !pr.IsEdited {
		t.Error("IsEdited should be true")
	}
	if pr.Comments != 5 {
		t.Errorf("Comments = %d, want 5", pr.Comments)
	}
}

func TestReviewItemToPullRequest_nullFields(t *testing.T) {
	item := ReviewItem{
		RepoURL:     "https://github.com/user/repo",
		Hash:        "abc123",
		Branch:      "gitmsg/review",
		Content:     "Simple PR",
		AuthorName:  "Alice",
		AuthorEmail: "alice@example.com",
	}

	pr := ReviewItemToPullRequest(item)
	if pr.Base != "" {
		t.Errorf("Base = %q, want empty", pr.Base)
	}
	if pr.Head != "" {
		t.Errorf("Head = %q, want empty", pr.Head)
	}
	if len(pr.Closes) != 0 {
		t.Errorf("len(Closes) = %d, want 0", len(pr.Closes))
	}
	if len(pr.Reviewers) != 0 {
		t.Errorf("len(Reviewers) = %d, want 0", len(pr.Reviewers))
	}
}

func TestReviewItemToFeedback(t *testing.T) {
	item := ReviewItem{
		RepoURL:            "https://github.com/user/repo",
		Hash:               "fb123def456",
		Branch:             "gitmsg/review",
		Type:               string(ItemTypeFeedback),
		PullRequestRepoURL: sql.NullString{String: "https://github.com/user/repo", Valid: true},
		PullRequestHash:    sql.NullString{String: "pr123", Valid: true},
		PullRequestBranch:  sql.NullString{String: "gitmsg/review", Valid: true},
		CommitRef:          sql.NullString{String: "commit456", Valid: true},
		File:               sql.NullString{String: "main.go", Valid: true},
		OldLine:            sql.NullInt64{Int64: 10, Valid: true},
		NewLine:            sql.NullInt64{Int64: 15, Valid: true},
		OldLineEnd:         sql.NullInt64{Int64: 12, Valid: true},
		NewLineEnd:         sql.NullInt64{Int64: 17, Valid: true},
		ReviewStateField:   sql.NullString{String: "approved", Valid: true},
		Suggestion:         1,
		Content:            "Looks good",
		AuthorName:         "Bob",
		AuthorEmail:        "bob@example.com",
		Timestamp:          time.Date(2025, 10, 16, 10, 0, 0, 0, time.UTC),
		IsEdited:           false,
		Comments:           1,
	}

	fb := ReviewItemToFeedback(item)
	if fb.Content != "Looks good" {
		t.Errorf("Content = %q", fb.Content)
	}
	if fb.PullRequest.RepoURL != "https://github.com/user/repo" {
		t.Errorf("PullRequest.RepoURL = %q", fb.PullRequest.RepoURL)
	}
	if fb.PullRequest.Hash != "pr123" {
		t.Errorf("PullRequest.Hash = %q", fb.PullRequest.Hash)
	}
	if fb.Commit != "commit456" {
		t.Errorf("Commit = %q", fb.Commit)
	}
	if fb.File != "main.go" {
		t.Errorf("File = %q", fb.File)
	}
	if fb.OldLine != 10 {
		t.Errorf("OldLine = %d", fb.OldLine)
	}
	if fb.NewLine != 15 {
		t.Errorf("NewLine = %d", fb.NewLine)
	}
	if fb.OldLineEnd != 12 {
		t.Errorf("OldLineEnd = %d", fb.OldLineEnd)
	}
	if fb.NewLineEnd != 17 {
		t.Errorf("NewLineEnd = %d", fb.NewLineEnd)
	}
	if fb.ReviewState != ReviewStateApproved {
		t.Errorf("ReviewState = %q", fb.ReviewState)
	}
	if !fb.Suggestion {
		t.Error("Suggestion should be true")
	}
	if fb.Author.Name != "Bob" {
		t.Errorf("Author.Name = %q", fb.Author.Name)
	}
}

func TestReviewItemToFeedback_nullLines(t *testing.T) {
	item := ReviewItem{
		RepoURL:          "https://github.com/user/repo",
		Hash:             "fb123",
		Branch:           "gitmsg/review",
		ReviewStateField: sql.NullString{String: "changes-requested", Valid: true},
		Content:          "Needs changes",
		AuthorName:       "Bob",
		AuthorEmail:      "bob@example.com",
	}

	fb := ReviewItemToFeedback(item)
	if fb.OldLine != 0 {
		t.Errorf("OldLine = %d, want 0", fb.OldLine)
	}
	if fb.NewLine != 0 {
		t.Errorf("NewLine = %d, want 0", fb.NewLine)
	}
	if fb.ReviewState != ReviewStateChangesRequested {
		t.Errorf("ReviewState = %q", fb.ReviewState)
	}
}

func TestParseCSV(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"single", "alice@example.com", 1},
		{"multiple", "alice@example.com,bob@example.com", 2},
		{"whitespace", " alice@example.com , bob@example.com ", 2},
		{"trailing comma", "alice@example.com,", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCSV(tt.input)
			if len(got) != tt.want {
				t.Errorf("parseCSV(%q) = %d items, want %d", tt.input, len(got), tt.want)
			}
		})
	}
}

func TestNullStr(t *testing.T) {
	if got := nullStr(sql.NullString{String: "hello", Valid: true}); got != "hello" {
		t.Errorf("nullStr(valid) = %q", got)
	}
	if got := nullStr(sql.NullString{Valid: false}); got != "" {
		t.Errorf("nullStr(invalid) = %q", got)
	}
}

func TestReviewItemToPullRequest_withOriginalAuthor(t *testing.T) {
	item := ReviewItem{
		RepoURL:     "https://github.com/user/repo",
		Hash:        "abc123def456",
		Branch:      "gitmsg/review",
		Content:     "Fork PR",
		AuthorName:  "Forker",
		AuthorEmail: "forker@example.com",
		Timestamp:   time.Date(2025, 10, 15, 12, 0, 0, 0, time.UTC),
		References: []protocol.Ref{
			{
				Ext:    "review",
				Author: "Original Author",
				Email:  "original@example.com",
				Time:   "2025-10-14T10:00:00Z",
				Fields: map[string]string{"type": "pull-request"},
			},
		},
	}
	pr := ReviewItemToPullRequest(item)
	if pr.OriginalAuthor == nil {
		t.Fatal("OriginalAuthor should not be nil")
	}
	if pr.OriginalAuthor.Name != "Original Author" {
		t.Errorf("OriginalAuthor.Name = %q", pr.OriginalAuthor.Name)
	}
	if pr.OriginalAuthor.Email != "original@example.com" {
		t.Errorf("OriginalAuthor.Email = %q", pr.OriginalAuthor.Email)
	}
	if pr.OriginalTime.IsZero() {
		t.Error("OriginalTime should be set")
	}
}

func TestReviewItemToPullRequest_refWithBadTime(t *testing.T) {
	item := ReviewItem{
		RepoURL:     "https://github.com/user/repo",
		Hash:        "abc123def456",
		Branch:      "gitmsg/review",
		Content:     "PR",
		AuthorName:  "A",
		AuthorEmail: "a@test.com",
		References: []protocol.Ref{
			{
				Ext:    "review",
				Author: "B",
				Email:  "b@test.com",
				Time:   "not-a-date",
				Fields: map[string]string{"type": "pull-request"},
			},
		},
	}
	pr := ReviewItemToPullRequest(item)
	if pr.OriginalAuthor == nil {
		t.Fatal("OriginalAuthor should still be set")
	}
	if !pr.OriginalTime.IsZero() {
		t.Error("OriginalTime should be zero for invalid time")
	}
}

func TestReviewItemToPullRequest_nonReviewRef(t *testing.T) {
	item := ReviewItem{
		RepoURL:     "https://github.com/user/repo",
		Hash:        "abc123def456",
		Branch:      "gitmsg/review",
		Content:     "PR",
		AuthorName:  "A",
		AuthorEmail: "a@test.com",
		References: []protocol.Ref{
			{Ext: "social", Fields: map[string]string{"type": "post"}},
		},
	}
	pr := ReviewItemToPullRequest(item)
	if pr.OriginalAuthor != nil {
		t.Error("OriginalAuthor should be nil for non-review refs")
	}
}

func TestReviewItemToFeedback_withBody(t *testing.T) {
	item := ReviewItem{
		RepoURL:     "https://github.com/user/repo",
		Hash:        "fb456",
		Branch:      "gitmsg/review",
		Content:     "Subject line\n\nBody text here",
		AuthorName:  "Bob",
		AuthorEmail: "bob@test.com",
	}
	fb := ReviewItemToFeedback(item)
	if fb.Content != "Subject line\n\nBody text here" {
		t.Errorf("Content = %q, want combined subject+body", fb.Content)
	}
}
