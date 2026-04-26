// db.go - SQLite database initialization, schema migrations, and locking
package cache

import (
	"database/sql"
	"errors"
	"fmt"
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

// schemaVersion is bumped whenever the core schema changes in a way that
// requires reseeding the cache. Open() compares user_version to this and
// nukes-and-recreates the file when it lags. Bump on every breaking change.
const schemaVersion = 1

const coreSchema = `
-- Core: Raw commits (1:1 with git, per repo+branch). The is_retracted/has_edits/
-- is_edit_commit flags + resolved_message column carry version-resolution state
-- maintained by applyEditToCanonical. The effective_* generated columns expose
-- the COALESCE'd "what to display" view so callers don't repeat the COALESCE
-- inline and indices can be plain (not expression-based).
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
    signer_key TEXT,
    is_retracted INTEGER NOT NULL DEFAULT 0,
    has_edits INTEGER NOT NULL DEFAULT 0,
    is_edit_commit INTEGER NOT NULL DEFAULT 0,
    resolved_message TEXT,
    -- Generated columns: VIRTUAL (recomputed on access; indexed values stored
    -- in the index). effective_message picks the latest edit's content when
    -- one exists; effective_author_*/timestamp prefer origin_* (set on
    -- imported content) over the raw git author/timestamp.
    effective_message TEXT GENERATED ALWAYS AS
        (COALESCE(resolved_message, message)) VIRTUAL,
    effective_author_name TEXT GENERATED ALWAYS AS
        (COALESCE(origin_author_name, author_name)) VIRTUAL,
    effective_author_email TEXT GENERATED ALWAYS AS
        (COALESCE(origin_author_email, author_email)) VIRTUAL,
    effective_timestamp TEXT GENERATED ALWAYS AS
        (COALESCE(origin_time, timestamp)) VIRTUAL,
    PRIMARY KEY (repo_url, hash, branch)
);
CREATE INDEX IF NOT EXISTS idx_core_commits_timestamp ON core_commits(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_core_commits_signer ON core_commits(signer_key, author_email) WHERE signer_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_core_commits_repo_timestamp ON core_commits(repo_url, timestamp);
CREATE INDEX IF NOT EXISTS idx_core_commits_repo_branch ON core_commits(repo_url, branch);
CREATE INDEX IF NOT EXISTS idx_core_commits_edits ON core_commits(edits) WHERE edits IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_core_commits_virtual ON core_commits(repo_url, hash, branch) WHERE is_virtual = 1;
CREATE INDEX IF NOT EXISTS idx_core_commits_stale ON core_commits(repo_url, branch) WHERE stale_since IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_core_commits_author ON core_commits(repo_url, author_email, timestamp DESC) WHERE author_email != '';
-- Plain indices on the generated effective_* columns. Replaces three
-- expression indices (idx_core_commits_eff_*) keyed on COALESCE expressions —
-- planner picks these up reliably without expression-matching heuristics.
CREATE INDEX IF NOT EXISTS idx_core_commits_eff_timestamp
    ON core_commits(effective_timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_core_commits_repo_eff_timestamp
    ON core_commits(repo_url, effective_timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_core_commits_eff_author
    ON core_commits(repo_url, effective_author_email, effective_timestamp DESC);

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

	// If an existing cache predates the current schemaVersion, nuke it and
	// reseed on next fetch. We treat schema migrations as cheap since the
	// cache is an index, not a source of truth — re-fetching from origin
	// repos is always possible.
	if needsReseed(dbPath) {
		log.Info("cache schema is older than current; deleting and recreating", "path", dbPath)
		if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
			log.Warn("failed to remove stale cache", "path", dbPath, "error", err)
		}
		// Also remove WAL/SHM siblings so the fresh DB starts clean.
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")
		_ = os.Remove(dbPath + "-journal")
	}

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

	if _, err := db.Exec(coreSchema); err != nil {
		log.Error("cache core schema init failed", "error", err)
		db.Close()
		db = nil
		initErr = err
		return err
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		log.Warn("cache user_version set failed", "error", err)
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

	opened = true
	initErr = nil
	log.Debug("cache opened", "path", dbPath, "duration_ms", time.Since(start).Milliseconds())
	return nil
}

// needsReseed returns true when the cache file at dbPath exists but its
// PRAGMA user_version is older than schemaVersion. A non-existent file or a
// file at the current version returns false.
func needsReseed(dbPath string) bool {
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return false
	}
	probe, err := sql.Open("sqlite", dbPath+"?_busy_timeout=5000")
	if err != nil {
		return false
	}
	defer probe.Close()
	var version int
	if err := probe.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return false
	}
	return version < schemaVersion
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
