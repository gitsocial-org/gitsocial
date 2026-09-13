// followers.go - Follower tracking and detection from remote lists
package social

import (
	"database/sql"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// insertFollower records a repository that follows the workspace.
func insertFollower(repoURL, workspaceURL, listID, commitHash string, followedAt time.Time) error {
	ts := followedAt.UTC().Format(time.RFC3339)
	return cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`
			INSERT OR REPLACE INTO social_followers (repo_url, workspace_url, detected_at, list_id, commit_hash)
			VALUES (?, ?, ?, ?, ?)
		`, protocol.NormalizeURL(repoURL), protocol.NormalizeURL(workspaceURL), ts, listID, commitHash)
		return err
	})
}

// GetFollowers returns the repository URLs that follow the workspace, newest first.
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

// GetFollowerSet returns the same repository URLs as a lookup set.
func GetFollowerSet(workspaceURL string) (map[string]bool, error) {
	urls, err := GetFollowers(workspaceURL)
	if err != nil {
		return nil, err
	}
	followers := make(map[string]bool, len(urls))
	for _, url := range urls {
		followers[url] = true
	}
	return followers, nil
}
