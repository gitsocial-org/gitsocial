// parse_test.go - Tests for search query parsing
package search

import (
	"testing"
)

func TestParseSearchQuery(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, q parsedQuery)
	}{
		{
			name:  "empty query",
			input: "",
			check: func(t *testing.T, q parsedQuery) {
				if q.Terms != "" {
					t.Errorf("Terms = %q, want empty", q.Terms)
				}
			},
		},
		{
			name:  "text only",
			input: "hello world",
			check: func(t *testing.T, q parsedQuery) {
				if q.Terms != "hello world" {
					t.Errorf("Terms = %q, want %q", q.Terms, "hello world")
				}
			},
		},
		{
			name:  "author filter",
			input: "author:alice@test.com hello",
			check: func(t *testing.T, q parsedQuery) {
				if q.Author != "alice@test.com" {
					t.Errorf("Author = %q, want alice@test.com", q.Author)
				}
				if q.Terms != "hello" {
					t.Errorf("Terms = %q, want hello", q.Terms)
				}
			},
		},
		{
			name:  "repo filter",
			input: "repo:github.com/user/repo",
			check: func(t *testing.T, q parsedQuery) {
				if q.Repo != "github.com/user/repo" {
					t.Errorf("Repo = %q", q.Repo)
				}
			},
		},
		{
			name:  "type filter",
			input: "type:comment",
			check: func(t *testing.T, q parsedQuery) {
				if q.Type != "comment" {
					t.Errorf("Type = %q, want comment", q.Type)
				}
			},
		},
		{
			name:  "multiple filters",
			input: "author:alice type:post search terms",
			check: func(t *testing.T, q parsedQuery) {
				if q.Author != "alice" {
					t.Errorf("Author = %q", q.Author)
				}
				if q.Type != "post" {
					t.Errorf("Type = %q", q.Type)
				}
				if q.Terms != "search terms" {
					t.Errorf("Terms = %q", q.Terms)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSearchQuery(tt.input)
			tt.check(t, got)
		})
	}
}

func TestParseSearchQuery_hashFilter(t *testing.T) {
	q := parseSearchQuery("hash:abc123")
	if q.Hash != "abc123" {
		t.Errorf("Hash = %q, want abc123", q.Hash)
	}
}

func TestParseSearchQuery_commitFilter(t *testing.T) {
	q := parseSearchQuery("commit:def456")
	if q.Hash != "def456" {
		t.Errorf("Hash = %q, want def456", q.Hash)
	}
}

func TestParseSearchQuery_listFilter(t *testing.T) {
	q := parseSearchQuery("list:my-list")
	if q.List != "my-list" {
		t.Errorf("List = %q, want my-list", q.List)
	}
}

func TestParseSearchQuery_repositoryAlias(t *testing.T) {
	q := parseSearchQuery("repository:github.com/a/b")
	if q.Repo != "github.com/a/b" {
		t.Errorf("Repo = %q, want github.com/a/b", q.Repo)
	}
}

func TestParseSearchQuery_invalidDate(t *testing.T) {
	q := parseSearchQuery("after:not-a-date before:also-not")
	if q.After != nil {
		t.Error("After should be nil for invalid date")
	}
	if q.Before != nil {
		t.Error("Before should be nil for invalid date")
	}
}

func TestParseSearchQuery_dateFilters(t *testing.T) {
	q := parseSearchQuery("after:2025-01-01 before:2025-12-31")
	if q.After == nil {
		t.Fatal("After should not be nil")
	}
	if q.Before == nil {
		t.Fatal("Before should not be nil")
	}
	if q.After.Year() != 2025 || q.After.Month() != 1 || q.After.Day() != 1 {
		t.Errorf("After = %v, want 2025-01-01", q.After)
	}
	if q.Before.Year() != 2025 || q.Before.Month() != 12 || q.Before.Day() != 31 {
		t.Errorf("Before = %v, want 2025-12-31", q.Before)
	}
}
