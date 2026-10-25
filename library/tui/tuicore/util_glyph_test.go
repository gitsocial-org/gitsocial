// util_glyph_test.go - Tests for the type and state glyph table
package tuicore

import "testing"

// TestTypeGlyph_matchesSiteTable pins each row of the site's glyph table.
func TestTypeGlyph_matchesSiteTable(t *testing.T) {
	cases := []struct{ itemType, state, want string }{
		{"issue", "", "☐"},
		{"issue", "open", "☐"},
		{"issue", "closed", "☑"},
		{"issue", "completed", "☑"},
		{"issue", "canceled", "☒"},
		{"pull-request", "", "△"},
		{"pull-request", "open", "△"},
		{"pull-request", "closed", "△"},
		{"pull-request", "merged", "▲"},
		{"milestone", "", "◇"},
		{"milestone", "open", "◇"},
		{"milestone", "closed", "◆"},
		{"milestone", "canceled", "◆"},
		{"sprint", "", "◷"},
		{"sprint", "planned", "◷"},
		{"sprint", "active", "◶"},
		{"sprint", "completed", "◵"},
		{"sprint", "canceled", "⊘"},
		{"post", "", "•"},
		{"comment", "", "↩"},
		{"feedback", "", "↩"},
		{"repost", "", "↻"},
		{"quote", "", "↻"},
		{"release", "", "⏏"},
		{"memo", "", "☞"},
		{"branch", "", "⎇"},
		{"tag", "", "⌗"},
		{"list", "", "☷"},
		{"commit", "", ""},
		{"unknown", "open", ""},
	}
	for _, c := range cases {
		if got := TypeGlyph(c.itemType, c.state); got != c.want {
			t.Errorf("TypeGlyph(%q, %q) = %q, want %q", c.itemType, c.state, got, c.want)
		}
	}
}
