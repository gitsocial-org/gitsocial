// bindings_migration_test.go - Tests for the per-source migration of core_verified_bindings
package identity

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
)

// legacyBindingsSchema is the pre-per-source shape: one row per (key, email),
// with no source or forge_host column.
const legacyBindingsSchema = `
CREATE TABLE core_verified_bindings (
    key_fingerprint TEXT NOT NULL,
    email TEXT NOT NULL,
    verified INTEGER NOT NULL,
    resolved_at TEXT NOT NULL,
    PRIMARY KEY (key_fingerprint, email)
);
`

// execRaw runs statements against a closed cache file, outside the cache package.
func execRaw(t *testing.T, dbPath string, statements ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer db.Close()
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("raw exec %q: %v", stmt, err)
		}
	}
}

// bindingPrimaryKey returns the primary-key column names of core_verified_bindings.
func bindingPrimaryKey(t *testing.T) string {
	t.Helper()
	pk, err := cache.QueryLocked(func(db *sql.DB) (string, error) {
		var cols sql.NullString
		err := db.QueryRow(`SELECT GROUP_CONCAT(name, ',') FROM (
			SELECT name FROM pragma_table_info('core_verified_bindings') WHERE pk > 0 ORDER BY pk)`).Scan(&cols)
		return cols.String, err
	})
	if err != nil {
		t.Fatalf("read primary key: %v", err)
	}
	return pk
}

func TestMigrateBindingsToPerSource(t *testing.T) {
	cache.Reset()
	dir := t.TempDir()
	if err := cache.Open(dir); err != nil {
		t.Fatalf("first cache.Open: %v", err)
	}
	cache.Reset()

	// Roll the file back to the legacy single-row-per-(key,email) shape at the
	// current schema version, so Open migrates instead of reseeding.
	dbPath := filepath.Join(dir, "cache.db")
	execRaw(t, dbPath,
		`DROP TABLE core_verified_bindings`,
		legacyBindingsSchema,
		`INSERT INTO core_verified_bindings (key_fingerprint, email, verified, resolved_at)
			VALUES ('ABCDEF1234567890', 'alice@example.com', 1, '2025-01-01T00:00:00Z')`,
	)

	if err := cache.Open(dir); err != nil {
		t.Fatalf("second cache.Open: %v", err)
	}
	t.Cleanup(func() { cache.Reset() })

	if got := bindingPrimaryKey(t); got != "key_fingerprint,email,source,forge_host" {
		t.Fatalf("primary key = %q, want key_fingerprint,email,source,forge_host", got)
	}

	// The migration drops the legacy rows: bindings are a cache and re-resolve.
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var n int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_verified_bindings`).Scan(&n)
		return n, err
	})
	if err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if count != 0 {
		t.Errorf("binding rows = %d, want 0 after the reshaping migration", count)
	}

	// The migrated table holds one row per source for the same (key, email).
	now := time.Now()
	insertTestBinding(t, &Binding{
		KeyFingerprint: "ABCDEF1234567890", Email: "alice@example.com",
		Source: SourceForgeGPG, ForgeHost: "github.com", ResolvedAt: now, Verified: true,
	})
	insertTestBinding(t, &Binding{
		KeyFingerprint: "ABCDEF1234567890", Email: "alice@example.com",
		Source: SourceDNS, ResolvedAt: now, Verified: true,
	})
	count, err = cache.QueryLocked(func(db *sql.DB) (int, error) {
		var n int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_verified_bindings`).Scan(&n)
		return n, err
	})
	if err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if count != 2 {
		t.Errorf("binding rows = %d, want 2 (one per source)", count)
	}
	if !IsVerified("ABCDEF1234567890", "alice@example.com") {
		t.Error("a forge binding written after the migration should verify")
	}
}

func TestMigrateBindingsToPerSource_alreadyMigratedIsNoop(t *testing.T) {
	cache.Reset()
	dir := t.TempDir()
	if err := cache.Open(dir); err != nil {
		t.Fatalf("first cache.Open: %v", err)
	}
	insertTestBinding(t, &Binding{
		KeyFingerprint: "ABCDEF1234567890", Email: "alice@example.com",
		Source: SourceForgeGPG, ForgeHost: "github.com", ResolvedAt: time.Now(), Verified: true,
	})
	cache.Reset()

	if err := cache.Open(dir); err != nil {
		t.Fatalf("second cache.Open: %v", err)
	}
	t.Cleanup(func() { cache.Reset() })

	if got := bindingPrimaryKey(t); got != "key_fingerprint,email,source,forge_host" {
		t.Fatalf("primary key = %q, want the per-source shape", got)
	}
	if !IsVerified("ABCDEF1234567890", "alice@example.com") {
		t.Error("an already-migrated binding must survive a reopen")
	}
}
