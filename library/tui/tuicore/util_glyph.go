// util_glyph.go - The glyph of an item type in a state
package tuicore

import "strings"

// TypeGlyph returns the glyph of an item type in a state, "" for a commit; it mirrors typeGlyph in gs-core.js and sitePageGlyph in site_pages_html.go.
func TypeGlyph(itemType, state string) string {
	canceled := strings.HasPrefix(state, "cancel")
	switch itemType {
	case "issue":
		switch {
		case canceled:
			return "☒"
		case state == "closed" || state == "completed":
			return "☑"
		}
		return "☐"
	case "pull-request":
		if state == "merged" {
			return "▲"
		}
		return "△"
	case "milestone":
		if state == "closed" || canceled {
			return "◆"
		}
		return "◇"
	case "sprint":
		switch {
		case state == "active":
			return "◶"
		case state == "completed":
			return "◵"
		case canceled:
			return "⊘"
		}
		return "◷"
	case "post":
		return "•"
	case "comment", "feedback":
		return "↩"
	case "repost", "quote":
		return "↻"
	case "release":
		return "⏏"
	case "memo":
		return "☞"
	case "branch":
		return "⎇"
	case "tag":
		return "⌗"
	case "list":
		return "☷"
	}
	return ""
}
