// trailer_processor.go - CommitProcessor that extracts git trailer references
package notifications

import (
	"database/sql"
	"log/slog"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/fetch"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// TrailerProcessor returns a CommitProcessor that extracts git trailer references
// from regular commits (without GitMsg headers) and inserts them into core_trailer_refs.
func TrailerProcessor() fetch.CommitProcessor {
	return func(commit git.Commit, msg *protocol.Message, repoURL, branch string) {
		if msg != nil {
			return // skip GitMsg commits — they use structured refs
		}
		trailers := protocol.ExtractTrailers(commit.Message)
		if len(trailers) == 0 {
			return
		}
		if err := cache.ExecLocked(func(db *sql.DB) error {
			for _, t := range trailers {
				ref := protocol.ResolveRefWithDefaults(t.Value, repoURL, branch)
				if ref.Hash == "" {
					continue // not a GitMsg ref (URL or opaque ID)
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
