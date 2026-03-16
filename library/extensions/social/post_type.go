// post_type.go - Social post type classification
package social

import "github.com/gitsocial-org/gitsocial/core/protocol"

// socialFieldOrder declares the spec-defined field ordering for social headers (GITSOCIAL.md 1.2).
var socialFieldOrder = []string{"reply-to", "original"}

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
