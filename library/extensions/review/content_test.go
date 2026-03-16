// content_test.go - Tests for pure content builder functions
package review

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

func TestBuildPRContent_minimal(t *testing.T) {
	content := buildPRContent("Add feature", "", CreatePROptions{}, "")
	if !strings.Contains(content, "Add feature") {
		t.Error("content should contain subject")
	}
	msg := protocol.ParseMessage(content)
	if msg == nil {
		t.Fatal("ParseMessage returned nil")
	}
	if msg.Header.Ext != "review" {
		t.Errorf("ext = %q, want review", msg.Header.Ext)
	}
	if msg.Header.Fields["type"] != "pull-request" {
		t.Errorf("type = %q, want pull-request", msg.Header.Fields["type"])
	}
	if msg.Header.Fields["state"] != "open" {
		t.Errorf("state = %q, want open", msg.Header.Fields["state"])
	}
}

func TestBuildPRContent_allFields(t *testing.T) {
	opts := CreatePROptions{
		Base:      "#branch:main",
		Head:      "#branch:feature",
		Closes:    []string{"#commit:iss1", "#commit:iss2"},
		Reviewers: []string{"bob@test.com", "carol@test.com"},
		MergeBase: "abc123",
		MergeHead: "def456",
	}
	content := buildPRContent("Add feature", "Detailed description", opts, "")
	if !strings.Contains(content, "Detailed description") {
		t.Error("content should contain body")
	}
	msg := protocol.ParseMessage(content)
	if msg == nil {
		t.Fatal("ParseMessage returned nil")
	}
	if msg.Header.Fields["base"] != "#branch:main" {
		t.Errorf("base = %q", msg.Header.Fields["base"])
	}
	if msg.Header.Fields["head"] != "#branch:feature" {
		t.Errorf("head = %q", msg.Header.Fields["head"])
	}
	if msg.Header.Fields["closes"] != "#commit:iss1,#commit:iss2" {
		t.Errorf("closes = %q", msg.Header.Fields["closes"])
	}
	if msg.Header.Fields["reviewers"] != "bob@test.com,carol@test.com" {
		t.Errorf("reviewers = %q", msg.Header.Fields["reviewers"])
	}
	if msg.Header.Fields["merge-base"] != "abc123" {
		t.Errorf("merge-base = %q", msg.Header.Fields["merge-base"])
	}
	if msg.Header.Fields["merge-head"] != "def456" {
		t.Errorf("merge-head = %q", msg.Header.Fields["merge-head"])
	}
}

func TestBuildPRContentWithState_withEdits(t *testing.T) {
	content := buildPRContentWithState("Updated PR", "", CreatePROptions{}, "#commit:abc123@gitmsg/review", PRStateMerged, nil)
	msg := protocol.ParseMessage(content)
	if msg == nil {
		t.Fatal("ParseMessage returned nil")
	}
	if msg.Header.Fields["edits"] != "#commit:abc123@gitmsg/review" {
		t.Errorf("edits = %q", msg.Header.Fields["edits"])
	}
	if msg.Header.Fields["state"] != "merged" {
		t.Errorf("state = %q, want merged", msg.Header.Fields["state"])
	}
}

func TestBuildPRContentWithState_withRefs(t *testing.T) {
	refs := []protocol.Ref{{
		Ext:    "review",
		Author: "Alice",
		Email:  "alice@test.com",
		Time:   "2025-01-01T00:00:00Z",
		Ref:    "#commit:orig123@gitmsg/review",
		V:      "0.1.0",
		Fields: map[string]string{"type": "pull-request"},
	}}
	content := buildPRContentWithState("Fork PR", "", CreatePROptions{Base: "#branch:main"}, "", PRStateOpen, refs)
	if !strings.Contains(content, "GitMsg-Ref") {
		t.Error("should contain GitMsg-Ref section")
	}
}

func TestBuildRetractContent_review(t *testing.T) {
	content := buildRetractContent("#commit:abc123@gitmsg/review")
	msg := protocol.ParseMessage(content)
	if msg == nil {
		t.Fatal("ParseMessage returned nil")
	}
	if msg.Header.Fields["edits"] != "#commit:abc123@gitmsg/review" {
		t.Errorf("edits = %q", msg.Header.Fields["edits"])
	}
	if msg.Header.Fields["retracted"] != "true" {
		t.Error("should contain retracted=true")
	}
	if msg.Header.Ext != "review" {
		t.Errorf("ext = %q, want review", msg.Header.Ext)
	}
}

func TestBuildFeedbackContent_minimal(t *testing.T) {
	opts := CreateFeedbackOptions{
		PullRequest: "#commit:pr123@gitmsg/review",
		ReviewState: ReviewStateApproved,
	}
	content := buildFeedbackContent("Looks good", opts, "")
	msg := protocol.ParseMessage(content)
	if msg == nil {
		t.Fatal("ParseMessage returned nil")
	}
	if msg.Header.Fields["type"] != "feedback" {
		t.Errorf("type = %q, want feedback", msg.Header.Fields["type"])
	}
	if msg.Header.Fields["review-state"] != "approved" {
		t.Errorf("review-state = %q", msg.Header.Fields["review-state"])
	}
	if msg.Header.Fields["pull-request"] != "#commit:pr123@gitmsg/review" {
		t.Errorf("pull-request = %q", msg.Header.Fields["pull-request"])
	}
}

func TestBuildFeedbackContent_allFields(t *testing.T) {
	opts := CreateFeedbackOptions{
		PullRequest: "#commit:pr123@gitmsg/review",
		Commit:      "abc123",
		File:        "main.go",
		OldLine:     10,
		NewLine:     15,
		OldLineEnd:  12,
		NewLineEnd:  17,
		ReviewState: ReviewStateChangesRequested,
		Suggestion:  true,
	}
	content := buildFeedbackContent("Fix this", opts, "")
	msg := protocol.ParseMessage(content)
	if msg == nil {
		t.Fatal("ParseMessage returned nil")
	}
	if msg.Header.Fields["commit"] != "abc123" {
		t.Errorf("commit = %q", msg.Header.Fields["commit"])
	}
	if msg.Header.Fields["file"] != "main.go" {
		t.Errorf("file = %q", msg.Header.Fields["file"])
	}
	if msg.Header.Fields["old-line"] != "10" {
		t.Errorf("old-line = %q", msg.Header.Fields["old-line"])
	}
	if msg.Header.Fields["new-line"] != "15" {
		t.Errorf("new-line = %q", msg.Header.Fields["new-line"])
	}
	if msg.Header.Fields["old-line-end"] != "12" {
		t.Errorf("old-line-end = %q", msg.Header.Fields["old-line-end"])
	}
	if msg.Header.Fields["new-line-end"] != "17" {
		t.Errorf("new-line-end = %q", msg.Header.Fields["new-line-end"])
	}
	if msg.Header.Fields["suggestion"] != "true" {
		t.Errorf("suggestion = %q", msg.Header.Fields["suggestion"])
	}
}

func TestBuildFeedbackContent_withEdits(t *testing.T) {
	opts := CreateFeedbackOptions{
		PullRequest: "#commit:pr123@gitmsg/review",
		ReviewState: ReviewStateApproved,
	}
	content := buildFeedbackContent("Updated feedback", opts, "#commit:fb123@gitmsg/review")
	msg := protocol.ParseMessage(content)
	if msg == nil {
		t.Fatal("ParseMessage returned nil")
	}
	if msg.Header.Fields["edits"] != "#commit:fb123@gitmsg/review" {
		t.Errorf("edits = %q", msg.Header.Fields["edits"])
	}
}

func TestBuildFeedbackContent_withPRRef(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	hash := "aef012345678"
	insertReviewTestCommit(t, repoURL, hash)
	InsertReviewItem(ReviewItem{
		RepoURL: repoURL,
		Hash:    hash,
		Branch:  reviewTestBranch,
		Type:    "pull-request",
		State:   cache.ToNullString("open"),
	})

	opts := CreateFeedbackOptions{
		PullRequest: repoURL + "#commit:" + hash + "@" + reviewTestBranch,
		ReviewState: ReviewStateApproved,
	}
	content := buildFeedbackContent("LGTM", opts, "")
	if !strings.Contains(content, "GitMsg-Ref") {
		t.Error("should contain GitMsg-Ref section when PR exists")
	}
}

func TestExtractSubjectLine(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Simple subject", "Simple subject"},
		{"Subject\n\nBody text", "Subject"},
		{"  Whitespace  \n  Body  ", "Whitespace"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := extractSubjectLine(tt.input); got != tt.want {
			t.Errorf("extractSubjectLine(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestJoinSubjectBody(t *testing.T) {
	tests := []struct {
		subject string
		body    string
		want    string
	}{
		{"Subject", "Body", "Subject\n\nBody"},
		{"Subject", "", "Subject"},
	}
	for _, tt := range tests {
		if got := joinSubjectBody(tt.subject, tt.body); got != tt.want {
			t.Errorf("joinSubjectBody(%q, %q) = %q, want %q", tt.subject, tt.body, got, tt.want)
		}
	}
}
