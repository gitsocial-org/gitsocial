// branches.go - The branches of a repository as the cache holds them
package cache

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// BranchSummary is one branch of a repository with its live commit count and its last commit time.
type BranchSummary struct {
	Name     string
	Commits  int
	LastTime time.Time
}

// GetRepositoryBranches lists the branches of a repository from the live, fetched rows the Repository view shows: code branches first, then gitmsg/* branches, each by last commit time.
func GetRepositoryBranches(repoURL string) ([]BranchSummary, error) {
	repoURL = protocol.NormalizeURL(repoURL)
	return QueryLocked(func(db *sql.DB) ([]BranchSummary, error) {
		rows, err := db.Query(`
			SELECT branch, COUNT(*), MAX(timestamp) AS last
			FROM core_commits
			WHERE repo_url = ? AND is_virtual = 0 AND stale_since IS NULL
			  AND is_edit_commit = 0 AND is_retracted = 0
			  AND branch NOT LIKE 'refs/gitmsg/%'
			GROUP BY branch
			ORDER BY branch LIKE 'gitmsg/%', last DESC, branch`, repoURL)
		if err != nil {
			return nil, fmt.Errorf("query branches: %w", err)
		}
		defer rows.Close()
		var branches []BranchSummary
		for rows.Next() {
			var b BranchSummary
			var last string
			if err := rows.Scan(&b.Name, &b.Commits, &last); err != nil {
				return nil, fmt.Errorf("scan branch: %w", err)
			}
			b.LastTime, _ = time.Parse(time.RFC3339, last) // RFC3339 from the insert; a zero time is fine
			branches = append(branches, b)
		}
		return branches, rows.Err()
	})
}
