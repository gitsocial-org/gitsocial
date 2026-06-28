// acceptances.go - Acceptance state: a cross-repo proposal was accepted, keyed on
// the proposing edit. Derived from the mirror edit's accepts= header; local-only
// bookkeeping that clears the proposed-edit marker and gives accept idempotency.
package cache

import "database/sql"

// RecordAcceptance records that the proposing edit was accepted (idempotent).
func RecordAcceptance(editRepoURL, editHash, editBranch string) error {
	return ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`
			INSERT OR IGNORE INTO core_edit_acceptances
			(edit_repo_url, edit_hash, edit_branch) VALUES (?, ?, ?)`,
			editRepoURL, editHash, editBranch)
		return err
	})
}

// HasAcceptance reports whether the proposing edit has already been accepted.
func HasAcceptance(editRepoURL, editHash, editBranch string) (bool, error) {
	return QueryLocked(func(db *sql.DB) (bool, error) {
		var n int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_edit_acceptances
			WHERE edit_repo_url = ? AND edit_hash = ? AND edit_branch = ?`,
			editRepoURL, editHash, editBranch).Scan(&n)
		return n > 0, err
	})
}
