// db.go - SQLite database initialization, schema migrations, and access.
//
// Concurrency model: the package-level *sql.DB is held in an atomic.Pointer
// so readers and writers can load it without acquiring a lock. SQLite's WAL
// journal already serializes writers and allows concurrent readers at the
// engine level, and database/sql's internal connection pool handles
// goroutine safety — an in-process mutex on every cache call would only
// re-serialize what the driver already serializes correctly. The only
// remaining lock guards Open / Close / Reset (singleton lifecycle).
package cache

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"

	"github.com/gitsocial-org/gitsocial/core/log"
)

var ErrNotOpen = errors.New("cache: database not open")

var (
	dbPtr   atomic.Pointer[sql.DB]
	initErr error
	opened  bool
	mu      sync.Mutex // serializes Open/Close/Reset; never held during DB operations
)

// Extension schema registration
var (
	extensionSchemas    = make(map[string]string)
	extensionMigrations []func(*sql.DB)
	schemaMu            sync.Mutex
)

func init() {
	RegisterMigration(func(db *sql.DB) {
		_, _ = db.Exec(`ALTER TABLE core_commits ADD COLUMN resolved_editor_name TEXT`)
	})
	RegisterMigration(func(db *sql.DB) {
		_, _ = db.Exec(`ALTER TABLE core_commits ADD COLUMN resolved_editor_email TEXT`)
	})
}

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
const schemaVersion = 2

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
    resolved_editor_name TEXT,
    resolved_editor_email TEXT,
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
-- DetectExtension and search hash-prefix filters scan by hash without a
-- repo_url, so the (repo_url, hash, branch) PK can't be used. A plain
-- index on hash makes "hash LIKE 'abc%'" an index range scan.
CREATE INDEX IF NOT EXISTS idx_core_commits_hash ON core_commits(hash);

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

-- Core: protocol-level labels normalized into linking-table form so search
-- can do indexed lookups instead of LIKE-on-comma-string scans.
-- Maintained by RebuildCSVLinkingTable whenever core_commits.labels is
-- written; core_commits.labels remains the source-of-truth for display.
CREATE TABLE IF NOT EXISTS core_labels (
    repo_url TEXT NOT NULL,
    hash TEXT NOT NULL,
    branch TEXT NOT NULL,
    label TEXT NOT NULL,
    PRIMARY KEY (repo_url, hash, branch, label),
    FOREIGN KEY (repo_url, hash, branch) REFERENCES core_commits(repo_url, hash, branch)
);
CREATE INDEX IF NOT EXISTS idx_core_labels_label ON core_labels(label);

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

	if opened && dbPtr.Load() != nil {
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

	// Refuse to open a cache whose schema is newer than this binary
	// supports. Older binaries silently misreading newer caches (missing
	// columns surfacing as NULLs, missing tables erroring out at query
	// time) is a worse failure mode than a clear error here.
	if cacheVer, ok := readUserVersion(dbPath); ok && cacheVer > schemaVersion {
		err := fmt.Errorf("cache schema version %d is newer than this binary supports (%d) — upgrade gitsocial",
			cacheVer, schemaVersion)
		initErr = err
		opened = true
		return err
	}

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

	// Pragmas applied to every pool connection. busy_timeout=30s gives
	// concurrent writers a generous window to wait for the WAL writer slot
	// before the driver returns SQLITE_BUSY. The _pragma=name(value) syntax
	// is modernc.org/sqlite's canonical form — applied per-connection at
	// connect time. _txlock=immediate makes every transaction acquire the
	// writer lock at BEGIN (instead of lazily on first write), which lets
	// busy_timeout work correctly under concurrent transactions: without
	// it, two DEFERRED txns can both BEGIN, both attempt to upgrade to
	// writer at first write, and one fails immediately with BUSY because
	// the upgrade path doesn't honor the busy timeout reliably.
	connStr := dbPath +
		"?_txlock=immediate" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=busy_timeout(30000)" +
		"&_pragma=cache_size(-65536)" +
		"&_pragma=mmap_size(268435456)" +
		"&_pragma=temp_store(MEMORY)"

	d, err := sql.Open("sqlite", connStr)
	if err != nil {
		log.Error("cache open failed", "error", err)
		initErr = err
		opened = true
		return err
	}

	d.SetMaxOpenConns(16)
	d.SetMaxIdleConns(16) // matches MaxOpenConns so reusable connections don't get torn down idle

	if _, err := d.Exec(coreSchema); err != nil {
		log.Error("cache core schema init failed", "error", err)
		_ = d.Close()
		initErr = err
		opened = true
		return err
	}
	if _, err := d.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		log.Warn("cache user_version set failed", "error", err)
	}
	schemaMu.Lock()
	names := make([]string, 0, len(extensionSchemas))
	for name := range extensionSchemas {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := d.Exec(extensionSchemas[name]); err != nil {
			log.Error("cache extension schema init failed", "extension", name, "error", err)
			schemaMu.Unlock()
			_ = d.Close()
			initErr = err
			opened = true
			return err
		}
	}
	for _, fn := range extensionMigrations {
		fn(d)
	}
	schemaMu.Unlock()

	dbPtr.Store(d)
	opened = true
	initErr = nil
	log.Debug("cache opened", "path", dbPath, "duration_ms", time.Since(start).Milliseconds())
	return nil
}

// needsReseed returns true when the cache file at dbPath exists but its
// PRAGMA user_version is older than schemaVersion. A non-existent file or a
// file at the current version returns false.
func needsReseed(dbPath string) bool {
	v, ok := readUserVersion(dbPath)
	return ok && v < schemaVersion
}

// readUserVersion probes a cache file for its PRAGMA user_version. Returns
// (0, false) for missing files, unreadable files, or query errors —
// callers treat that as "no opinion."
func readUserVersion(dbPath string) (int, bool) {
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return 0, false
	}
	probe, err := sql.Open("sqlite", dbPath+"?_busy_timeout=5000")
	if err != nil {
		return 0, false
	}
	defer probe.Close()
	var version int
	if err := probe.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return 0, false
	}
	return version, true
}

// Close runs PRAGMA optimize (so the next Open starts with fresh planner
// stats) and closes the database connection.
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	d := dbPtr.Swap(nil)
	if d == nil {
		return nil
	}
	_, _ = d.Exec("PRAGMA optimize")
	return d.Close()
}

// Reset closes the database and resets singleton state for testing.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	if d := dbPtr.Swap(nil); d != nil {
		_ = d.Close()
	}
	opened = false
	initErr = nil
}

// DB returns the underlying database connection (nil if not open).
func DB() *sql.DB {
	return dbPtr.Load()
}

// ExecLocked runs fn with the current *sql.DB. The name is preserved for
// caller compatibility; the function no longer holds a process-level mutex —
// concurrency is handled by SQLite WAL and the database/sql connection pool.
func ExecLocked(fn func(*sql.DB) error) error {
	db := dbPtr.Load()
	if db == nil {
		return ErrNotOpen
	}
	return fn(db)
}

// QueryLocked runs fn with the current *sql.DB. See ExecLocked for the
// concurrency model.
func QueryLocked[T any](fn func(*sql.DB) (T, error)) (T, error) {
	db := dbPtr.Load()
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
