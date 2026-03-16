// util_anchor.go - AnchorCollector unifies zone marking, collection, and focus styling for clickable card elements
package tuicore

import (
	"fmt"

	zone "github.com/lrstanley/bubblezone/v2"
)

// FocusedLinkMarker is the ANSI prefix applied to focused links, used for scroll detection.
const FocusedLinkMarker = "\x1b[1;4;48;5;236m"

// AnchorCollector tracks zone-marked clickable elements during rendering.
// All methods are nil-safe: calling Mark/Zones/Count on a nil receiver is a no-op.
type AnchorCollector struct {
	prefix  string
	zones   []CardLinkZone
	focused int // -1 = none
}

// NewAnchorCollector creates an AnchorCollector with the given zone prefix and focused link index.
func NewAnchorCollector(prefix string, focused int) *AnchorCollector {
	return &AnchorCollector{prefix: prefix, focused: focused}
}

// Mark zone-marks text for mouse interaction and registers the zone for hit-testing.
// Applies underline styling when this link is focused. Returns text unchanged on nil receiver.
func (a *AnchorCollector) Mark(text string, loc Location) string {
	if a == nil {
		return text
	}
	id := fmt.Sprintf("%s_%d", a.prefix, len(a.zones))
	a.zones = append(a.zones, CardLinkZone{ZoneID: id, Location: loc})
	if a.focused == len(a.zones)-1 {
		// Use raw ANSI codes instead of lipgloss. Lipgloss Underline(true) enables
		// useSpaceStyler which iterates character-by-character, shattering any existing
		// ANSI escape codes in the text (from TitleStyle, Dim, etc.).
		// Bold (\x1b[1m) + underline (\x1b[4m) + bg 236 (\x1b[48;5;236m) for focused links.
		text = FocusedLinkMarker + text + "\x1b[22;24;49m"
	}
	return zone.Mark(id, text)
}

// MarkLink wraps text in a styled OSC 8 hyperlink and registers it as a navigable anchor.
// Combines Hyperlink + Mark into a single call. Safe on nil receiver.
func (a *AnchorCollector) MarkLink(text, url string, loc Location) string {
	text = Hyperlink(url, text)
	return a.Mark(text, loc)
}

// Zones returns all collected link zones. Returns nil on nil receiver.
func (a *AnchorCollector) Zones() []CardLinkZone {
	if a == nil {
		return nil
	}
	return a.zones
}

// Count returns the number of collected zones. Returns 0 on nil receiver.
func (a *AnchorCollector) Count() int {
	if a == nil {
		return 0
	}
	return len(a.zones)
}
