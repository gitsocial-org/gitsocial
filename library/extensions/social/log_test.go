// log_test.go - Tests for activity log functions
package social

import (
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

func TestMatchesLogFilters_noFilters(t *testing.T) {
	entry := LogEntry{Type: logTypePost, Timestamp: time.Now()}
	opts := &GetLogsOptions{}
	if !matchesLogFilters(entry, opts) {
		t.Error("entry with no filters should match")
	}
}

func TestMatchesLogFilters_typeFilter(t *testing.T) {
	entry := LogEntry{Type: logTypePost, Timestamp: time.Now()}
	opts := &GetLogsOptions{Types: []LogEntryType{logTypeComment}}
	if matchesLogFilters(entry, opts) {
		t.Error("post should not match comment filter")
	}
	opts.Types = []LogEntryType{logTypePost, logTypeComment}
	if !matchesLogFilters(entry, opts) {
		t.Error("post should match when post is in types")
	}
}

func TestMatchesLogFilters_authorFilter(t *testing.T) {
	entry := LogEntry{Type: logTypePost, Author: Author{Email: "alice@test.com"}, Timestamp: time.Now()}
	opts := &GetLogsOptions{Author: "alice"}
	if !matchesLogFilters(entry, opts) {
		t.Error("should match partial author email")
	}
	opts.Author = "bob"
	if matchesLogFilters(entry, opts) {
		t.Error("should not match different author")
	}
}

func TestMatchesLogFilters_authorCaseInsensitive(t *testing.T) {
	entry := LogEntry{Type: logTypePost, Author: Author{Email: "Alice@Test.com"}, Timestamp: time.Now()}
	opts := &GetLogsOptions{Author: "alice"}
	if !matchesLogFilters(entry, opts) {
		t.Error("author filter should be case-insensitive")
	}
}

func TestMatchesLogFilters_dateFilters(t *testing.T) {
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	entry := LogEntry{Type: logTypePost, Timestamp: ts}

	after := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
	opts := &GetLogsOptions{After: &after}
	if matchesLogFilters(entry, opts) {
		t.Error("June entry should not match after:July")
	}

	before := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	opts = &GetLogsOptions{Before: &before}
	if matchesLogFilters(entry, opts) {
		t.Error("June entry should not match before:May")
	}

	afterMay := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	beforeJuly := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
	opts = &GetLogsOptions{After: &afterMay, Before: &beforeJuly}
	if !matchesLogFilters(entry, opts) {
		t.Error("June entry should match May-July range")
	}
}

func TestMatchesLogFilters_combined(t *testing.T) {
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	entry := LogEntry{Type: logTypePost, Author: Author{Email: "alice@test.com"}, Timestamp: ts}

	after := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	opts := &GetLogsOptions{Types: []LogEntryType{logTypePost}, Author: "alice", After: &after}
	if !matchesLogFilters(entry, opts) {
		t.Error("should match all combined filters")
	}

	opts.Author = "bob"
	if matchesLogFilters(entry, opts) {
		t.Error("should fail when author doesn't match")
	}
}

// TestGetLogs_entryTypes walks the write path and asserts each entry's own type.
func TestGetLogs_entryTypes(t *testing.T) {
	workdir := initWorkspace(t)
	post := CreatePost(workdir, "Hello world", nil)
	if !post.Success {
		t.Fatalf("CreatePost() failed: %s", post.Error.Message)
	}
	if r := CreateComment(workdir, post.Data.ID, "Great idea!", nil); !r.Success {
		t.Fatalf("CreateComment() failed: %s", r.Error.Message)
	}
	if r := CreateRepost(workdir, post.Data.ID, nil); !r.Success {
		t.Fatalf("CreateRepost() failed: %s", r.Error.Message)
	}
	if r := CreateQuote(workdir, post.Data.ID, "Worth reading:", nil); !r.Success {
		t.Fatalf("CreateQuote() failed: %s", r.Error.Message)
	}
	if r := CreateList(workdir, "following", "Following"); !r.Success {
		t.Fatalf("CreateList() failed: %s", r.Error.Message)
	}

	result := GetLogs(workdir, "timeline", &GetLogsOptions{Limit: 50})
	if !result.Success {
		t.Fatalf("GetLogs() failed: %s", result.Error.Message)
	}
	seen := make(map[LogEntryType]LogEntry)
	for _, entry := range result.Data {
		seen[entry.Type] = entry
	}
	for _, want := range []LogEntryType{logTypePost, logTypeComment, logTypeRepost, logTypeQuote, logTypeListCreate} {
		if _, ok := seen[want]; !ok {
			t.Errorf("no %q entry in the log; got %v", want, seen)
		}
	}
	if details := seen[logTypeComment].Details; !strings.HasPrefix(details, "Re: ") {
		t.Errorf("comment details = %q, want a Re: prefix", details)
	}
	if details := seen[logTypeListCreate].Details; details != "Created list" {
		t.Errorf("list details = %q, want %q", details, "Created list")
	}
}

func TestDetectLogEntryType_defaultPost(t *testing.T) {
	msg := &protocol.Message{Header: protocol.Header{Fields: map[string]string{}}}
	got := detectLogEntryType(git.Commit{Hash: "xyz"}, msg, nil)
	if got != logTypePost {
		t.Errorf("detectLogEntryType() = %q, want %q", got, logTypePost)
	}
}

func TestDetectLogEntryType_nilMsg(t *testing.T) {
	got := detectLogEntryType(git.Commit{Hash: "xyz"}, nil, nil)
	if got != logTypePost {
		t.Errorf("detectLogEntryType(nil msg) = %q, want %q", got, logTypePost)
	}
}

func TestFormatLogDetails_post(t *testing.T) {
	commit := git.Commit{Message: "Hello world"}
	got := formatLogDetails(commit, nil, logTypePost)
	if got != "Hello world" {
		t.Errorf("formatLogDetails(post) = %q, want %q", got, "Hello world")
	}
}

func TestFormatLogDetails_comment(t *testing.T) {
	commit := git.Commit{Message: "Nice work"}
	got := formatLogDetails(commit, nil, logTypeComment)
	if got != "Re: Nice work" {
		t.Errorf("formatLogDetails(comment) = %q, want %q", got, "Re: Nice work")
	}
}

func TestFormatLogDetails_repost(t *testing.T) {
	commit := git.Commit{Message: "Content"}
	got := formatLogDetails(commit, nil, logTypeRepost)
	if got != "Repost: Content" {
		t.Errorf("formatLogDetails(repost) = %q", got)
	}
}

func TestFormatLogDetails_quote(t *testing.T) {
	commit := git.Commit{Message: "Content"}
	got := formatLogDetails(commit, nil, logTypeQuote)
	if got != "Quote: Content" {
		t.Errorf("formatLogDetails(quote) = %q", got)
	}
}

func TestFormatLogDetails_listCreate(t *testing.T) {
	got := formatLogDetails(git.Commit{}, nil, logTypeListCreate)
	if got != "Created list" {
		t.Errorf("formatLogDetails(list-create) = %q", got)
	}
}

func TestFormatLogDetails_listDelete(t *testing.T) {
	got := formatLogDetails(git.Commit{}, nil, logTypeListDelete)
	if got != "Deleted list" {
		t.Errorf("formatLogDetails(list-delete) = %q", got)
	}
}

func TestFormatLogDetails_config(t *testing.T) {
	got := formatLogDetails(git.Commit{}, nil, logTypeConfig)
	if got != "Updated config" {
		t.Errorf("formatLogDetails(config) = %q", got)
	}
}

func TestFormatLogDetails_metadata(t *testing.T) {
	got := formatLogDetails(git.Commit{}, nil, logTypeMetadata)
	if got != "Metadata update" {
		t.Errorf("formatLogDetails(metadata) = %q", got)
	}
}

func TestExtractRepoFromRefname_empty(t *testing.T) {
	if got := extractRepoFromRefname(""); got != "" {
		t.Errorf("extractRepoFromRefname(\"\") = %q, want empty", got)
	}
}

func TestExtractRepoFromRefname_nonRemote(t *testing.T) {
	if got := extractRepoFromRefname("refs/heads/main"); got != "" {
		t.Errorf("extractRepoFromRefname(heads) = %q, want empty", got)
	}
}

func TestExtractRepoFromRefname_remote(t *testing.T) {
	got := extractRepoFromRefname("refs/remotes/origin/main")
	if got != "origin" {
		t.Errorf("extractRepoFromRefname(remotes) = %q, want %q", got, "origin")
	}
}

func TestCommitToLogEntry(t *testing.T) {
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	commit := git.Commit{
		Hash:      "abc123def456",
		Timestamp: ts,
		Author:    "Alice",
		Email:     "alice@test.com",
		Message:   "Hello world",
		Refname:   "refs/remotes/origin/main",
	}
	entry := commitToLogEntry(commit, nil)

	if entry.Hash != "abc123def456" {
		t.Errorf("Hash = %q", entry.Hash)
	}
	if entry.Timestamp != ts {
		t.Errorf("Timestamp = %v", entry.Timestamp)
	}
	if entry.Author.Name != "Alice" {
		t.Errorf("Author.Name = %q", entry.Author.Name)
	}
	if entry.Author.Email != "alice@test.com" {
		t.Errorf("Author.Email = %q", entry.Author.Email)
	}
	if entry.Type != logTypePost {
		t.Errorf("Type = %q, want post", entry.Type)
	}
	if entry.Repository != "origin" {
		t.Errorf("Repository = %q, want origin", entry.Repository)
	}
	if entry.PostID != "abc123def456" {
		t.Errorf("PostID = %q", entry.PostID)
	}
}
