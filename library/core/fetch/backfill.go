// backfill.go - Replay extension processors over cached commits whose items row is missing.
package fetch

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// ExtBackfillSpec links a GitMsg ext name to its extension items table.
type ExtBackfillSpec struct {
	Extension  string
	ItemsTable string
}

// BackfillExtensionItems scans the given repos for cached commits whose
// GitMsg ext header has no matching extension items row, then replays the
// processors on those commits. Best-effort; per-spec errors are logged and
// skipped so a backfill failure never poisons the fetch result. Closes the
// gap where a processor wired into the pipeline after a commit was first
// cached (cdfa2d5 added social.Processors to fork fetch) never sees that
// commit again — FilterUnfetchedCommits skips it forever otherwise.
func BackfillExtensionItems(repoURLs []string, specs []ExtBackfillSpec, processors []CommitProcessor) {
	if len(repoURLs) == 0 || len(specs) == 0 || len(processors) == 0 {
		return
	}
	dispatched := make(map[string]bool)
	for _, spec := range specs {
		orphans, err := findOrphanCommits(repoURLs, spec)
		if err != nil {
			log.Debug("backfill find orphans", "ext", spec.Extension, "error", err)
			continue
		}
		for _, gc := range orphans {
			key := gc.RepoURL + "\x00" + gc.Hash + "\x00" + gc.Branch
			if dispatched[key] {
				continue
			}
			dispatched[key] = true
			msg := protocol.ParseMessage(gc.Message)
			if msg == nil {
				continue
			}
			for _, p := range processors {
				p(gc.Commit, msg, gc.RepoURL, gc.Branch)
			}
		}
		if len(orphans) > 0 {
			log.Debug("backfill replayed", "ext", spec.Extension, "orphans", len(orphans))
		}
	}
}

// orphanCommit pairs a cached commit's git data with its repo/branch coords.
type orphanCommit struct {
	git.Commit
	RepoURL string
	Branch  string
}

// findOrphanCommits returns rows in core_commits whose message carries the
// spec's GitMsg ext marker but whose items row is absent. Scoped to repoURLs
// to bound the scan after a fetch.
func findOrphanCommits(repoURLs []string, spec ExtBackfillSpec) ([]orphanCommit, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]orphanCommit, error) {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(repoURLs)), ",")
		query := fmt.Sprintf(`
			SELECT c.repo_url, c.hash, c.branch,
			       c.author_name, c.author_email, c.timestamp, c.message
			FROM core_commits c
			LEFT JOIN %s e
			  ON c.repo_url = e.repo_url AND c.hash = e.hash AND c.branch = e.branch
			WHERE c.repo_url IN (%s)
			  AND c.message LIKE ?
			  AND e.repo_url IS NULL
		`, spec.ItemsTable, placeholders)
		args := make([]interface{}, 0, len(repoURLs)+1)
		for _, r := range repoURLs {
			args = append(args, r)
		}
		args = append(args, `%ext="`+spec.Extension+`"%`)
		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []orphanCommit
		for rows.Next() {
			var repoURL, hash, branch, message string
			var authorName, authorEmail, ts sql.NullString
			if err := rows.Scan(&repoURL, &hash, &branch, &authorName, &authorEmail, &ts, &message); err != nil {
				continue
			}
			var stamp time.Time
			if ts.Valid {
				if t, err := time.Parse(time.RFC3339, ts.String); err == nil {
					stamp = t
				}
			}
			out = append(out, orphanCommit{
				Commit: git.Commit{
					Hash:      hash,
					Message:   message,
					Author:    authorName.String,
					Email:     authorEmail.String,
					Timestamp: stamp,
					Refname:   "refs/heads/" + branch,
				},
				RepoURL: repoURL,
				Branch:  branch,
			})
		}
		return out, rows.Err()
	})
}
