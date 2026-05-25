// post_type.go - Social post type classification
package social

import (
	"sort"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// socialFieldOrder declares the spec-defined field ordering for social headers (GITSOCIAL.md 1.2).
var socialFieldOrder = []string{"reply-to", "original", "labels"}

// joinSocialLabels produces a deterministic comma-separated label string,
// trimming whitespace, dropping empties, and deduplicating.
func joinSocialLabels(labels []string) string {
	cleaned := make([]string, 0, len(labels))
	seen := make(map[string]bool, len(labels))
	for _, l := range labels {
		l = strings.TrimSpace(l)
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		cleaned = append(cleaned, l)
	}
	sort.Strings(cleaned)
	return strings.Join(cleaned, ",")
}

// splitSocialLabels parses a comma-separated label string into a clean slice.
func splitSocialLabels(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

type PostType string

const (
	PostTypePost    PostType = "post"
	PostTypeComment PostType = "comment"
	PostTypeRepost  PostType = "repost"
	PostTypeQuote   PostType = "quote"
)

// GetPostType returns the social post type from a message.
func GetPostType(msg *protocol.Message) PostType {
	if msg == nil || msg.Header.Ext != "social" {
		return PostTypePost
	}
	switch msg.Header.Fields["type"] {
	case "comment":
		return PostTypeComment
	case "repost":
		return PostTypeRepost
	case "quote":
		return PostTypeQuote
	default:
		return PostTypePost
	}
}

// IsEmptyRepost checks if a repost has no additional content.
func IsEmptyRepost(msg *protocol.Message) bool {
	if msg == nil || !protocol.IsMessageType(&msg.Header, "social", "repost") {
		return false
	}
	content := msg.Content
	if len(content) == 0 {
		return true
	}
	for i, c := range content {
		if c == '\n' {
			return content[0] == '#' && i == len(content)-1
		}
		if i == 0 && c != '#' {
			return false
		}
	}
	return content[0] == '#'
}
