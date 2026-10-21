// reseed_test.go - Tests for the schema-version boundary and the reseed it triggers
package cache

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// reseedChildEnv names the cache directory a child process of TestOpen_concurrentReseedOnce opens.
const reseedChildEnv = "GITSOCIAL_TEST_CACHE_CHILD"

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

// startReseedChild runs this test binary on one test with the cache directory in the environment and its output in buf.
func startReseedChild(t *testing.T, test, dir string, buf *bytes.Buffer) *exec.Cmd {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	cmd := exec.Command(self, "-test.run=^"+test+"$")
	cmd.Env = append(os.Environ(), reseedChildEnv+"="+dir)
	cmd.Stdout, cmd.Stderr = buf, buf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the child: %v", err)
	}
	return cmd
}

// waitForFiles polls dir until count files with the prefix exist.
func waitForFiles(t *testing.T, dir, prefix string, count int) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		matches, _ := filepath.Glob(filepath.Join(dir, prefix+"*"))
		if len(matches) >= count {
			return
		}
	}
	t.Fatalf("%d files %s* did not appear in %s", count, prefix, dir)
}

// TestOpen_concurrentReseedOnce holds two children of the test binary at the open lock, then lets both open one older cache; each child writes a row and waits for the row of the other, which one shared file allows.
func TestOpen_concurrentReseedOnce(t *testing.T) {
	if dir := os.Getenv(reseedChildEnv); dir != "" {
		runReseedChild(t, dir)
		return
	}
	dir := t.TempDir()
	dbPath := seedCacheFile(t, dir, schemaVersion-1, "")
	barrier, err := takeLock(filepath.Join(dir, "open.lock"), true, true)
	if err != nil || barrier == nil {
		t.Fatalf("take the barrier lock: %v", err)
	}
	children := make([]*exec.Cmd, 2)
	outputs := make([]*bytes.Buffer, 2)
	for i := range children {
		outputs[i] = &bytes.Buffer{}
		children[i] = startReseedChild(t, "TestOpen_concurrentReseedOnce", dir, outputs[i])
	}
	waitForFiles(t, dir, "ready-", 2)
	closeLock(barrier)
	reseeds := 0
	for i, cmd := range children {
		if err := cmd.Wait(); err != nil {
			t.Errorf("child %d: %v\n%s", i, err, outputs[i])
		}
		reseeds += strings.Count(outputs[i].String(), "deleting and recreating")
	}
	if reseeds != 1 {
		t.Errorf("reseeds = %d, want 1\n%s\n%s", reseeds, outputs[0], outputs[1])
	}
	if got := probeUserVersion(t, dbPath); got != schemaVersion {
		t.Errorf("user_version = %d, want %d", got, schemaVersion)
	}
	if n := probeCount(t, dbPath, `SELECT COUNT(*) FROM core_commits WHERE repo_url = 'https://example.com/reseed'`); n != 2 {
		t.Errorf("rows of both children in the file = %d, want 2", n)
	}
}

// runReseedChild reports itself ready, opens the cache at dir, writes a row keyed on its process and waits until the row of the other child is in the same file.
func runReseedChild(t *testing.T, dir string) {
	Reset()
	defer Reset()
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("ready-%d", os.Getpid())), nil, 0600); err != nil {
		t.Fatalf("write the ready file: %v", err)
	}
	if err := Open(dir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := DB().Exec(`INSERT INTO core_commits (repo_url, hash, branch, message, timestamp)
		VALUES ('https://example.com/reseed', ?, 'main', 'child', '2026-01-01T00:00:00Z')`, fmt.Sprintf("pid%d", os.Getpid())); err != nil {
		t.Fatalf("insert the child row: %v", err)
	}
	var n int
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if err := DB().QueryRow(`SELECT COUNT(*) FROM core_commits WHERE repo_url = 'https://example.com/reseed'`).Scan(&n); err != nil {
			t.Fatalf("count the rows: %v", err)
		}
		if n == 2 {
			return
		}
	}
	t.Fatalf("rows seen by this child = %d, want the row of the other child as well", n)
}

// TestOpen_reseedRefusedWhileOpen lets a child hold the pool lock on an older cache; the parent's Open fails with the error text and removes nothing.
func TestOpen_reseedRefusedWhileOpen(t *testing.T) {
	if dir := os.Getenv(reseedChildEnv); dir != "" {
		holdPoolLock(t, dir)
		return
	}
	Reset()
	defer Reset()
	dir := t.TempDir()
	dbPath := seedCacheFile(t, dir, schemaVersion-1, `
		CREATE TABLE legacy_marker (id INTEGER PRIMARY KEY);
		INSERT INTO legacy_marker (id) VALUES (1);
	`)
	output := &bytes.Buffer{}
	child := startReseedChild(t, "TestOpen_reseedRefusedWhileOpen", dir, output)
	waitForFiles(t, dir, "ready-", 1)
	err := Open(dir)
	if err == nil || !strings.Contains(err.Error(), "open in another gitsocial process") {
		t.Errorf("Open() error = %v, want the refusal of a held cache", err)
	}
	if got := probeUserVersion(t, dbPath); got != schemaVersion-1 {
		t.Errorf("user_version = %d, want %d (file must be untouched)", got, schemaVersion-1)
	}
	if n := probeCount(t, dbPath, `SELECT COUNT(*) FROM legacy_marker`); n != 1 {
		t.Errorf("legacy_marker rows = %d, want 1 (file must be untouched)", n)
	}
	if err := os.WriteFile(filepath.Join(dir, "stop"), nil, 0600); err != nil {
		t.Fatalf("write the stop file: %v", err)
	}
	if err := child.Wait(); err != nil {
		t.Errorf("child: %v\n%s", err, output)
	}
}

// holdPoolLock takes the shared pool lock at dir as an open pool would, reports itself ready and holds the lock until the stop file appears.
func holdPoolLock(t *testing.T, dir string) {
	lock, err := takeLock(filepath.Join(dir, "cache.lock"), false, true)
	if err != nil || lock == nil {
		t.Fatalf("take the pool lock: %v", err)
	}
	defer closeLock(lock)
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("ready-%d", os.Getpid())), nil, 0600); err != nil {
		t.Fatalf("write the ready file: %v", err)
	}
	waitForFiles(t, dir, "stop", 1)
}

// TestOpen_staleLockFile opens a cache beside a cache.lock that a crashed process left behind.
func TestOpen_staleLockFile(t *testing.T) {
	Reset()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cache.lock"), nil, 0600); err != nil {
		t.Fatalf("write the leftover lock file: %v", err)
	}
	if err := Open(dir); err != nil {
		t.Fatalf("Open() beside a leftover cache.lock error = %v", err)
	}
	defer Reset()
	if DB() == nil {
		t.Error("DB() is nil after Open succeeded")
	}
}
