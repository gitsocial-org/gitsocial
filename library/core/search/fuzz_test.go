// fuzz_test.go - Fuzz target for the search query parser
package search

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// canonicalKeys lists the filter fields in the order a re-joined query spells them.
var canonicalKeys = []string{"author", "repo", "type", "hash", "list", "after", "before"}

// filterAliases maps each filter field to every prefix that fills it.
var filterAliases = map[string][]string{
	"author": {"author"},
	"repo":   {"repo", "repository"},
	"type":   {"type"},
	"hash":   {"hash", "commit"},
	"list":   {"list"},
	"after":  {"after"},
	"before": {"before"},
}

// filterValue returns the parsed value of one filter field.
func filterValue(field string, q parsedQuery) string {
	switch field {
	case "author":
		return q.Author
	case "repo":
		return q.Repo
	case "type":
		return q.Type
	case "hash":
		return q.Hash
	case "list":
		return q.List
	case "after":
		return formatFilterDate(q.After)
	case "before":
		return formatFilterDate(q.Before)
	}
	return ""
}

// formatFilterDate spells a date filter in the layout the parser reads.
func formatFilterDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

// sameParsedQuery reports whether two parsed queries carry the same filters and free text.
func sameParsedQuery(a, b parsedQuery) bool {
	for _, field := range canonicalKeys {
		if filterValue(field, a) != filterValue(field, b) {
			return false
		}
	}
	return a.Terms == b.Terms
}

// rejoinQuery spells the parsed parts back as a query, one canonical prefix per field.
func rejoinQuery(q parsedQuery) string {
	parts := make([]string, 0, len(canonicalKeys)+1)
	for _, field := range canonicalKeys {
		if value := filterValue(field, q); value != "" {
			parts = append(parts, field+":"+value)
		}
	}
	if q.Terms != "" {
		parts = append(parts, q.Terms)
	}
	return strings.Join(parts, " ")
}

// checkFilterValues asserts every filter value is one run of the query, taken whole after its prefix.
func checkFilterValues(t *testing.T, query string, q parsedQuery) {
	t.Helper()
	for _, field := range canonicalKeys {
		value := filterValue(field, q)
		if value == "" {
			continue
		}
		// The pattern takes a run of non-space characters, and its class leaves out the vertical tab of "repo:\v".
		if strings.ContainsAny(value, " \t\n\f\r") {
			t.Fatalf("parseSearchQuery(%q) %s = %q, want no whitespace", query, field, value)
		}
		if field == "after" || field == "before" {
			continue
		}
		// A repeated prefix keeps the inner one, so a value may start with a prefix of its own.
		if !containsFilter(query, filterAliases[field], value) {
			t.Fatalf("parseSearchQuery(%q) %s = %q, want a value the query spells after its prefix", query, field, value)
		}
	}
}

// containsFilter reports whether the query spells the value after one of the prefixes.
func containsFilter(query string, keys []string, value string) bool {
	for _, key := range keys {
		if strings.Contains(query, key+":"+value) {
			return true
		}
	}
	return false
}

// checkFTSWellFormed asserts the FTS5 query built from a string closes every quote it opens.
func checkFTSWellFormed(t *testing.T, text string) {
	t.Helper()
	fts := ftsQuery(text)
	if strings.Count(fts, `"`)%2 != 0 {
		t.Fatalf("ftsQuery(%q) = %q, want an even number of quotes", text, fts)
	}
	if len(strings.Fields(text)) == 0 {
		// Text of nothing but spaces passes through, and carries no quote to balance.
		if fts != text {
			t.Fatalf("ftsQuery(%q) = %q, want the text unchanged", text, fts)
		}
		return
	}
	if !strings.HasPrefix(fts, `"`) || !strings.HasSuffix(fts, "*") {
		t.Fatalf("ftsQuery(%q) = %q, want quoted prefix terms", text, fts)
	}
}

// TestSearchQueryPrefixes_documentedSetAccepted checks the prefixes the parser reads.
func TestSearchQueryPrefixes_documentedSetAccepted(t *testing.T) {
	// CLI.md documents six inline prefixes; the parser also reads repository, commit and list.
	documented := []string{"author", "repo", "type", "hash", "after", "before"}
	accepted := []string{"author", "repo", "repository", "type", "hash", "commit", "list", "after", "before"}
	fieldOf := map[string]string{"repository": "repo", "commit": "hash"}
	for _, key := range accepted {
		field := key
		if canonical, ok := fieldOf[key]; ok {
			field = canonical
		}
		// One value serves every prefix: it reads as a date and as plain text.
		got := filterValue(field, parseSearchQuery(key+":2025-01-02"))
		if got != "2025-01-02" {
			t.Errorf("parseSearchQuery(%q) %s = %q, want 2025-01-02", key+":2025-01-02", field, got)
		}
	}
	for _, key := range documented {
		if !slices.Contains(accepted, key) {
			t.Errorf("prefix %q is documented in CLI.md but the parser drops it", key)
		}
	}
}

// FuzzParseSearchQuery checks the search query parser against a re-join of the parts it returns.
func FuzzParseSearchQuery(f *testing.F) {
	seeds := []string{
		"author:alice@example.com",
		"repo:github.com/user/repo",
		"repository:https://github.com/user/repo",
		"type:issue",
		"hash:abc1234",
		"commit:abc1234def5678",
		"list:reading",
		"after:2025-01-01",
		"before:2025-12-31",
		"author:alice type:pr after:2025-01-01 before:2025-12-31 release notes",
		"state:open label:bug assignee:dev@example.com",
		`"quoted phrase"`,
		`author:"alice smith" quoted value`,
		`unterminated "quote in the text`,
		"author:",
		"author: alice",
		"type:",
		"repo:https://github.com/user/repo#branch:main",
		"author:author:alice",
		"after:not-a-date before:9999-99-99",
		"поиск по тексту тип:значение",
		"日本語 type:post",
		"",
		"   ",
		"\n\t hello \r\n",
		strings.Repeat("a", 8192),
		strings.Repeat("author:a ", 500),
		"a:b a:b:c ab:c",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, query string) {
		parsed := parseSearchQuery(query)
		checkFilterValues(t, query, parsed)
		checkFTSWellFormed(t, parsed.Terms)
		checkFTSWellFormed(t, parsed.Author)
		// The parts re-joined with spaces parse back to the same filters and free text.
		rejoined := rejoinQuery(parsed)
		if again := parseSearchQuery(rejoined); !sameParsedQuery(again, parsed) {
			t.Errorf("parseSearchQuery(%q) = %+v, want the parts of %q %+v", rejoined, again, query, parsed)
		}
	})
}
