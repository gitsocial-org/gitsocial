// parse.go - Search query parsing and filter extraction
package search

import (
	"regexp"
	"strings"
	"time"
)

var filterPattern = regexp.MustCompile(`(\w+):(\S+)`)
var hashPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

type parsedQuery struct {
	Terms  string
	Author string
	Repo   string
	Type   string
	Hash   string
	List   string
	After  *time.Time
	Before *time.Time
}

// parseSearchQuery extracts filters and terms from a search query string.
func parseSearchQuery(query string) parsedQuery {
	result := parsedQuery{}
	remaining := query

	matches := filterPattern.FindAllStringSubmatch(query, -1)
	for _, match := range matches {
		key, value := match[1], match[2]
		remaining = strings.Replace(remaining, match[0], "", 1)

		switch key {
		case "author":
			result.Author = value
		case "repo", "repository":
			result.Repo = value
		case "type":
			result.Type = value
		case "hash", "commit":
			result.Hash = value
		case "list":
			result.List = value
		case "after":
			if t, err := time.Parse("2006-01-02", value); err == nil {
				result.After = &t
			}
		case "before":
			if t, err := time.Parse("2006-01-02", value); err == nil {
				result.Before = &t
			}
		}
	}

	result.Terms = strings.TrimSpace(remaining)
	return result
}
