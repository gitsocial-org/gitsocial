// util_helpers_test.go - Tests for follow status and list indicator helpers
package tuisocial

import (
	"testing"
)

func TestFormatListIndicator(t *testing.T) {
	tests := []struct {
		name       string
		names      []string
		maxVisible int
		want       string
	}{
		{"empty", nil, 2, ""},
		{"single", []string{"Friends"}, 2, "[Friends]"},
		{"two within limit", []string{"Friends", "Work"}, 2, "[Friends, Work]"},
		{"three exceeds limit", []string{"A", "B", "C"}, 2, "[A, B +1 more]"},
		{"many", []string{"A", "B", "C", "D", "E"}, 2, "[A, B +3 more]"},
		{"exact limit", []string{"A", "B"}, 2, "[A, B]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatListIndicator(tt.names, tt.maxVisible)
			if got != tt.want {
				t.Errorf("FormatListIndicator(%v, %d) = %q, want %q", tt.names, tt.maxVisible, got, tt.want)
			}
		})
	}
}

func TestExtractSearchTerms(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"no filters", "hello world", "hello world"},
		{"repo filter", "repository:user/repo hello", "hello"},
		{"author filter", "author:alice hello", "hello"},
		{"type filter", "type:post hello", "hello"},
		{"multiple filters", "repository:a author:b hello world", "hello world"},
		{"filter only", "repository:user/repo", ""},
		{"empty", "", ""},
		{"hash filter", "hash:abc123 search term", "search term"},
		{"after filter", "after:2025-01-01 hello", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractSearchTerms(tt.query)
			if got != tt.want {
				t.Errorf("ExtractSearchTerms(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}
