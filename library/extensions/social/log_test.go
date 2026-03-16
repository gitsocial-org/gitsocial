// log_test.go - Tests for activity log functions
package social

import (
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

func TestMatchesLogFilters_noFilters(t *testing.T) {
	entry := LogEntry{Type: LogTypePost, Timestamp: time.Now()}
	opts := &GetLogsOptions{}
	if !matchesLogFilters(entry, opts) {
		t.Error("entry with no filters should match")
	}
}

func TestMatchesLogFilters_typeFilter(t *testing.T) {
	entry := LogEntry{Type: LogTypePost, Timestamp: time.Now()}
	opts := &GetLogsOptions{Types: []LogEntryType{LogTypeComment}}
	if matchesLogFilters(entry, opts) {
		t.Error("post should not match comment filter")
	}
	opts.Types = []LogEntryType{LogTypePost, LogTypeComment}
	if !matchesLogFilters(entry, opts) {
		t.Error("post should match when post is in types")
	}
}

func TestMatchesLogFilters_authorFilter(t *testing.T) {
	entry := LogEntry{Type: LogTypePost, Author: Author{Email: "alice@test.com"}, Timestamp: time.Now()}
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
	entry := LogEntry{Type: LogTypePost, Author: Author{Email: "Alice@Test.com"}, Timestamp: time.Now()}
	opts := &GetLogsOptions{Author: "alice"}
	if !matchesLogFilters(entry, opts) {
		t.Error("author filter should be case-insensitive")
	}
}

func TestMatchesLogFilters_dateFilters(t *testing.T) {
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	entry := LogEntry{Type: LogTypePost, Timestamp: ts}

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
	entry := LogEntry{Type: LogTypePost, Author: Author{Email: "alice@test.com"}, Timestamp: ts}

	after := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	opts := &GetLogsOptions{Types: []LogEntryType{LogTypePost}, Author: "alice", After: &after}
	if !matchesLogFilters(entry, opts) {
		t.Error("should match all combined filters")
	}

	opts.Author = "bob"
	if matchesLogFilters(entry, opts) {
		t.Error("should fail when author doesn't match")
	}
}

func TestDetectLogEntryType_listRef(t *testing.T) {
	refMap := map[string]string{"abc123": "social/list/my-list"}
	commit := git.Commit{Hash: "abc123", Message: "created list"}
	got := detectLogEntryType(commit, nil, refMap)
	if got != LogTypeListCreate {
		t.Errorf("detectLogEntryType() = %q, want %q", got, LogTypeListCreate)
	}
}

func TestDetectLogEntryType_listDeleteRef(t *testing.T) {
	refMap := map[string]string{"abc123": "social/list/my-list"}
	commit := git.Commit{Hash: "abc123", Message: "deleted list"}
	got := detectLogEntryType(commit, nil, refMap)
	if got != LogTypeListDelete {
		t.Errorf("detectLogEntryType() = %q, want %q", got, LogTypeListDelete)
	}
}

func TestDetectLogEntryType_configRef(t *testing.T) {
	refMap := map[string]string{"abc123": "config"}
	commit := git.Commit{Hash: "abc123"}
	got := detectLogEntryType(commit, nil, refMap)
	if got != LogTypeConfig {
		t.Errorf("detectLogEntryType() = %q, want %q", got, LogTypeConfig)
	}
}

func TestDetectLogEntryType_metadataRef(t *testing.T) {
	refMap := map[string]string{"abc123": "social/something"}
	commit := git.Commit{Hash: "abc123"}
	got := detectLogEntryType(commit, nil, refMap)
	if got != LogTypeMetadata {
		t.Errorf("detectLogEntryType() = %q, want %q", got, LogTypeMetadata)
	}
}

func TestDetectLogEntryType_interactionField(t *testing.T) {
	tests := []struct {
		interaction string
		want        LogEntryType
	}{
		{"comment", LogTypeComment},
		{"repost", LogTypeRepost},
		{"quote", LogTypeQuote},
	}
	for _, tt := range tests {
		t.Run(tt.interaction, func(t *testing.T) {
			msg := &protocol.Message{Header: protocol.Header{Fields: map[string]string{"interaction": tt.interaction}}}
			got := detectLogEntryType(git.Commit{Hash: "xyz"}, msg, nil)
			if got != tt.want {
				t.Errorf("detectLogEntryType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectLogEntryType_referenceType(t *testing.T) {
	tests := []struct {
		refType string
		want    LogEntryType
	}{
		{"comment", LogTypeComment},
		{"repost", LogTypeRepost},
		{"quote", LogTypeQuote},
	}
	for _, tt := range tests {
		t.Run(tt.refType, func(t *testing.T) {
			msg := &protocol.Message{
				Header:     protocol.Header{Fields: map[string]string{}},
				References: []protocol.Ref{{Fields: map[string]string{"type": tt.refType}}},
			}
			got := detectLogEntryType(git.Commit{Hash: "xyz"}, msg, nil)
			if got != tt.want {
				t.Errorf("detectLogEntryType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectLogEntryType_defaultPost(t *testing.T) {
	msg := &protocol.Message{Header: protocol.Header{Fields: map[string]string{}}}
	got := detectLogEntryType(git.Commit{Hash: "xyz"}, msg, nil)
	if got != LogTypePost {
		t.Errorf("detectLogEntryType() = %q, want %q", got, LogTypePost)
	}
}

func TestDetectLogEntryType_nilMsg(t *testing.T) {
	got := detectLogEntryType(git.Commit{Hash: "xyz"}, nil, nil)
	if got != LogTypePost {
		t.Errorf("detectLogEntryType(nil msg) = %q, want %q", got, LogTypePost)
	}
}

func TestFormatLogDetails_post(t *testing.T) {
	commit := git.Commit{Message: "Hello world"}
	got := formatLogDetails(commit, nil, LogTypePost)
	if got != "Hello world" {
		t.Errorf("formatLogDetails(post) = %q, want %q", got, "Hello world")
	}
}

func TestFormatLogDetails_comment(t *testing.T) {
	commit := git.Commit{Message: "Nice work"}
	got := formatLogDetails(commit, nil, LogTypeComment)
	if got != "Re: Nice work" {
		t.Errorf("formatLogDetails(comment) = %q, want %q", got, "Re: Nice work")
	}
}

func TestFormatLogDetails_repost(t *testing.T) {
	commit := git.Commit{Message: "Content"}
	got := formatLogDetails(commit, nil, LogTypeRepost)
	if got != "Repost: Content" {
		t.Errorf("formatLogDetails(repost) = %q", got)
	}
}

func TestFormatLogDetails_quote(t *testing.T) {
	commit := git.Commit{Message: "Content"}
	got := formatLogDetails(commit, nil, LogTypeQuote)
	if got != "Quote: Content" {
		t.Errorf("formatLogDetails(quote) = %q", got)
	}
}

func TestFormatLogDetails_listCreate(t *testing.T) {
	got := formatLogDetails(git.Commit{}, nil, LogTypeListCreate)
	if got != "Created list" {
		t.Errorf("formatLogDetails(list-create) = %q", got)
	}
}

func TestFormatLogDetails_listDelete(t *testing.T) {
	got := formatLogDetails(git.Commit{}, nil, LogTypeListDelete)
	if got != "Deleted list" {
		t.Errorf("formatLogDetails(list-delete) = %q", got)
	}
}

func TestFormatLogDetails_config(t *testing.T) {
	got := formatLogDetails(git.Commit{}, nil, LogTypeConfig)
	if got != "Updated config" {
		t.Errorf("formatLogDetails(config) = %q", got)
	}
}

func TestFormatLogDetails_metadata(t *testing.T) {
	got := formatLogDetails(git.Commit{}, nil, LogTypeMetadata)
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
	if entry.Type != LogTypePost {
		t.Errorf("Type = %q, want post", entry.Type)
	}
	if entry.Repository != "origin" {
		t.Errorf("Repository = %q, want origin", entry.Repository)
	}
	if entry.PostID != "abc123def456" {
		t.Errorf("PostID = %q", entry.PostID)
	}
}
