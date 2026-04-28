// csv.go - Helpers for normalizing comma-separated header fields (labels,
// assignees, reviewers) into linking-table rows for indexed search.
package cache

import (
	"strings"
)

// SplitCSVField splits a comma-separated string, trimming whitespace and
// dropping empty entries. Used by linking-table maintenance to turn
// "alice@x.com, bob@y.com" into ["alice@x.com", "bob@y.com"].
func SplitCSVField(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// RebuildCSVLinkingTable replaces the rows in linkTable for the given commit
// with one row per value in csv. Idempotent. valueCol is the column on
// linkTable that holds the value (e.g., "label", "email"). The table must
// have the standard (repo_url, hash, branch, valueCol) shape.
//
// Callers must invoke this in the same transaction as the comma-string
// column write so search can never observe a stale linking-table state.
func RebuildCSVLinkingTable(tx sqlExecutor, linkTable, valueCol, repoURL, hash, branch, csv string) error {
	if _, err := tx.Exec(`DELETE FROM `+linkTable+` WHERE repo_url = ? AND hash = ? AND branch = ?`,
		repoURL, hash, branch); err != nil {
		return err
	}
	for _, v := range SplitCSVField(csv) {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO `+linkTable+` (repo_url, hash, branch, `+valueCol+`) VALUES (?, ?, ?, ?)`,
			repoURL, hash, branch, v); err != nil {
			return err
		}
	}
	return nil
}
