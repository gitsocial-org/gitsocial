// db.go - SQLite database initialization, schema migrations, and locking
package cache

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/gitsocial-org/gitsocial/core/log"
)

var ErrNotOpen = errors.New("cache: database not open")

var (
	db      *sql.DB
	initErr error
	opened  bool
	mu      sync.RWMutex
)

// Extension schema registration
var (
	extensionSchemas    = make(map[string]string)
	extensionMigrations []func(*sql.DB)
	schemaMu            sync.Mutex
)

// RegisterSchema registers an extension schema to be executed after core schema.
// Extensions should call this in their init() function.
func RegisterSchema(name, schema string) {
	schemaMu.Lock()
	defer schemaMu.Unlock()
	extensionSchemas[name] = schema
}

// RegisterMigration registers a migration function that runs after all schemas.
// Errors from migrations are silently ignored (best-effort, e.g. ALTER TABLE ADD COLUMN
// when the column already exists on fresh installs).
func RegisterMigration(fn func(*sql.DB)) {
	schemaMu.Lock()
	defer schemaMu.Unlock()
	extensionMigrations = append(extensionMigrations, fn)
}

const coreSchema = `
-- Core: Raw commits (1:1 with git, per repo+branch)
CREATE TABLE IF NOT EXISTS core_commits (
    repo_url TEXT NOT NULL,
    hash TEXT NOT NULL,
    branch TEXT NOT NULL DEFAULT 'main',
    author_name TEXT,
    author_email TEXT,
    message TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    origin_time TEXT,
    edits TEXT,
    labels TEXT,
    fetched_at TEXT,
    is_virtual INTEGER DEFAULT 0,
    stale_since TEXT,
    origin_author_name TEXT,
    origin_author_email TEXT,
    PRIMARY KEY (repo_url, hash, branch)
);
CREATE INDEX IF NOT EXISTS idx_core_commits_timestamp ON core_commits(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_core_commits_repo_timestamp ON core_commits(repo_url, timestamp);
CREATE INDEX IF NOT EXISTS idx_core_commits_repo_branch ON core_commits(repo_url, branch);
CREATE INDEX IF NOT EXISTS idx_core_commits_edits ON core_commits(edits) WHERE edits IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_core_commits_virtual ON core_commits(repo_url, hash, branch) WHERE is_virtual = 1;
CREATE INDEX IF NOT EXISTS idx_core_commits_stale ON core_commits(repo_url, branch) WHERE stale_since IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_core_commits_author ON core_commits(repo_url, author_email, timestamp DESC) WHERE author_email != '';

-- Core: Version tracking (edit relationships)
CREATE TABLE IF NOT EXISTS core_commits_version (
    edit_repo_url TEXT NOT NULL,
    edit_hash TEXT NOT NULL,
    edit_branch TEXT NOT NULL,
    canonical_repo_url TEXT NOT NULL,
    canonical_hash TEXT NOT NULL,
    canonical_branch TEXT NOT NULL,
    is_retracted INTEGER DEFAULT 0,
    PRIMARY KEY (edit_repo_url, edit_hash, edit_branch),
    FOREIGN KEY (edit_repo_url, edit_hash, edit_branch) REFERENCES core_commits(repo_url, hash, branch),
    FOREIGN KEY (canonical_repo_url, canonical_hash, canonical_branch) REFERENCES core_commits(repo_url, hash, branch)
);
CREATE INDEX IF NOT EXISTS idx_core_commits_version_canonical ON core_commits_version(canonical_repo_url, canonical_hash, canonical_branch);

-- Core: Resolved commits (materialized table, maintained at insert time)
-- Replaces the old correlated-subquery VIEW for indexed access.
-- Same columns, now with indexes that extension views can use.
CREATE TABLE IF NOT EXISTS core_commits_resolved (
    repo_url TEXT NOT NULL,
    hash TEXT NOT NULL,
    branch TEXT NOT NULL,
    resolved_message TEXT,
    original_message TEXT,
    edits TEXT,
    is_retracted INTEGER DEFAULT 0,
    has_edits INTEGER DEFAULT 0,
    is_edit_commit INTEGER DEFAULT 0,
    author_name TEXT,
    author_email TEXT,
    timestamp TEXT,
    labels TEXT,
    is_virtual INTEGER DEFAULT 0,
    fetched_at TEXT,
    stale_since TEXT,
    PRIMARY KEY (repo_url, hash, branch)
);
CREATE INDEX IF NOT EXISTS idx_ccr_timestamp ON core_commits_resolved(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_ccr_repo_timestamp ON core_commits_resolved(repo_url, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_ccr_author ON core_commits_resolved(author_email, timestamp DESC) WHERE author_email != '';
CREATE INDEX IF NOT EXISTS idx_ccr_stale ON core_commits_resolved(repo_url, branch) WHERE stale_since IS NOT NULL;

-- Core: Full-text search index
CREATE VIRTUAL TABLE IF NOT EXISTS core_fts USING fts5(
    repo_url UNINDEXED, hash UNINDEXED, branch UNINDEXED,
    content, author
);

-- Core: Repository tracking
CREATE TABLE IF NOT EXISTS core_repositories (
    url TEXT PRIMARY KEY,
    branch TEXT NOT NULL,
    storage_path TEXT NOT NULL,
    last_fetch TEXT
);

-- Core: Fetch range tracking
CREATE TABLE IF NOT EXISTS core_fetch_ranges (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_url TEXT NOT NULL,
    range_start TEXT NOT NULL,
    range_end TEXT NOT NULL,
    status TEXT DEFAULT 'partial',
    fetched_at TEXT,
    commit_count INTEGER DEFAULT 0,
    error_message TEXT
);
CREATE INDEX IF NOT EXISTS idx_core_fetch_ranges_repo ON core_fetch_ranges(repo_url);

-- Core: Lists
CREATE TABLE IF NOT EXISTS core_lists (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    source TEXT,
    version TEXT DEFAULT '0.1.0',
    created_at TEXT,
    updated_at TEXT,
    workdir TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_core_lists_workdir ON core_lists(workdir);

CREATE TABLE IF NOT EXISTS core_list_repositories (
    list_id TEXT REFERENCES core_lists(id) ON DELETE CASCADE,
    repo_url TEXT NOT NULL,
    branch TEXT DEFAULT 'main',
    added_at TEXT,
    PRIMARY KEY (list_id, repo_url)
);
CREATE INDEX IF NOT EXISTS idx_core_list_repos_url ON core_list_repositories(repo_url);

-- Core: Notification read state (shared across extensions)
CREATE TABLE IF NOT EXISTS core_notification_reads (
    repo_url TEXT NOT NULL,
    hash TEXT NOT NULL,
    branch TEXT NOT NULL,
    read_at TEXT,
    PRIMARY KEY (repo_url, hash, branch)
);

-- Core: Mentions extracted from commit messages
CREATE TABLE IF NOT EXISTS core_mentions (
    repo_url TEXT NOT NULL,
    hash TEXT NOT NULL,
    branch TEXT NOT NULL,
    email TEXT NOT NULL,
    PRIMARY KEY (repo_url, hash, branch, email)
);
CREATE INDEX IF NOT EXISTS idx_core_mentions_email ON core_mentions(email);

-- Core: Trailer references extracted from regular commits (GITMSG.md Section 1.7)
CREATE TABLE IF NOT EXISTS core_trailer_refs (
    repo_url TEXT NOT NULL,
    hash TEXT NOT NULL,
    branch TEXT NOT NULL,
    ref_repo_url TEXT NOT NULL,
    ref_hash TEXT NOT NULL,
    ref_branch TEXT NOT NULL,
    trailer_key TEXT NOT NULL,
    trailer_value TEXT NOT NULL,
    PRIMARY KEY (repo_url, hash, branch, ref_repo_url, ref_hash, ref_branch, trailer_key),
    FOREIGN KEY (repo_url, hash, branch) REFERENCES core_commits(repo_url, hash, branch)
);
CREATE INDEX IF NOT EXISTS idx_core_trailer_refs_target ON core_trailer_refs(ref_repo_url, ref_hash, ref_branch);

-- Core: Repository metadata (platform-specific key/value pairs)
CREATE TABLE IF NOT EXISTS core_repository_meta (
    repo_url TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    PRIMARY KEY (repo_url, key)
);

-- Core: Sync tips for workspace sync short-circuiting across restarts
CREATE TABLE IF NOT EXISTS core_sync_tips (
    key TEXT PRIMARY KEY,
    tip TEXT NOT NULL
);
`

// Open initializes the SQLite database connection and creates schema tables.
// If a previous call failed, Open will retry initialization on subsequent calls.
func Open(cacheDir string) error {
	mu.Lock()
	defer mu.Unlock()

	if opened && db != nil {
		return nil
	}
	if opened && initErr != nil {
		return initErr
	}

	start := time.Now()
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		initErr = err
		opened = true
		return err
	}

	dbPath := filepath.Join(cacheDir, "cache.db")
	connStr := dbPath + "?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000&_cache_size=-65536&_mmap_size=268435456&_temp_store=MEMORY"

	var err error
	db, err = sql.Open("sqlite", connStr)
	if err != nil {
		log.Error("cache open failed", "error", err)
		db = nil
		initErr = err
		return err
	}

	db.SetMaxOpenConns(16)

	// Migration: drop the old core_commits_resolved VIEW (if present) so the
	// CREATE TABLE IF NOT EXISTS in coreSchema succeeds. This is a one-time
	// upgrade from the correlated-subquery view to the materialized table.
	_, _ = db.Exec(`DROP VIEW IF EXISTS core_commits_resolved`)

	if _, err := db.Exec(coreSchema); err != nil {
		log.Error("cache core schema init failed", "error", err)
		db.Close()
		db = nil
		initErr = err
		return err
	}
	schemaMu.Lock()
	names := make([]string, 0, len(extensionSchemas))
	for name := range extensionSchemas {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := db.Exec(extensionSchemas[name]); err != nil {
			log.Error("cache extension schema init failed", "extension", name, "error", err)
			schemaMu.Unlock()
			db.Close()
			db = nil
			initErr = err
			return err
		}
	}
	for _, fn := range extensionMigrations {
		fn(db)
	}
	schemaMu.Unlock()

	backfillResolvedTable(db)
	backfillExtensionFields(db)

	opened = true
	initErr = nil
	log.Debug("cache opened", "path", dbPath, "duration_ms", time.Since(start).Milliseconds())
	return nil
}

// Close closes the database connection.
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	if db != nil {
		return db.Close()
	}
	return nil
}

// Reset closes the database and resets singleton state for testing.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	if db != nil {
		db.Close()
		db = nil
	}
	opened = false
	initErr = nil
}

// DB returns the underlying database connection.
func DB() *sql.DB {
	mu.RLock()
	defer mu.RUnlock()
	return db
}

// ExecLocked executes a write operation with exclusive lock.
func ExecLocked(fn func(*sql.DB) error) error {
	mu.Lock()
	defer mu.Unlock()
	if db == nil {
		return ErrNotOpen
	}
	return fn(db)
}

// QueryLocked executes a read operation with shared lock.
func QueryLocked[T any](fn func(*sql.DB) (T, error)) (T, error) {
	mu.RLock()
	defer mu.RUnlock()
	if db == nil {
		var zero T
		return zero, ErrNotOpen
	}
	return fn(db)
}

// ToNullString converts a string to sql.NullString (empty string → NULL).
func ToNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// ToNullInt64 converts an int to sql.NullInt64 (zero → NULL).
func ToNullInt64(n int) sql.NullInt64 {
	if n == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(n), Valid: true}
}

// RunAnalyze runs SQLite ANALYZE for query optimization.
func RunAnalyze() error {
	return ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec("ANALYZE")
		return err
	})
}

// backfillResolvedTable populates core_commits_resolved from core_commits
// if the table is empty (first run after converting from VIEW to TABLE).
func backfillResolvedTable(database *sql.DB) {
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM core_commits_resolved").Scan(&count); err != nil || count > 0 {
		return
	}
	var sourceCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM core_commits").Scan(&sourceCount); err != nil || sourceCount == 0 {
		return
	}
	log.Info("backfilling core_commits_resolved", "commits", sourceCount)

	// Step 1: insert all commits with base resolved fields
	if _, err := database.Exec(`
		INSERT OR IGNORE INTO core_commits_resolved (
			repo_url, hash, branch, resolved_message, original_message, edits,
			is_retracted, has_edits, is_edit_commit,
			author_name, author_email, timestamp,
			labels, is_virtual, fetched_at, stale_since
		)
		SELECT
			c.repo_url, c.hash, c.branch,
			c.message, c.message, c.edits,
			0, 0, 0,
			COALESCE(c.origin_author_name, c.author_name),
			COALESCE(c.origin_author_email, c.author_email),
			COALESCE(c.origin_time, c.timestamp),
			c.labels, c.is_virtual, c.fetched_at, c.stale_since
		FROM core_commits c
	`); err != nil {
		log.Error("backfill resolved insert", "error", err)
		return
	}

	// Step 2: mark edit commits
	if _, err := database.Exec(`
		UPDATE core_commits_resolved SET is_edit_commit = 1
		WHERE EXISTS (
			SELECT 1 FROM core_commits_version v
			WHERE v.edit_repo_url = core_commits_resolved.repo_url
			  AND v.edit_hash = core_commits_resolved.hash
			  AND v.edit_branch = core_commits_resolved.branch
		)
	`); err != nil {
		log.Error("backfill resolved mark edits", "error", err)
	}

	// Step 3: resolve canonical rows (latest edit message, labels, retraction)
	if _, err := database.Exec(`
		UPDATE core_commits_resolved SET
			has_edits = 1,
			resolved_message = COALESCE(
				(SELECT e.message FROM core_commits_version v
				 JOIN core_commits e ON v.edit_repo_url = e.repo_url AND v.edit_hash = e.hash AND v.edit_branch = e.branch
				 WHERE v.canonical_repo_url = core_commits_resolved.repo_url
				   AND v.canonical_hash = core_commits_resolved.hash
				   AND v.canonical_branch = core_commits_resolved.branch
				 ORDER BY e.timestamp DESC LIMIT 1),
				core_commits_resolved.original_message
			),
			is_retracted = COALESCE(
				(SELECT v.is_retracted FROM core_commits_version v
				 JOIN core_commits e ON v.edit_repo_url = e.repo_url AND v.edit_hash = e.hash AND v.edit_branch = e.branch
				 WHERE v.canonical_repo_url = core_commits_resolved.repo_url
				   AND v.canonical_hash = core_commits_resolved.hash
				   AND v.canonical_branch = core_commits_resolved.branch
				 ORDER BY e.timestamp DESC LIMIT 1),
				0
			),
			labels = COALESCE(
				(SELECT e.labels FROM core_commits_version v
				 JOIN core_commits e ON v.edit_repo_url = e.repo_url AND v.edit_hash = e.hash AND v.edit_branch = e.branch
				 WHERE v.canonical_repo_url = core_commits_resolved.repo_url
				   AND v.canonical_hash = core_commits_resolved.hash
				   AND v.canonical_branch = core_commits_resolved.branch
				 ORDER BY e.timestamp DESC LIMIT 1),
				core_commits_resolved.labels
			)
		WHERE EXISTS (
			SELECT 1 FROM core_commits_version v
			WHERE v.canonical_repo_url = core_commits_resolved.repo_url
			  AND v.canonical_hash = core_commits_resolved.hash
			  AND v.canonical_branch = core_commits_resolved.branch
		)
	`); err != nil {
		log.Error("backfill resolved canonicals", "error", err)
	}

	// Step 4: backfill FTS5
	if _, err := database.Exec(`
		INSERT INTO core_fts (repo_url, hash, branch, content, author)
		SELECT repo_url, hash, branch, resolved_message,
			   COALESCE(author_name, '') || ' ' || COALESCE(author_email, '')
		FROM core_commits_resolved
		WHERE NOT is_edit_commit
	`); err != nil {
		log.Error("backfill fts", "error", err)
	}

	log.Info("backfill complete", "commits", sourceCount)
}

// backfillExtensionFields syncs mutable extension fields from edit rows to canonical
// rows for all existing edit chains. This is a one-time migration that runs on upgrade
// from the old ROW_NUMBER-based views to the simplified views.
// Uses a pragma-guarded flag to avoid re-running on subsequent opens.
func backfillExtensionFields(database *sql.DB) {
	// Check if already backfilled using application_id pragma (0 = not done, 42 = done)
	var appID int
	if err := database.QueryRow("PRAGMA application_id").Scan(&appID); err != nil || appID == 42 {
		return
	}
	var versionCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM core_commits_version").Scan(&versionCount); err != nil || versionCount == 0 {
		_, _ = database.Exec("PRAGMA application_id = 42")
		return
	}
	log.Info("backfilling extension fields from edit chains", "versions", versionCount)

	// Process all edits ordered by timestamp ASC so latest edit's values stick
	rows, err := database.Query(`
		SELECT v.edit_repo_url, v.edit_hash, v.edit_branch,
		       v.canonical_repo_url, v.canonical_hash, v.canonical_branch
		FROM core_commits_version v
		JOIN core_commits e ON v.edit_repo_url = e.repo_url AND v.edit_hash = e.hash AND v.edit_branch = e.branch
		ORDER BY e.timestamp ASC
	`)
	if err != nil {
		log.Error("backfill extension fields query", "error", err)
		return
	}
	defer rows.Close()
	synced := 0
	for rows.Next() {
		var eURL, eHash, eBranch, cURL, cHash, cBranch string
		if err := rows.Scan(&eURL, &eHash, &eBranch, &cURL, &cHash, &cBranch); err != nil {
			continue
		}
		syncExtensionFields(database, eURL, eHash, eBranch, cURL, cHash, cBranch)
		synced++
	}
	_, _ = database.Exec("PRAGMA application_id = 42")
	log.Info("backfill extension fields complete", "synced", synced)
}
