// facet_test.go - Tests for the assignee, reviewer and label facets over the seeded corpus
package search

import (
	"slices"
	"sort"
	"testing"
)

// facetHashes runs one query over the corpus repository and returns the hashes it finds, sorted.
func facetHashes(t *testing.T, query string) []string {
	t.Helper()
	result, err := Search(t.TempDir(), Params{Query: query, Repo: testRepoURL})
	if err != nil {
		t.Fatalf("Search(%q): %v", query, err)
	}
	hashes := make([]string, 0, len(result.Results))
	for _, item := range result.Results {
		hashes = append(hashes, item.Hash)
	}
	sort.Strings(hashes)
	return hashes
}

// TestSearch_facets checks each inline facet narrows the corpus the way its flag does.
func TestSearch_facets(t *testing.T) {
	seedCorpus(t)
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"assignee by email", "assignee:alice@test.com", []string{hashIssueOpen}},
		{"assignee by a part of the email", "assignee:bob", []string{hashIssueOpen, hashIssueClosed}},
		{"assignee without case", "assignee:BOB", []string{hashIssueOpen, hashIssueClosed}},
		{"reviewer", "reviewer:carol", []string{hashPullRequest}},
		{"label across extensions", "label:ui", []string{hashIssueOpen, hashPullRequest, hashRelease}},
		{"label without case", "label:UI", []string{hashIssueOpen, hashPullRequest, hashRelease}},
		{"labels alias with any match", "labels:ui,none", []string{hashIssueOpen, hashPullRequest, hashRelease}},
		{"label with text", "label:bug parser", []string{hashIssueOpen}},
		{"label nobody carries", "label:docs", nil},
		{"wildcard is a character", "assignee:%", nil},
		{"underscore is a character", "reviewer:c_rol", nil},
		{"assignee implies the issue type", "assignee:test.com", []string{hashIssueOpen, hashIssueClosed}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sort.Strings(tt.want)
			if got := facetHashes(t, tt.query); !slices.Equal(got, tt.want) {
				t.Errorf("Search(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

// TestSearch_facetFlagWins checks the flag value takes priority over the inline facet.
func TestSearch_facetFlagWins(t *testing.T) {
	seedCorpus(t)
	result, err := Search(t.TempDir(), Params{Query: "assignee:alice", Assignee: "bob@test.com", Repo: testRepoURL})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Results) != 2 {
		t.Fatalf("Search returned %d items, want the 2 issues of bob", len(result.Results))
	}
}
