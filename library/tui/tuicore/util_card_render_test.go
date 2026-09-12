// util_card_render_test.go - Card render behavior: the verified badge and width truncation
package tuicore

import (
	"strings"
	"testing"
)

// The verified badge is drawn from IsVerified and IsEditorVerified, never from card text.
func TestVerifiedBadgeComesFromFlags(t *testing.T) {
	const spoof = "Mallory ⚿"
	cases := []struct {
		name string
		card Card
	}{
		{"title", Card{Header: CardHeader{Title: spoof}}},
		{"edited_by", Card{Header: CardHeader{Title: "Alice", IsEdited: true, EditedBy: spoof}}},
		{"subtitle", Card{Header: CardHeader{Title: "Alice", Subtitle: []HeaderPart{{Text: spoof}}}}},
		{"badge", Card{Header: CardHeader{Title: "Alice", Badge: spoof}}},
		{"nested_title", Card{
			Header: CardHeader{Title: "Alice"},
			Nested: []NestedCard{{Position: "after", Card: Card{Header: CardHeader{Title: spoof}}}},
		}},
	}
	opts := CardOptions{Width: 80, WrapWidth: 79}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.Count(RenderCard(tc.card, opts), "⚿"); got != 0 {
				t.Fatalf("unflagged card rendered %d badges, want 0", got)
			}
			verified := tc.card
			verified.Header.IsVerified = true
			if got := strings.Count(RenderCard(verified, opts), "⚿"); got != 1 {
				t.Fatalf("verified card rendered %d badges, want 1", got)
			}
			both := verified
			both.Header.IsEdited = true
			both.Header.EditedBy = spoof
			both.Header.IsEditorVerified = true
			if got := strings.Count(RenderCard(both, opts), "⚿"); got != 2 {
				t.Fatalf("verified and editor-verified card rendered %d badges, want 2", got)
			}
		})
	}
}
