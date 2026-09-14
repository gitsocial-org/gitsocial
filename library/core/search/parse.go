// parse.go - Search query parsing and filter extraction
package search

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

var hashPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// filterPrefixes lists every inline prefix the parser reads, in the order it reads them.
var filterPrefixes = []string{"author", "repo", "repository", "type", "hash", "commit", "list", "after", "before"}

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
	var terms []string

	for _, token := range strings.FieldsFunc(query, unicode.IsSpace) {
		key, value, found := strings.Cut(token, ":")
		if found && value != "" && applyFilter(&result, key, value) {
			continue
		}
		terms = append(terms, token)
	}

	result.Terms = strings.Join(terms, " ")
	return result
}

// applyFilter fills the field a prefix names, and reports whether filterPrefixes lists the prefix.
func applyFilter(q *parsedQuery, key, value string) bool {
	if !slices.Contains(filterPrefixes, key) {
		return false
	}
	switch key {
	case "author":
		q.Author = value
	case "repo", "repository":
		q.Repo = value
	case "type":
		q.Type = value
	case "hash", "commit":
		q.Hash = value
	case "list":
		q.List = value
	case "after":
		if t := parseFilterDate(value); t != nil {
			q.After = t
		}
	case "before":
		if t := parseFilterDate(value); t != nil {
			q.Before = t
		}
	}
	return true
}

// parseFilterDate reads a YYYY-MM-DD filter value, or returns nil.
func parseFilterDate(value string) *time.Time {
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil
	}
	return &t
}
