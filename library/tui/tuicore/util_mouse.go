// util_mouse.go - Bubblezone helpers for declarative mouse hit-testing
package tuicore

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
)

// ZoneID builds a zone identifier from prefix + index.
func ZoneID(prefix string, index int) string {
	return fmt.Sprintf("%s%d", prefix, index)
}

// ZoneClicked iterates zones 0..count-1 and returns the first index where
// the mouse event is in bounds. Returns -1 if no zone matches.
func ZoneClicked(msg tea.MouseMsg, count int, prefix string) int {
	for i := 0; i < count; i++ {
		if zone.Get(ZoneID(prefix, i)).InBounds(msg) {
			return i
		}
	}
	return -1
}

// MarkZone wraps a single line with a zone.Mark for the given id.
func MarkZone(id, line string) string {
	return zone.Mark(id, line)
}

// LinkZoneClicked checks a set of link zones and returns the matching Location.
// Returns nil if no link zone was clicked.
func LinkZoneClicked(msg tea.MouseMsg, linkZones []CardLinkZone) *Location {
	for _, lz := range linkZones {
		if zone.Get(lz.ZoneID).InBounds(msg) {
			loc := lz.Location
			return &loc
		}
	}
	return nil
}

// CardLinkZone associates a zone ID with its navigation target.
type CardLinkZone struct {
	ZoneID   string
	Location Location
}
