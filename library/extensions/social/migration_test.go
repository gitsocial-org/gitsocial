// migration_test.go - Tests for the one-time retirement of the legacy social tables
package social

import (
	"database/sql"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
)

// TestRetireLegacySocialTables pins invariants 7 and 8: the legacy tables go
// after one open, the read markers survive in the core table, and the counts
// the migration rebuilds match the live rows.
func TestRetireLegacySocialTables(t *testing.T) {
	dir := t.TempDir()
	cache.Reset()
	if err := cache.Open(dir); err != nil {
		t.Fatalf("cache.Open() error = %v", err)
	}
	t.Cleanup(func() {
		cache.Reset()
		_ = cache.Open(testCacheDir)
	})

	repo := "https://github.com/legacy/origin"
	fork := "https://github.com/legacy/mirror"
	branch := "gitmsg/social"
	comment := func(hash string) string {
		return "Reply " + hash + "\n\n" + `GitMsg: ext="social"; type="comment"; original="` + repo + `#commit:e55500000001@gitmsg/social"; v="0.1.0"`
	}
	fetchSocialCommit(t, repo, branch, "e55500000001", "Root post")
	fetchSocialCommit(t, repo, branch, "e55500000002", comment("e55500000002"))
	fetchSocialCommit(t, repo, branch, "e55500000003", comment("e55500000003"))
	fetchSocialCommit(t, fork, branch, "e55500000003", comment("e55500000003"))
	fetchSocialCommit(t, repo, branch, "e55500000004", comment("e55500000004"))
	fetchSocialCommit(t, repo, branch, "e55500000005",
		"\n\n"+`GitMsg: ext="social"; type="comment"; edits="`+repo+`#commit:e55500000004@gitmsg/social"; retracted="true"; original="`+repo+`#commit:e55500000001@gitmsg/social"; v="0.1.0"`)

	// Put the cache back in its pre-migration shape: the two legacy tables, a
	// read marker only in the legacy table, and a count that drifted.
	if err := cache.ExecLocked(func(db *sql.DB) error {
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS social_notification_reads (
			repo_url TEXT NOT NULL, hash TEXT NOT NULL, branch TEXT NOT NULL, read_at TEXT,
			PRIMARY KEY (repo_url, hash, branch))`); err != nil {
			return err
		}
		if _, err := db.Exec(`INSERT INTO social_notification_reads (repo_url, hash, branch, read_at)
			VALUES (?, ?, ?, ?)`, repo, "e55500000002", branch, "2026-01-01T00:00:00Z"); err != nil {
			return err
		}
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS social_counted_sources (hash TEXT PRIMARY KEY)`); err != nil {
			return err
		}
		_, err := db.Exec(`UPDATE social_interactions SET comments = 99`)
		return err
	}); err != nil {
		t.Fatalf("seed legacy state: %v", err)
	}

	cache.Reset()
	if err := cache.Open(dir); err != nil {
		t.Fatalf("second cache.Open() error = %v", err)
	}

	for _, table := range []string{"social_notification_reads", "social_counted_sources"} {
		var exists bool
		if _, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
			exists = socialTableExists(db, table)
			return 0, nil
		}); err != nil {
			t.Fatalf("read sqlite_master: %v", err)
		}
		if exists {
			t.Errorf("%s survived the migration", table)
		}
	}

	reads, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var n int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_notification_reads WHERE repo_url = ? AND hash = ?`,
			repo, "e55500000002").Scan(&n)
		return n, err
	})
	if err != nil {
		t.Fatalf("count core_notification_reads: %v", err)
	}
	if reads != 1 {
		t.Errorf("core_notification_reads rows = %d, want 1", reads)
	}

	counts, err := RefreshInteractionCounts(repo, "e55500000001", branch)
	if err != nil {
		t.Fatalf("RefreshInteractionCounts() error = %v", err)
	}
	if counts.Comments != 2 {
		t.Errorf("root comments after the migration = %d, want 2", counts.Comments)
	}
}
