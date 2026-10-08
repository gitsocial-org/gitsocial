// actions.go - The action a commit shows on the timeline, derived from its header and the version before it
package cache

import (
	"database/sql"
	"fmt"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// headerFields returns the header fields of a message, or an empty map when it has no header.
func headerFields(message string) map[string]string {
	if msg := protocol.ParseMessage(message); msg != nil {
		return msg.Header.Fields
	}
	return map[string]string{}
}

// headerAction returns the action of a version from its header fields and those of the version before it; a nil prev is a first version.
func headerAction(prev, cur map[string]string) string {
	if cur["retracted"] == "true" {
		return ""
	}
	if review := cur["review-state"]; review != "" && (prev == nil || review != prev["review-state"]) {
		return review
	}
	if prev == nil {
		return ""
	}
	state := cur["state"]
	switch {
	case state != "" && state != prev["state"]:
		return stateAction(state)
	case prev["draft"] == "true" && cur["draft"] != "true" && state == "open":
		return "ready"
	}
	return ""
}

// stateAction names the change to a state: an item that opens again is reopened, a sprint that goes active is started, and a return to planned is no action.
func stateAction(state string) string {
	switch state {
	case "open":
		return "reopened"
	case "active":
		return "started"
	case "planned":
		return ""
	}
	return state
}

// writeEditActions writes the action of each same-repo edit of a canonical to each row of the edit's hash, in the order of the versions.
func writeEditActions(tx sqlExecutor, canonicalRepoURL, canonicalHash, canonicalMessage string) error {
	rows, err := tx.Query(`SELECT DISTINCT v.edit_hash, c.message, c.timestamp `+sameRepoEdits("")+
		` ORDER BY c.timestamp, v.edit_hash`, canonicalRepoURL, canonicalHash)
	if err != nil {
		return fmt.Errorf("apply edit: read versions of %s/%s: %w", canonicalRepoURL, canonicalHash, err)
	}
	actions := map[string]string{}
	var order []string
	prev := headerFields(canonicalMessage)
	for rows.Next() {
		var hash, message, timestamp string
		if err := rows.Scan(&hash, &message, &timestamp); err != nil {
			rows.Close()
			return fmt.Errorf("apply edit: scan version: %w", err)
		}
		cur := headerFields(message)
		order = append(order, hash)
		actions[hash] = headerAction(prev, cur)
		if cur["retracted"] == "true" {
			continue
		}
		// A version that omits a field keeps the value of the version before it.
		for _, field := range []string{"state", "review-state"} {
			if cur[field] == "" {
				cur[field] = prev[field]
			}
		}
		prev = cur
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("apply edit: read versions: %w", err)
	}
	for _, hash := range order {
		if _, err := tx.Exec(`UPDATE core_commits SET action = ? WHERE repo_url = ? AND hash = ?`,
			sql.NullString{String: actions[hash], Valid: actions[hash] != ""}, canonicalRepoURL, hash); err != nil {
			return fmt.Errorf("apply edit: write action of %s: %w", hash, err)
		}
	}
	return nil
}
