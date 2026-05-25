// split.go - String splitting helpers shared across packages
package text

import "strings"

// SplitCSV parses a comma-separated string into a clean slice: trims each
// element, drops empties. Returns nil for empty or whitespace-only input.
func SplitCSV(s string) []string {
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
