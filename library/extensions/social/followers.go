// followers.go - Follower tracking and detection from remote lists
package social

import (
	"database/sql"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// InsertFollower records a repository that follows the workspace.
func InsertFollower(repoURL, workspaceURL, listID, commitHash string, followedAt time.Time) error {
	ts := followedAt.UTC().Format(time.RFC3339)
	return cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`
			INSERT OR REPLACE INTO social_followers (repo_url, workspace_url, detected_at, list_id, commit_hash)
			VALUES (?, ?, ?, ?, ?)
		`, protocol.NormalizeURL(repoURL), protocol.NormalizeURL(workspaceURL), ts, listID, commitHash)
		return err
	})
}

// GetFollowerSet returns a set of repository URLs that follow the workspace.
func GetFollowerSet(workspaceURL string) (map[string]bool, error) {
	return cache.QueryLocked(func(db *sql.DB) (map[string]bool, error) {
		rows, err := db.Query(`
			SELECT repo_url FROM social_followers WHERE workspace_url = ?
		`, protocol.NormalizeURL(workspaceURL))
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		followers := make(map[string]bool)
		for rows.Next() {
			var repoURL string
			if err := rows.Scan(&repoURL); err != nil {
				continue
			}
			followers[repoURL] = true
		}
		return followers, nil
	})
}

// GetFollowers returns a list of repository URLs that follow the workspace.
func GetFollowers(workspaceURL string) ([]string, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]string, error) {
		rows, err := db.Query(`
			SELECT repo_url FROM social_followers WHERE workspace_url = ? ORDER BY detected_at DESC
		`, protocol.NormalizeURL(workspaceURL))
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var followers []string
		for rows.Next() {
			var repoURL string
			if err := rows.Scan(&repoURL); err != nil {
				continue
			}
			followers = append(followers, repoURL)
		}
		return followers, nil
	})
}
