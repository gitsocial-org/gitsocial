// clear.go - Cache clearing operations for database and repositories
package cache

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// repoRowDeletes removes the rows of one repository from each table keyed by it; the FTS rows go first, while core_commits still names their rowids.
var repoRowDeletes = []string{
	"DELETE FROM core_fts WHERE rowid IN (SELECT rowid FROM core_commits WHERE repo_url = ?)",
	"DELETE FROM pm_assignees WHERE repo_url = ?",
	"DELETE FROM pm_links WHERE from_repo_url = ? OR to_repo_url = ?",
	"DELETE FROM pm_items WHERE repo_url = ?",
	"DELETE FROM review_reviewers WHERE repo_url = ?",
	"DELETE FROM review_branch_observations WHERE repo_url = ?",
	"DELETE FROM review_items WHERE repo_url = ?",
	"DELETE FROM release_sbom_cache WHERE repo_url = ?",
	"DELETE FROM release_items WHERE repo_url = ?",
	"DELETE FROM memo_items WHERE repo_url = ?",
	"DELETE FROM social_items WHERE repo_url = ?",
	"DELETE FROM social_interactions WHERE repo_url = ?",
	"DELETE FROM core_commits_version WHERE edit_repo_url = ? OR canonical_repo_url = ?",
	"DELETE FROM core_edit_acceptances WHERE edit_repo_url = ?",
	"DELETE FROM core_edit_declines WHERE edit_repo_url = ?",
	"DELETE FROM core_notification_reads WHERE repo_url = ?",
	"DELETE FROM core_mentions WHERE repo_url = ?",
	"DELETE FROM core_trailer_refs WHERE repo_url = ?",
	"DELETE FROM core_labels WHERE repo_url = ?",
	"DELETE FROM core_commits WHERE repo_url = ?",
}

// repoRecordDeletes removes the repository itself: its record, its fetch ranges, its list memberships and its follower rows.
var repoRecordDeletes = []string{
	"DELETE FROM social_followers WHERE repo_url = ? OR workspace_url = ?",
	"DELETE FROM social_repo_list_repositories WHERE owner_repo_url = ?",
	"DELETE FROM social_repo_lists WHERE repo_url = ?",
	"DELETE FROM core_fetch_ranges WHERE repo_url = ?",
	"DELETE FROM core_list_repositories WHERE repo_url = ?",
	"DELETE FROM core_repositories WHERE url = ?",
}

// deleteRepoRows runs each delete with the repository URL in every placeholder; a table of an extension that is not registered is skipped.
func deleteRepoRows(tx *sql.Tx, repoURL string, deletes []string) error {
	for _, q := range deletes {
		args := make([]interface{}, strings.Count(q, "?"))
		for i := range args {
			args[i] = repoURL
		}
		if _, err := tx.Exec(q, args...); err != nil && !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("delete %q: %w", q, err)
		}
	}
	return nil
}

// ClearDatabase deletes the SQLite database file.
func ClearDatabase(cacheDir string) error {
	Reset()
	dbPath := filepath.Join(cacheDir, "cache.db")
	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ClearRepositories removes all cached repository directories.
func ClearRepositories(cacheDir string) error {
	reposDir := filepath.Join(cacheDir, "repositories")
	if err := os.RemoveAll(reposDir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ClearForks removes all fork bare repo directories.
func ClearForks(cacheDir string) error {
	forksDir := filepath.Join(cacheDir, "forks")
	if err := os.RemoveAll(forksDir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// DeleteRepository deletes every row of one repository, its record included.
func DeleteRepository(repoURL string) error {
	return ExecLocked(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback() }()
		if err := deleteRepoRows(tx, repoURL, repoRowDeletes); err != nil {
			return err
		}
		if err := deleteRepoRows(tx, repoURL, repoRecordDeletes); err != nil {
			return err
		}
		return tx.Commit()
	})
}

// ClearAll clears database, repositories, and forks.
func ClearAll(cacheDir string) error {
	if err := ClearDatabase(cacheDir); err != nil {
		return err
	}
	if err := ClearRepositories(cacheDir); err != nil {
		return err
	}
	return ClearForks(cacheDir)
}
