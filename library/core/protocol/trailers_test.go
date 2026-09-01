// trailers_test.go - Tests for git trailer extraction from commit messages
package protocol

import "testing"

func TestExtractTrailers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		message string
		want    []Trailer
	}{
		{
			name:    "empty message",
			message: "",
		},
		{
			name:    "subject only, no footer",
			message: "Fix the login bug",
		},
		{
			name:    "body without trailers",
			message: "Fix the login bug\n\nThe session cookie expired too early.",
		},
		{
			name:    "single closing trailer",
			message: "Fix the login bug\n\nCloses: #commit:abc123456789",
			want:    []Trailer{{Key: "Closes", Value: "#commit:abc123456789"}},
		},
		{
			name:    "trailing newline still yields the trailer",
			message: "Fix the login bug\n\nCloses: #commit:abc123456789\n",
			want:    []Trailer{{Key: "Closes", Value: "#commit:abc123456789"}},
		},
		{
			name:    "trailing blank lines and spaces",
			message: "Fix the login bug\n\nRefs: #commit:abc123456789\n  \n\n",
			want:    []Trailer{{Key: "Refs", Value: "#commit:abc123456789"}},
		},
		{
			name:    "all recognized keys",
			message: "Subject\n\nFixes: #commit:aaa111222333\nCloses: #commit:bbb111222333\nResolves: #commit:ccc111222333\nImplements: #commit:ddd111222333\nRefs: #commit:eee111222333",
			want: []Trailer{
				{Key: "Fixes", Value: "#commit:aaa111222333"},
				{Key: "Closes", Value: "#commit:bbb111222333"},
				{Key: "Resolves", Value: "#commit:ccc111222333"},
				{Key: "Implements", Value: "#commit:ddd111222333"},
				{Key: "Refs", Value: "#commit:eee111222333"},
			},
		},
		{
			name:    "unrecognized keys are ignored",
			message: "Subject\n\nSigned-off-by: Alice <alice@example.com>\nCloses: #commit:abc123456789\nReviewed-by: Bob",
			want:    []Trailer{{Key: "Closes", Value: "#commit:abc123456789"}},
		},
		{
			name:    "lowercase key is not a trailer",
			message: "Subject\n\ncloses: #commit:abc123456789",
		},
		{
			name:    "trailer in the body is ignored, only the footer counts",
			message: "Subject\n\nCloses: #commit:aaa111222333\n\nRefs: #commit:bbb111222333",
			want:    []Trailer{{Key: "Refs", Value: "#commit:bbb111222333"}},
		},
		{
			name:    "value whitespace is trimmed",
			message: "Subject\n\nCloses:   #commit:abc123456789   ",
			want:    []Trailer{{Key: "Closes", Value: "#commit:abc123456789"}},
		},
		{
			name:    "key with no space before the value is not a trailer",
			message: "Subject\n\nCloses:#commit:abc123456789",
		},
		{
			name:    "opaque and url values are kept verbatim",
			message: "Subject\n\nCloses: https://github.com/o/r/issues/7\nRefs: JIRA-42",
			want: []Trailer{
				{Key: "Closes", Value: "https://github.com/o/r/issues/7"},
				{Key: "Refs", Value: "JIRA-42"},
			},
		},
		{
			name:    "cross-repo ref value",
			message: "Subject\n\nFixes: https://github.com/o/r#commit:abc123456789@gitmsg/pm",
			want:    []Trailer{{Key: "Fixes", Value: "https://github.com/o/r#commit:abc123456789@gitmsg/pm"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractTrailers(tt.message)
			if len(got) != len(tt.want) {
				t.Fatalf("ExtractTrailers() = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("trailer[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestIsClosingTrailer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key  string
		want bool
	}{
		{"Fixes", true},
		{"Closes", true},
		{"Resolves", true},
		{"Implements", true},
		{"Refs", false},
		{"closes", false},
		{"Signed-off-by", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsClosingTrailer(tt.key); got != tt.want {
			t.Errorf("IsClosingTrailer(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}
