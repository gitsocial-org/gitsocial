// mentions_test.go - Tests for mention extraction
package notifications

import (
	"testing"
)

func TestExtractMentions(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "single mention",
			input: "Hello @alice@example.com",
			want:  []string{"alice@example.com"},
		},
		{
			name:  "multiple mentions",
			input: "@alice@example.com and @bob@example.com",
			want:  []string{"alice@example.com", "bob@example.com"},
		},
		{
			name:  "duplicate mentions deduplicated",
			input: "@alice@example.com hello @alice@example.com",
			want:  []string{"alice@example.com"},
		},
		{
			name:  "mention at start of line",
			input: "@alice@example.com mentioned",
			want:  []string{"alice@example.com"},
		},
		{
			name:  "mention in middle of text",
			input: "Hey @alice@example.com check this",
			want:  []string{"alice@example.com"},
		},
		{
			name:  "no mentions",
			input: "Just a regular message",
			want:  nil,
		},
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "bare email not a mention",
			input: "Email alice@example.com here",
			want:  nil,
		},
		{
			name:  "case normalized to lowercase",
			input: "@Alice@Example.COM",
			want:  []string{"alice@example.com"},
		},
		{
			name:  "multiline mentions",
			input: "Line 1 @alice@example.com\nLine 2 @bob@example.com",
			want:  []string{"alice@example.com", "bob@example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractMentions(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("ExtractMentions() = %v (len %d), want %v (len %d)", got, len(got), tt.want, len(tt.want))
			}
			for i, email := range got {
				if email != tt.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, email, tt.want[i])
				}
			}
		})
	}
}
