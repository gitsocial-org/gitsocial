// group.go - Grouping logic for search results
package search

import (
	"sort"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/text"
)

// validGroupByFields lists fields that can be used with --group-by.
var validGroupByFields = map[string]bool{
	"state":     true,
	"author":    true,
	"type":      true,
	"extension": true,
	"repo":      true,
	"label":     true,
	"assignee":  true,
	"reviewer":  true,
	"milestone": true,
	"base":      true,
}

// IsValidGroupBy checks if a field name is valid for grouping.
func IsValidGroupBy(field string) bool {
	return validGroupByFields[field]
}

// groupBy groups scored items by the specified field and builds the Groups slice on Result.
func groupBy(items []ScoredItem, field string, top int, countOnly bool) []Group {
	type groupEntry struct {
		items []ScoredItem
	}
	groups := make(map[string]*groupEntry)
	var order []string

	for i := range items {
		keys := extractGroupKeys(items[i], field)
		for _, key := range keys {
			g, exists := groups[key]
			if !exists {
				g = &groupEntry{}
				groups[key] = g
				order = append(order, key)
			}
			g.items = append(g.items, items[i])
		}
	}

	result := make([]Group, 0, len(order))
	for _, key := range order {
		g := groups[key]
		group := Group{
			Key:   key,
			Count: len(g.items),
		}
		if !countOnly {
			limit := len(g.items)
			if top > 0 && top < limit {
				limit = top
			}
			group.Items = make([]GroupedItem, 0, limit)
			for j := 0; j < limit; j++ {
				group.Items = append(group.Items, toGroupedItem(g.items[j], field))
			}
		}
		result = append(result, group)
	}

	// Largest group first, then by key, so equal counts keep one order.
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Key < result[j].Key
	})

	return result
}

// extractGroupKeys returns the grouping key(s) for an item. Multi-valued fields return multiple keys.
func extractGroupKeys(item ScoredItem, field string) []string {
	var val string
	switch field {
	case "state":
		val = item.State
	case "author":
		val = item.AuthorEmail
	case "type":
		val = item.Type
	case "extension":
		val = item.Extension
	case "repo":
		val = item.RepoURL
	case "label":
		return splitCSVOrNone(item.Labels)
	case "assignee":
		return splitCSVOrNone(item.Assignees)
	case "reviewer":
		return splitCSVOrNone(item.Reviewers)
	case "milestone":
		val = item.Milestone
	case "base":
		val = item.Base
	}
	if val == "" {
		return []string{"(none)"}
	}
	return []string{val}
}

// splitCSVOrNone splits a comma-separated string into trimmed values, or returns ["(none)"] if empty.
func splitCSVOrNone(s string) []string {
	if s == "" {
		return []string{"(none)"}
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return []string{"(none)"}
	}
	return result
}

// toGroupedItem creates a compact item representation, including context fields based on group-by field.
func toGroupedItem(item ScoredItem, groupField string) GroupedItem {
	subject := strings.TrimSpace(item.Content)
	if idx := strings.IndexByte(subject, '\n'); idx >= 0 {
		subject = subject[:idx]
	}
	subject = text.Truncate(subject, 100)

	gi := GroupedItem{
		Hash:      item.Hash[:12],
		Subject:   subject,
		Timestamp: item.Timestamp.Format("2006-01-02"),
	}

	// Include context fields that aren't the grouping field itself
	if groupField != "author" {
		gi.Author = item.AuthorName
	}
	if groupField != "state" {
		gi.State = item.State
	}
	if groupField != "label" {
		gi.Labels = item.Labels
	}
	if groupField != "repo" {
		gi.RepoURL = item.RepoURL
	}

	return gi
}
