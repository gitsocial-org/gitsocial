// types.go - Protocol data types for headers, messages, and refs
package protocol

import "strings"

type Header struct {
	Ext        string
	V          string
	Fields     map[string]string
	FieldOrder []string // extension-declared priority fields placed before alphabetical remainder
}

type Ref struct {
	Ext        string
	Ref        string
	V          string
	Author     string
	Email      string
	Time       string
	Fields     map[string]string
	Metadata   string
	FieldOrder []string // extension-declared priority fields placed before alphabetical remainder
}

type Message struct {
	Content    string
	Header     Header
	References []Ref
}

// Origin holds provenance metadata for content imported from external platforms.
type Origin struct {
	AuthorEmail string // email address of original author
	AuthorName  string // display name of original author
	Time        string // ISO 8601 timestamp of original creation
	URL         string // URL to original item on source platform
	Platform    string // source platform name (e.g., "github")
}

// ApplyOrigin adds origin-* fields to a header fields map.
func ApplyOrigin(fields map[string]string, origin *Origin) {
	if origin == nil {
		return
	}
	if origin.AuthorEmail != "" {
		fields["origin-author-email"] = origin.AuthorEmail
	}
	if origin.AuthorName != "" {
		fields["origin-author-name"] = origin.AuthorName
	}
	if origin.Platform != "" {
		fields["origin-platform"] = origin.Platform
	}
	if origin.Time != "" {
		fields["origin-time"] = origin.Time
	}
	if origin.URL != "" {
		fields["origin-url"] = origin.URL
	}
}

// ExtractOrigin reads origin-* fields from a parsed header and returns an Origin.
// Returns nil when no origin fields are present.
func ExtractOrigin(header *Header) *Origin {
	if header == nil {
		return nil
	}
	authorEmail := header.Fields["origin-author-email"]
	authorName := header.Fields["origin-author-name"]
	// Backward compat: old format used "origin-author" for email
	if authorEmail == "" {
		authorEmail = header.Fields["origin-author"]
	}
	platform := header.Fields["origin-platform"]
	t := header.Fields["origin-time"]
	url := header.Fields["origin-url"]
	if authorEmail == "" && authorName == "" && platform == "" && t == "" && url == "" {
		return nil
	}
	return &Origin{AuthorEmail: authorEmail, AuthorName: authorName, Platform: platform, Time: t, URL: url}
}

// IsMessageType checks if a header matches extension and type.
func IsMessageType(header *Header, ext, msgType string) bool {
	if header == nil {
		return false
	}
	return header.Ext == ext && header.Fields["type"] == msgType
}

// SplitSubjectBody splits content into a subject (first line) and body (rest).
func SplitSubjectBody(content string) (subject, body string) {
	content = strings.TrimSpace(content)
	if idx := strings.Index(content, "\n"); idx > 0 {
		subject = strings.TrimSpace(content[:idx])
		body = strings.TrimSpace(content[idx+1:])
	} else {
		subject = content
	}
	return
}
