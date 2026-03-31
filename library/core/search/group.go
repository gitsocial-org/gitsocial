// group.go - Grouping logic for search results
package search

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/gitsocial-org/gitsocial/core/cache"
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

// needsEnrichment returns true if the group-by field requires data from resolved views.
func needsEnrichment(field string) bool {
	switch field {
	case "state", "label", "assignee", "reviewer", "base", "milestone":
		return true
	}
	return false
}

// itemKey identifies a unique item by its composite key.
type itemKey struct {
	repoURL, hash, branch string
}

// enrichForGrouping populates internal grouping fields on items by querying resolved views.
func enrichForGrouping(items []ScoredItem, field string) {
	if len(items) == 0 || !needsEnrichment(field) {
		return
	}

	keyIndex := make(map[itemKey][]int, len(items))
	for i := range items {
		k := itemKey{items[i].RepoURL, items[i].Hash, items[i].Branch}
		keyIndex[k] = append(keyIndex[k], i)
	}

	_ = cache.ExecLocked(func(db *sql.DB) error {
		enrichPM(db, items, keyIndex, field)
		enrichReview(db, items, keyIndex, field)
		if field == "milestone" {
			enrichMilestoneNames(db, items)
		}
		return nil
	})
}

// enrichPM queries pm_items_resolved for state, labels, assignees, scoped to result set items.
func enrichPM(db *sql.DB, items []ScoredItem, keyIndex map[itemKey][]int, field string) {
	if field != "state" && field != "label" && field != "assignee" && field != "milestone" {
		return
	}

	hashFilter, hashArgs := buildHashFilter(keyIndex)
	query := `SELECT repo_url, hash, branch, state, labels, assignees,
		milestone_repo_url, milestone_hash, milestone_branch
		FROM pm_items_resolved WHERE type IN ('issue', 'milestone', 'sprint') AND ` + hashFilter
	rows, err := db.Query(query, hashArgs...)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var repoURL, hash, branch string
		var state, labels, assignees sql.NullString
		var msRepoURL, msHash, msBranch sql.NullString
		if err := rows.Scan(&repoURL, &hash, &branch, &state, &labels, &assignees,
			&msRepoURL, &msHash, &msBranch); err != nil {
			continue
		}
		k := itemKey{repoURL, hash, branch}
		for _, idx := range keyIndex[k] {
			if state.Valid && items[idx].groupState == "" {
				items[idx].groupState = state.String
			}
			if labels.Valid {
				items[idx].groupLabels = labels.String
			}
			if assignees.Valid {
				items[idx].groupAssignees = assignees.String
			}
			if msHash.Valid {
				items[idx].groupMilestone = fmt.Sprintf("%s#%s#%s", msRepoURL.String, msHash.String, msBranch.String)
			}
		}
	}
}

// enrichReview queries review_items_resolved for state, labels, reviewers, base, scoped to result set items.
func enrichReview(db *sql.DB, items []ScoredItem, keyIndex map[itemKey][]int, field string) {
	if field != "state" && field != "label" && field != "reviewer" && field != "base" {
		return
	}

	hashFilter, hashArgs := buildHashFilter(keyIndex)
	query := `SELECT repo_url, hash, branch, state, labels, reviewers, base
		FROM review_items_resolved WHERE type = 'pull-request' AND ` + hashFilter
	rows, err := db.Query(query, hashArgs...)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var repoURL, hash, branch string
		var state, labels, reviewers, base sql.NullString
		if err := rows.Scan(&repoURL, &hash, &branch, &state, &labels, &reviewers, &base); err != nil {
			continue
		}
		k := itemKey{repoURL, hash, branch}
		for _, idx := range keyIndex[k] {
			if state.Valid && items[idx].groupState == "" {
				items[idx].groupState = state.String
			}
			if labels.Valid && items[idx].groupLabels == "" {
				items[idx].groupLabels = labels.String
			}
			if reviewers.Valid {
				items[idx].groupReviewers = reviewers.String
			}
			if base.Valid {
				items[idx].groupBase = base.String
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

// enrichMilestoneNames resolves milestone composite refs to names.
func enrichMilestoneNames(db *sql.DB, items []ScoredItem) {
	msRefs := make(map[string]bool)
	for i := range items {
		if items[i].groupMilestone != "" {
			msRefs[items[i].groupMilestone] = true
		}
	}
	if len(msRefs) == 0 {
		return
	}

	msNames := make(map[string]string)
	for ref := range msRefs {
		parts := strings.SplitN(ref, "#", 3)
		if len(parts) != 3 {
			continue
		}
		var message sql.NullString
		err := db.QueryRow(`SELECT message FROM core_commits WHERE repo_url = ? AND hash = ? AND branch = ? LIMIT 1`,
			parts[0], parts[1], parts[2]).Scan(&message)
		if err == nil && message.Valid {
			subject := message.String
			if idx := strings.IndexByte(subject, '\n'); idx >= 0 {
				subject = subject[:idx]
			}
			msNames[ref] = strings.TrimSpace(subject)
		}
	}

	for i := range items {
		if name, ok := msNames[items[i].groupMilestone]; ok {
			items[i].groupMilestone = name
		}
	}
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

	// Sort groups by count descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].Count > result[j].Count
	})

	return result
}

// extractGroupKeys returns the grouping key(s) for an item. Multi-valued fields return multiple keys.
func extractGroupKeys(item ScoredItem, field string) []string {
	var val string
	switch field {
	case "state":
		val = item.groupState
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
		return splitCSVOrNone(item.groupAssignees)
	case "reviewer":
		return splitCSVOrNone(item.groupReviewers)
	case "milestone":
		val = item.groupMilestone
	case "base":
		val = item.groupBase
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
	if len(subject) > 100 {
		subject = subject[:100] + "..."
	}

	gi := GroupedItem{
		Hash:      item.Hash[:12],
		Subject:   subject,
		Timestamp: item.Timestamp.Format("2006-01-02"),
	}

	// Include context fields that aren't the grouping field itself
	if groupField != "author" {
		gi.Author = item.AuthorName
	}
	if groupField != "state" && item.groupState != "" {
		gi.State = item.groupState
	}
	if groupField != "label" && item.groupLabels != "" {
		gi.Labels = item.groupLabels
	}
	if groupField != "repo" {
		gi.RepoURL = item.RepoURL
	}

	return gi
}
