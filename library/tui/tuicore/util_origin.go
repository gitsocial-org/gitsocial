// util_origin.go - Render helpers for imported content origin display
package tuicore

import (
	"strings"
	"time"
	"unicode"

	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// FormatOriginBadge returns "↙ platform" for card subtitles.
// Returns "" when origin is nil.
func FormatOriginBadge(origin *protocol.Origin) string {
	if origin == nil {
		return ""
	}
	platform := origin.Platform
	if platform == "" {
		platform = "imported"
	}
	return "↙ " + platform
}

// FormatOriginAuthor returns the origin author display name.
// Uses AuthorName directly when available.
// Falls back to email-based parsing for backward compat (old origin-author data).
// Returns "" when origin is nil or both fields are empty.
func FormatOriginAuthor(origin *protocol.Origin) string {
	if origin == nil {
		return ""
	}
	if origin.AuthorName != "" {
		return origin.AuthorName
	}
	if origin.AuthorEmail == "" {
		return ""
	}
	email := origin.AuthorEmail
	if strings.HasSuffix(email, "@users.noreply.github.com") {
		login := strings.TrimSuffix(email, "@users.noreply.github.com")
		// GitHub may prefix with numeric ID: "12345+octocat"
		if idx := strings.Index(login, "+"); idx >= 0 {
			login = login[idx+1:]
		}
		return "@" + login
	}
	if idx := strings.Index(email, "@"); idx > 0 {
		return "@" + email[:idx]
	}
	return "@" + email
}

// FormatOriginAuthorDisplay returns the origin author name, optionally with email.
// Respects the display.show_email setting. Returns "" when origin has no author.
func FormatOriginAuthorDisplay(origin *protocol.Origin, showEmail bool) string {
	name := FormatOriginAuthor(origin)
	if name == "" {
		return ""
	}
	if showEmail && origin != nil && origin.AuthorEmail != "" {
		name += " <" + origin.AuthorEmail + ">"
	}
	return name
}

// FormatOriginTime parses an ISO 8601 origin time and formats it for display.
func FormatOriginTime(originTime string) string {
	if originTime == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, originTime)
	if err != nil {
		return originTime
	}
	return FormatTime(t)
}

// capitalizeFirst returns the string with the first letter uppercased.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// RenderOriginRows returns detail-view metadata rows using RowStylesWithWidths.
// Returns nil when origin is nil.
func RenderOriginRows(origin *protocol.Origin, styles RowStyles, selectionBar string, anchors *AnchorCollector, showEmail ...bool) []string {
	if origin == nil {
		return nil
	}
	se := len(showEmail) > 0 && showEmail[0]
	var lines []string
	// Build "Origin" row: @author via Platform · time
	var parts []string
	if author := FormatOriginAuthorDisplay(origin, se); author != "" {
		parts = append(parts, author)
	}
	if origin.Platform != "" {
		parts = append(parts, "via "+capitalizeFirst(origin.Platform))
	}
	if origin.Time != "" {
		if t, err := time.Parse(time.RFC3339, origin.Time); err == nil {
			parts = append(parts, t.Format("Jan 2, 2006"))
		} else {
			parts = append(parts, origin.Time)
		}
	}
	if len(parts) > 0 {
		lines = append(lines, selectionBar+styles.Label.Render("Origin")+styles.Value.Render(strings.Join(parts, " · ")))
	}
	// "Source URL" row (strip https:// for readability, hyperlink to full URL)
	if origin.URL != "" {
		display := origin.URL
		display = strings.TrimPrefix(display, "https://")
		display = strings.TrimPrefix(display, "http://")
		link := anchors.MarkLink(display, origin.URL, Location{Path: origin.URL})
		lines = append(lines, selectionBar+styles.Label.Render("Source URL")+link)
	}
	return lines
}
