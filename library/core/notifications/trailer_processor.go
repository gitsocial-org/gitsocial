// trailer_processor.go - Commit processor that extracts git trailer references
package notifications

import (
	"database/sql"
	"log/slog"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// TrailerProcessor returns a commit processor that inserts trailer refs into core_trailer_refs, unnamed to keep notifications below fetch.
func TrailerProcessor() func(commit git.Commit, msg *protocol.Message, repoURL, branch string) {
	return func(commit git.Commit, msg *protocol.Message, repoURL, branch string) {
		if msg != nil {
			return // GitMsg commits carry structured refs instead
		}
		trailers := protocol.ExtractTrailers(commit.Message)
		if len(trailers) == 0 {
			return
		}
		if err := cache.ExecLocked(func(db *sql.DB) error {
			for _, t := range trailers {
				// Only commit refs join core_commits, so other ref types would land as dead rows.
				if protocol.ParseRef(t.Value).Type != protocol.RefTypeCommit {
					continue
				}
				ref := protocol.ResolveRefWithDefaults(t.Value, repoURL, branch)
				if ref.Hash == "" {
					continue
				}
				if _, err := db.Exec(`
					INSERT INTO core_trailer_refs (repo_url, hash, branch, ref_repo_url, ref_hash, ref_branch, trailer_key, trailer_value)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?)
					ON CONFLICT DO NOTHING
				`, repoURL, commit.Hash, branch, ref.RepoURL, ref.Hash, ref.Branch, t.Key, t.Value); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			slog.Warn("insert trailer refs", "error", err, "repo", repoURL, "hash", commit.Hash)
		}
	}
}
