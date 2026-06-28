// declines.go - Decline state: the owner's durable record that a cross-repo
// proposal was declined, keyed on the proposing edit. Mirrors acceptances and is
// published at refs/gitmsg/core/declines/* (see core/gitmsg) so the proposer
// learns and the owner's choice survives a re-clone. Clears the proposed marker.
package cache

import "database/sql"

// RecordDecline records that the proposing edit was declined. Idempotent.
func RecordDecline(editRepoURL, editHash, editBranch string) error {
	return ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`
			INSERT OR IGNORE INTO core_edit_declines
			(edit_repo_url, edit_hash, edit_branch) VALUES (?, ?, ?)`,
			editRepoURL, editHash, editBranch)
		return err
	})
}

// HasDecline reports whether the proposing edit has already been declined.
func HasDecline(editRepoURL, editHash, editBranch string) (bool, error) {
	return QueryLocked(func(db *sql.DB) (bool, error) {
		var n int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_edit_declines
			WHERE edit_repo_url = ? AND edit_hash = ? AND edit_branch = ?`,
			editRepoURL, editHash, editBranch).Scan(&n)
		return n > 0, err
	})
}
