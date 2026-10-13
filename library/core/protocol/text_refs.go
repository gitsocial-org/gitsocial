// text_refs.go - Refs and mentions found in free text
package protocol

import "regexp"

var (
	// reTextRef is the one free-text ref grammar; REF_LINK_RE in gs-render.js is its JS copy, pinned by TestParityRefTokens. A value ends on a letter, so a trailing period stays outside.
	reTextRef = regexp.MustCompile(`(?:^|[ \t\n\r\f([{"'<])(#(?:commit:[0-9a-f]{7,40}(?:@[\w./-]*[\w/-])?|(?:branch|tag|list):[\w./-]*[\w/-]|file:[\w./-]+@[\w./-]*[\w/-](?::(?:L\d+(?:-\d+)?|[\w./-]*[\w/-]))?))`)
	// reTextMention is the one free-text mention grammar; MENTION_RE in gs-render.js is its JS copy.
	reTextMention = regexp.MustCompile(`(?:^|[^@])(@([a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}))`)
	reTextURL     = regexp.MustCompile(`https?://[^\s)>\]"]+`)
	reTextMDLink  = regexp.MustCompile(`\[[^\]]*\]\([^)]*\)`)
)

// RefSpan is a ref found in free text, at its byte offsets.
type RefSpan struct {
	Start int
	End   int
	Ref   string
}

// MentionSpan is a mention found in free text, at its byte offsets, with the email it names.
type MentionSpan struct {
	Start int
	End   int
	Email string
}

// FindRefs returns each workspace-relative ref in text that starts at a boundary, outside a URL and a markdown link.
func FindRefs(text string) []RefSpan {
	covered := append(reTextURL.FindAllStringIndex(text, -1), reTextMDLink.FindAllStringIndex(text, -1)...)
	var spans []RefSpan
	for _, m := range reTextRef.FindAllStringSubmatchIndex(text, -1) {
		if insideSpan(covered, m[2]) {
			continue
		}
		spans = append(spans, RefSpan{Start: m[2], End: m[3], Ref: text[m[2]:m[3]]})
	}
	return spans
}

// FindMentions returns each @email mention in text, with the email as written.
func FindMentions(text string) []MentionSpan {
	matches := reTextMention.FindAllStringSubmatchIndex(text, -1)
	spans := make([]MentionSpan, 0, len(matches))
	for _, m := range matches {
		spans = append(spans, MentionSpan{Start: m[2], End: m[3], Email: text[m[4]:m[5]]})
	}
	return spans
}

// insideSpan reports whether offset falls inside one of the spans.
func insideSpan(spans [][]int, offset int) bool {
	for _, s := range spans {
		if offset >= s[0] && offset < s[1] {
			return true
		}
	}
	return false
}
