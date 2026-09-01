// migrations_test.go - Tests for the schema-version boundary and the registered migrations
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
	if !strings.Contains(err.Error(), "newer than this binary supports") {
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

func TestOpen_runsRegisteredMigrations(t *testing.T) {
	Reset()
	dir := t.TempDir()
	if err := Open(dir); err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	if err := InsertCommits([]Commit{
		{RepoURL: "r", Hash: "aaa111222333", Branch: "tags/v1.0.0", Message: "tagged", Timestamp: time.Now()},
		{RepoURL: "r", Hash: "bbb111222333", Branch: "main", Message: "on a branch", Timestamp: time.Now()},
	}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	Reset()

	// Roll the file back to a pre-migration shape: the resolved_editor_* columns
	// gone, the tag-attributed row still present.
	editorColumns := []string{"resolved_editor_name", "resolved_editor_email", "resolved_edit_repo_url", "resolved_edit_hash", "resolved_edit_branch"}
	dbPath := filepath.Join(dir, "cache.db")
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen raw: %v", err)
	}
	for _, col := range editorColumns {
		if _, err := raw.Exec("ALTER TABLE core_commits DROP COLUMN " + col); err != nil {
			raw.Close()
			t.Fatalf("drop column %s: %v", col, err)
		}
	}
	raw.Close()

	if err := Open(dir); err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer Reset()

	for _, col := range editorColumns {
		var n int
		if err := DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('core_commits') WHERE name = ?`, col).Scan(&n); err != nil {
			t.Fatalf("pragma_table_info: %v", err)
		}
		if n != 1 {
			t.Errorf("column %s was not re-added by migration", col)
		}
	}

	var branches []string
	rows, err := DB().Query(`SELECT branch FROM core_commits ORDER BY branch`)
	if err != nil {
		t.Fatalf("select branches: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			t.Fatalf("scan branch: %v", err)
		}
		branches = append(branches, b)
	}
	if len(branches) != 1 || branches[0] != "main" {
		t.Errorf("branches = %v, want [main]: the tags/ rows migration did not run", branches)
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
