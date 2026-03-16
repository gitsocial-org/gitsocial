// trailers.go - Git trailer extraction from commit messages
package protocol

import (
	"regexp"
	"strings"
)

// Trailer represents a parsed git trailer (Key: Value) from a commit message.
type Trailer struct {
	Key   string // Fixes, Closes, Resolves, Implements, Refs
	Value string // Raw value (GitMsg ref, URL, or opaque ID)
}

var trailerPattern = regexp.MustCompile(`^(Fixes|Closes|Resolves|Implements|Refs):\s+(.+)$`)

// ExtractTrailers parses recognized git trailers from the commit message footer.
// Returns nil if no trailers are found.
func ExtractTrailers(message string) []Trailer {
	lines := strings.Split(message, "\n")
	// Find the last blank line — trailers follow it
	blankIdx := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			blankIdx = i
			break
		}
	}
	if blankIdx < 0 || blankIdx >= len(lines)-1 {
		return nil
	}
	var trailers []Trailer
	for i := blankIdx + 1; i < len(lines); i++ {
		m := trailerPattern.FindStringSubmatch(lines[i])
		if len(m) == 3 {
			trailers = append(trailers, Trailer{
				Key:   m[1],
				Value: strings.TrimSpace(m[2]),
			})
		}
	}
	return trailers
}

// IsClosingTrailer returns true if the trailer key implies closing/completing an item.
func IsClosingTrailer(key string) bool {
	switch key {
	case "Fixes", "Closes", "Resolves", "Implements":
		return true
	}
	return false
}
