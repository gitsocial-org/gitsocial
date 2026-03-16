// clear.go - Cache clearing operations for database and repositories
package cache

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

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

// DeleteRepository cascade-deletes all data for a single repo URL from the database.
func DeleteRepository(repoURL string) error {
	return ExecLocked(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback() }()
		deletes := []string{
			"DELETE FROM social_items WHERE repo_url = ?",
			"DELETE FROM social_interactions WHERE repo_url = ?",
			"DELETE FROM social_followers WHERE repo_url = ? OR workspace_url = ?",
			"DELETE FROM social_repo_list_repositories WHERE owner_repo_url = ?",
			"DELETE FROM social_repo_lists WHERE repo_url = ?",
			"DELETE FROM pm_items WHERE repo_url = ?",
			"DELETE FROM pm_links WHERE from_repo_url = ? OR to_repo_url = ?",
			"DELETE FROM release_items WHERE repo_url = ?",
			"DELETE FROM review_items WHERE repo_url = ?",
			"DELETE FROM core_commits_version WHERE edit_repo_url = ? OR canonical_repo_url = ?",
			"DELETE FROM core_notification_reads WHERE repo_url = ?",
			"DELETE FROM core_mentions WHERE repo_url = ?",
			"DELETE FROM core_fetch_ranges WHERE repo_url = ?",
			"DELETE FROM core_list_repositories WHERE repo_url = ?",
			"DELETE FROM core_commits WHERE repo_url = ?",
			"DELETE FROM core_repositories WHERE url = ?",
		}
		for _, q := range deletes {
			// Count ? placeholders to determine args
			argCount := 0
			for _, c := range q {
				if c == '?' {
					argCount++
				}
			}
			args := make([]interface{}, argCount)
			for i := range args {
				args[i] = repoURL
			}
			if _, err := tx.Exec(q, args...); err != nil {
				return fmt.Errorf("delete %q: %w", q, err)
			}
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
