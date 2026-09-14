// parse_test.go - Tests for search query parsing and the text it keeps
package search

import (
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
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

// TestParseSearchQuery_unknownKeyStaysInText checks a token with an unknown prefix reaches the free text.
func TestParseSearchQuery_unknownKeyStaysInText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "state", input: "state:open", want: "state:open"},
		{name: "label", input: "label:bug", want: "label:bug"},
		{name: "assignee", input: "assignee:dev@example.com", want: "assignee:dev@example.com"},
		{name: "prose colon", input: "error: timeout", want: "error: timeout"},
		{name: "bare url", input: "https://github.com/user/repo", want: "https://github.com/user/repo"},
		{name: "beside a known prefix", input: "author:alice state:open", want: "state:open"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSearchQuery(tt.input)
			if got.Terms != tt.want {
				t.Errorf("parseSearchQuery(%q).Terms = %q, want %q", tt.input, got.Terms, tt.want)
			}
		})
	}
}

// TestParseSearchQuery_knownPrefixesFillTheirFields checks every prefix the parser lists fills one field.
func TestParseSearchQuery_knownPrefixesFillTheirFields(t *testing.T) {
	for _, prefix := range filterPrefixes {
		t.Run(prefix, func(t *testing.T) {
			query := prefix + ":2026-01-02"
			got := parseSearchQuery(query)
			if got.Terms != "" {
				t.Errorf("parseSearchQuery(%q).Terms = %q, want empty", query, got.Terms)
			}
			if filled := filledFields(got); len(filled) != 1 {
				t.Errorf("parseSearchQuery(%q) filled %v, want one field", query, filled)
			}
		})
	}
}

// filledFields names the filter fields a parsed query carries.
func filledFields(q parsedQuery) []string {
	var names []string
	for _, field := range canonicalKeys {
		if filterValue(field, q) != "" {
			names = append(names, field)
		}
	}
	return names
}

// seedTextCommit writes one social commit with the given subject into the open test cache.
func seedTextCommit(t *testing.T, repoURL, hash, subject string) {
	t.Helper()
	message := protocol.FormatMessage(subject, protocol.Header{Ext: "social", V: "1", Fields: map[string]string{"type": "post"}}, nil)
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: repoURL, Branch: "main",
		AuthorName: "Alice", AuthorEmail: "alice@test.com",
		Message:   message,
		Timestamp: time.Date(2026, 3, 14, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("InsertCommits %s: %v", hash, err)
	}
}

// TestSearch_unknownKeySearchesAsText checks an unknown prefix fills no filter and matches as text.
func TestSearch_unknownKeySearchesAsText(t *testing.T) {
	testutil.OpenTempCache(t, "")
	workdir := t.TempDir()
	repoURL := gitmsg.ResolveRepoURL(workdir)
	const carriesTokens = "aaaaaaaaaaaa1111"
	seedTextCommit(t, repoURL, carriesTokens, "Filters state:open and label:bug read as text")
	seedTextCommit(t, repoURL, "bbbbbbbbbbbb2222", "Nothing to filter here")

	for _, query := range []string{"state:open", "label:bug"} {
		result, err := Search(workdir, Params{Query: query})
		if err != nil {
			t.Fatalf("Search(%q): %v", query, err)
		}
		if len(result.Results) != 1 {
			t.Fatalf("Search(%q) returned %d items, want the one item spelling it", query, len(result.Results))
		}
		if result.Results[0].Hash != carriesTokens {
			t.Errorf("Search(%q) matched %s, want %s", query, result.Results[0].Hash, carriesTokens)
		}
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
