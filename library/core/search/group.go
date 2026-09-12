// group.go - Grouping logic for search results
package search

import (
	"database/sql"
	"sort"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
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

// itemKey identifies a unique item by its composite key.
type itemKey struct {
	repoURL, hash, branch string
}

// enrichForGrouping populates the label grouping field on items.
func enrichForGrouping(items []ScoredItem, field string) {
	if len(items) == 0 || field != "label" {
		return
	}

	keyIndex := make(map[itemKey][]int, len(items))
	for i := range items {
		k := itemKey{items[i].RepoURL, items[i].Hash, items[i].Branch}
		keyIndex[k] = append(keyIndex[k], i)
	}

	_ = cache.ExecLocked(func(db *sql.DB) error {
		enrichPM(db, items, keyIndex)
		enrichReview(db, items, keyIndex)
		return nil
	})
}

// enrichPM queries pm_items for labels, scoped to result set items.
func enrichPM(db *sql.DB, items []ScoredItem, keyIndex map[itemKey][]int) {
	hashFilter, hashArgs := buildHashFilter(keyIndex)
	query := `SELECT repo_url, hash, branch, labels
		FROM pm_items WHERE type IN ('issue', 'milestone', 'sprint') AND ` + hashFilter
	rows, err := db.Query(query, hashArgs...)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var repoURL, hash, branch string
		var labels sql.NullString
		if err := rows.Scan(&repoURL, &hash, &branch, &labels); err != nil {
			continue
		}
		for _, idx := range keyIndex[itemKey{repoURL, hash, branch}] {
			if labels.Valid {
				items[idx].groupLabels = labels.String
			}
		}
	}
}

// enrichReview queries review_items for labels, scoped to result set items.
func enrichReview(db *sql.DB, items []ScoredItem, keyIndex map[itemKey][]int) {
	hashFilter, hashArgs := buildHashFilter(keyIndex)
	query := `SELECT repo_url, hash, branch,
		(SELECT c.labels FROM core_commits c WHERE c.repo_url = review_items.repo_url
			AND c.hash = review_items.hash AND c.branch = review_items.branch) AS labels
		FROM review_items WHERE type = 'pull-request' AND ` + hashFilter
	rows, err := db.Query(query, hashArgs...)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var repoURL, hash, branch string
		var labels sql.NullString
		if err := rows.Scan(&repoURL, &hash, &branch, &labels); err != nil {
			continue
		}
		for _, idx := range keyIndex[itemKey{repoURL, hash, branch}] {
			if labels.Valid && items[idx].groupLabels == "" {
				items[idx].groupLabels = labels.String
			}
		}
	}
}

// buildHashFilter builds a hash IN clause from the keyIndex to scope enrichment queries to result set items.
func buildHashFilter(keyIndex map[itemKey][]int) (string, []interface{}) {
	hashes := make(map[string]bool, len(keyIndex))
	for k := range keyIndex {
		hashes[k.hash] = true
	}
	args := make([]interface{}, 0, len(hashes))
	for h := range hashes {
		args = append(args, h)
	}
	ph := strings.Repeat("?,", len(args))
	ph = ph[:len(ph)-1]
	return "hash IN (" + ph + ")", args
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
		return splitCSVOrNone(item.groupLabels)
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
	if groupField != "label" && item.groupLabels != "" {
		gi.Labels = item.groupLabels
	}
	if groupField != "repo" {
		gi.RepoURL = item.RepoURL
	}

	return gi
}
