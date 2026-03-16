// trailer_refs.go - Query trailer references from regular commits
package cache

import (
	"database/sql"
	"time"
)

// TrailerRef represents a commit that references a GitMsg item via a git trailer.
type TrailerRef struct {
	RepoURL     string
	Hash        string
	Branch      string
	AuthorName  string
	AuthorEmail string
	Message     string
	TrailerKey  string
	Timestamp   time.Time
}

// GetTrailerRefsTo returns commits that reference the given item via git trailers.
func GetTrailerRefsTo(refRepoURL, refHash, refBranch string) ([]TrailerRef, error) {
	return QueryLocked(func(db *sql.DB) ([]TrailerRef, error) {
		rows, err := db.Query(`
			SELECT t.repo_url, t.hash, t.branch, t.trailer_key,
			       c.author_name, c.author_email, c.message, c.timestamp
			FROM core_trailer_refs t
			JOIN core_commits c ON t.repo_url = c.repo_url AND t.hash = c.hash AND t.branch = c.branch
			WHERE t.ref_repo_url = ? AND t.ref_hash = ? AND t.ref_branch = ?
			ORDER BY c.timestamp DESC
		`, refRepoURL, refHash, refBranch)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var refs []TrailerRef
		for rows.Next() {
			var r TrailerRef
			var ts sql.NullString
			if err := rows.Scan(&r.RepoURL, &r.Hash, &r.Branch, &r.TrailerKey,
				&r.AuthorName, &r.AuthorEmail, &r.Message, &ts); err != nil {
				continue
			}
			if ts.Valid {
				if t, err := time.Parse(time.RFC3339, ts.String); err == nil {
					r.Timestamp = t
				}
			}
			refs = append(refs, r)
		}
		return refs, rows.Err()
	})
}
