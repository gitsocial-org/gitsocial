// util_card_render_test.go - Card render behavior: the verified badge and width truncation
package tuicore

import (
	"strings"
	"testing"
)

// lastSGR returns the last color or attribute sequence in s, and whether one is present.
func lastSGR(s string) (string, bool) {
	last, found := "", false
	for i := 0; i < len(s); {
		if s[i] != '\x1b' {
			i++
			continue
		}
		end := escapeEnd(s, i)
		if seq := s[i:end]; strings.HasSuffix(seq, "m") {
			last, found = seq, true
		}
		i = end
	}
	return last, found
}

// hasBrokenEscape reports whether s ends inside an escape sequence.
func hasBrokenEscape(s string) bool {
	for i := 0; i < len(s); {
		if s[i] != '\x1b' {
			i++
			continue
		}
		end := escapeEnd(s, i)
		last := s[end-1]
		terminated := last == '\x07' || (last >= 0x40 && last <= 0x5a) || (last >= 0x61 && last <= 0x7a)
		if !terminated {
			return true
		}
		i = end
	}
	return false
}

// linkLeftOpen reports whether s opens an OSC 8 hyperlink it never closes.
func linkLeftOpen(s string) bool {
	open := false
	for i := 0; i < len(s); {
		if s[i] != '\x1b' {
			i++
			continue
		}
		end := escapeEnd(s, i)
		if link, opens := osc8(s[i:end]); link {
			open = opens
		}
		i = end
	}
	return open
}

// A truncated string fits the width, keeps its escapes whole, and closes an open style or link.
func TestTruncateToWidth(t *testing.T) {
	inputs := []string{
		"abcdefgh\x1b[38;5;196mij",
		"abc\x1b[38;5;196mdefghij",
		"\x1b[38;5;196mred text here\x1b[0m and more",
		"plain text here",
		"héllo wörld ünicode 日本語",
		"\x1b]8;;http://x\x07link\x1b]8;;\x07 tail",
		Hyperlink("http://x", "a linked label") + " tail",
	}
	for _, in := range inputs {
		full := AnsiWidth(in)
		for w := 1; w <= full; w++ {
			got := TruncateToWidth(in, w)
			if AnsiWidth(got) > w {
				t.Fatalf("width %d of %q: %q is %d cells wide", w, in, got, AnsiWidth(got))
			}
			if hasBrokenEscape(got) {
				t.Fatalf("width %d of %q: %q ends inside an escape", w, in, got)
			}
			if got == in {
				continue
			}
			if seq, ok := lastSGR(got); ok && seq != "\x1b[0m" {
				t.Fatalf("width %d of %q: %q leaves %q open", w, in, got, seq)
			}
			if linkLeftOpen(got) {
				t.Fatalf("width %d of %q: %q leaves a hyperlink open", w, in, got)
			}
		}
	}
}

// A card rendered with Bold draws every body line bold and closes the style; without it, none.
func TestBoldCardBodyLines(t *testing.T) {
	card := Card{
		Header:  CardHeader{Title: "Alice"},
		Content: CardContent{Text: "first body line\n\nsecond body line"},
	}
	for _, markdown := range []bool{true, false} {
		opts := CardOptions{Width: 80, WrapWidth: 79, MaxLines: -1, Markdown: markdown}
		boldOpts := opts
		boldOpts.Bold = true
		lines := bodyLines(RenderCard(card, boldOpts))
		if len(lines) != 2 {
			t.Fatalf("markdown %v: found %d body lines, want 2", markdown, len(lines))
		}
		for _, line := range lines {
			if !strings.Contains(line, "\x1b[1m") {
				t.Fatalf("markdown %v: body line %q is not bold", markdown, line)
			}
			if !strings.HasSuffix(line, "\x1b[22m") {
				t.Fatalf("markdown %v: body line %q leaves bold open", markdown, line)
			}
		}
		for _, line := range bodyLines(RenderCard(card, opts)) {
			if strings.Contains(line, "\x1b[1m") {
				t.Fatalf("markdown %v: unbolded body line %q is bold", markdown, line)
			}
		}
	}
}

// bodyLines returns the rendered lines carrying the test card's body text.
func bodyLines(rendered string) []string {
	var out []string
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "body") {
			out = append(out, line)
		}
	}
	return out
}

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
