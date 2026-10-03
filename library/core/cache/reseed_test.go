// reseed_test.go - Tests for the schema-version boundary and the reseed it triggers
package cache

import (
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// seedCacheFile writes a cache.db at dir with the given user_version and setup SQL.
// Every caller passes a t.TempDir(), so no test here can reach the real cache.
func seedCacheFile(t *testing.T, dir string, version int, setup string) string {
	t.Helper()
	dbPath := filepath.Join(dir, "cache.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	defer db.Close()
	if setup != "" {
		if _, err := db.Exec(setup); err != nil {
			t.Fatalf("seed setup: %v", err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version = " + strconv.Itoa(version)); err != nil {
		t.Fatalf("seed user_version: %v", err)
	}
	return dbPath
}

// probeUserVersion reads PRAGMA user_version straight off a file.
func probeUserVersion(t *testing.T, dbPath string) int {
	t.Helper()
	v, ok := readUserVersion(dbPath)
	if !ok {
		t.Fatalf("readUserVersion(%s) reported no version", dbPath)
	}
	return v
}

// probeCount runs a scalar COUNT against a cache file that is not currently open.
func probeCount(t *testing.T, dbPath, query string) int {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("probe open: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("probe query %q: %v", query, err)
	}
	return n
}

func TestOpen_olderSchemaIsReseeded(t *testing.T) {
	Reset()
	dir := t.TempDir()
	dbPath := seedCacheFile(t, dir, schemaVersion-1, `
		CREATE TABLE legacy_marker (id INTEGER PRIMARY KEY);
		INSERT INTO legacy_marker (id) VALUES (1);
	`)

	if err := Open(dir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer Reset()

	if got := probeUserVersion(t, dbPath); got != schemaVersion {
		t.Errorf("user_version = %d, want %d", got, schemaVersion)
	}
	var n int
	err := DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'legacy_marker'`).Scan(&n)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if n != 0 {
		t.Error("stale cache was not deleted: legacy_marker survived the reseed")
	}
	if err := DB().QueryRow(`SELECT COUNT(*) FROM core_commits`).Scan(&n); err != nil {
		t.Fatalf("core schema missing after reseed: %v", err)
	}
}

func TestOpen_newerSchemaIsRefusedNotDeleted(t *testing.T) {
	Reset()
	dir := t.TempDir()
	dbPath := seedCacheFile(t, dir, schemaVersion+1, `
		CREATE TABLE future_marker (id INTEGER PRIMARY KEY);
		INSERT INTO future_marker (id) VALUES (1);
	`)

	err := Open(dir)
	if err == nil {
		Reset()
		t.Fatal("Open() accepted a newer schema, want an error")
	}
	defer Reset()
	if !strings.Contains(err.Error(), "newer than the supported") {
		t.Errorf("Open() error = %v, want a schema-version refusal", err)
	}
	if DB() != nil {
		t.Error("DB() should stay nil after a refused open")
	}
	if _, statErr := os.Stat(dbPath); statErr != nil {
		t.Fatalf("the refused cache file was removed: %v", statErr)
	}
	if got := probeUserVersion(t, dbPath); got != schemaVersion+1 {
		t.Errorf("user_version = %d, want %d (file must be untouched)", got, schemaVersion+1)
	}
	if n := probeCount(t, dbPath, `SELECT COUNT(*) FROM future_marker`); n != 1 {
		t.Errorf("future_marker rows = %d, want 1 (file must be untouched)", n)
	}
}

func TestOpen_currentSchemaIsPreserved(t *testing.T) {
	Reset()
	dir := t.TempDir()
	if err := Open(dir); err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	if err := InsertCommits([]Commit{{
		RepoURL: "https://github.com/test/repo", Hash: "abc123456789", Branch: "main",
		Message: "keep me", Timestamp: time.Now(),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	Reset()

	if err := Open(dir); err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer Reset()
	var n int
	if err := DB().QueryRow(`SELECT COUNT(*) FROM core_commits`).Scan(&n); err != nil {
		t.Fatalf("count core_commits: %v", err)
	}
	if n != 1 {
		t.Errorf("core_commits rows = %d, want 1 (a same-version cache must survive)", n)
	}
}

func TestReadUserVersion(t *testing.T) {
	dir := t.TempDir()

	if _, ok := readUserVersion(filepath.Join(dir, "absent.db")); ok {
		t.Error("readUserVersion() reported a version for a missing file")
	}

	garbage := filepath.Join(dir, "garbage.db")
	if err := os.WriteFile(garbage, []byte("this is not a sqlite file"), 0600); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	if _, ok := readUserVersion(garbage); ok {
		t.Error("readUserVersion() reported a version for a non-sqlite file")
	}

	seeded := t.TempDir()
	dbPath := seedCacheFile(t, seeded, 3, "")
	v, ok := readUserVersion(dbPath)
	if !ok || v != 3 {
		t.Errorf("readUserVersion() = (%d, %v), want (3, true)", v, ok)
	}
}

func TestNeedsReseed(t *testing.T) {
	dir := t.TempDir()
	if needsReseed(filepath.Join(dir, "absent.db")) {
		t.Error("needsReseed() = true for a missing file, want false")
	}

	older := seedCacheFile(t, t.TempDir(), schemaVersion-1, "")
	if !needsReseed(older) {
		t.Error("needsReseed() = false for an older cache, want true")
	}

	current := seedCacheFile(t, t.TempDir(), schemaVersion, "")
	if needsReseed(current) {
		t.Error("needsReseed() = true for a current cache, want false")
	}

	newer := seedCacheFile(t, t.TempDir(), schemaVersion+1, "")
	if needsReseed(newer) {
		t.Error("needsReseed() = true for a newer cache, want false (Open refuses it instead)")
	}
}

// TestOpen_reseedKeepsReadMarkers pins invariant 8: a read marker and a follow marker survive the version boundary.
func TestOpen_reseedKeepsReadMarkers(t *testing.T) {
	Reset()
	dir := t.TempDir()
	dbPath := seedCacheFile(t, dir, schemaVersion-1, `
		CREATE TABLE core_notification_reads (repo_url TEXT NOT NULL, hash TEXT NOT NULL, branch TEXT NOT NULL, read_at TEXT, PRIMARY KEY (repo_url, hash, branch));
		INSERT INTO core_notification_reads VALUES ('https://github.com/test/repo', 'abc123456789', 'gitmsg/pm', '2026-01-01T00:00:00Z');
		INSERT INTO core_notification_reads VALUES ('https://github.com/test/follower', 'follow', '', NULL);
	`)

	if err := Open(dir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer Reset()
	if got := probeUserVersion(t, dbPath); got != schemaVersion {
		t.Errorf("user_version = %d, want %d", got, schemaVersion)
	}
	var kept int
	var readAt sql.NullString
	if err := DB().QueryRow(`SELECT COUNT(*), MAX(read_at) FROM core_notification_reads`).Scan(&kept, &readAt); err != nil {
		t.Fatalf("count markers: %v", err)
	}
	if kept != 2 || readAt.String != "2026-01-01T00:00:00Z" {
		t.Errorf("markers after the reseed = %d with read_at %q, want both rows with their time", kept, readAt.String)
	}
	var followKept int
	if err := DB().QueryRow(`SELECT COUNT(*) FROM core_notification_reads WHERE hash = 'follow' AND branch = ''`).Scan(&followKept); err != nil || followKept != 1 {
		t.Errorf("follow markers after the reseed = %d, %v, want 1", followKept, err)
	}
}

// TestOpen_reseedWithoutMarkersTable keeps the reseed of a cache that is older than the markers table.
func TestOpen_reseedWithoutMarkersTable(t *testing.T) {
	Reset()
	dir := t.TempDir()
	seedCacheFile(t, dir, schemaVersion-1, "")
	if err := Open(dir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer Reset()
	var n int
	if err := DB().QueryRow(`SELECT COUNT(*) FROM core_notification_reads`).Scan(&n); err != nil || n != 0 {
		t.Errorf("markers = %d, %v, want an empty table", n, err)
	}
}
