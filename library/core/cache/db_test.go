// db_test.go - Tests for SQLite initialization, locking, and helpers
package cache

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	Reset()
	dir := t.TempDir()
	if err := Open(dir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { Reset() })
}

func TestOpen(t *testing.T) {
	Reset()
	dir := t.TempDir()
	if err := Open(dir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer Reset()

	if DB() == nil {
		t.Error("DB() should not be nil after Open")
	}
}

func TestOpen_idempotent(t *testing.T) {
	Reset()
	dir := t.TempDir()
	if err := Open(dir); err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	defer Reset()

	if err := Open(dir); err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
}

// TestOpen_failedInitStaysFailedUntilReset asserts Open never reports success without a database, and that Reset clears the failure.
func TestOpen_failedInitStaysFailedUntilReset(t *testing.T) {
	Reset()
	defer Reset()

	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
		t.Fatalf("write the blocking file: %v", err)
	}
	if err := Open(filepath.Join(blocker, "cache")); err == nil {
		t.Fatal("Open() under a plain file = nil, want an error")
	}

	good := t.TempDir()
	if err := Open(good); err == nil {
		t.Error("Open() after a failed init = nil, want the recorded failure")
	}
	if DB() != nil {
		t.Error("DB() is not nil although no Open has succeeded")
	}

	Reset()
	if err := Open(good); err != nil {
		t.Fatalf("Open() after Reset error = %v", err)
	}
	if DB() == nil {
		t.Error("DB() is nil after the reopen succeeded")
	}
}

func TestReset(t *testing.T) {
	Reset()
	dir := t.TempDir()
	Open(dir)
	Reset()

	if DB() != nil {
		t.Error("DB() should be nil after Reset")
	}
}

func TestExecLocked_notOpen(t *testing.T) {
	Reset()
	err := ExecLocked(func(db *sql.DB) error { return nil })
	if err != ErrNotOpen {
		t.Errorf("ExecLocked() error = %v, want ErrNotOpen", err)
	}
}

func TestQueryLocked_notOpen(t *testing.T) {
	Reset()
	_, err := QueryLocked(func(db *sql.DB) (int, error) { return 0, nil })
	if err != ErrNotOpen {
		t.Errorf("QueryLocked() error = %v, want ErrNotOpen", err)
	}
}

func TestExecLocked(t *testing.T) {
	setupTestDB(t)

	err := ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec("SELECT 1")
		return err
	})
	if err != nil {
		t.Errorf("ExecLocked() error = %v", err)
	}
}

func TestQueryLocked(t *testing.T) {
	setupTestDB(t)

	result, err := QueryLocked(func(db *sql.DB) (int, error) {
		var n int
		err := db.QueryRow("SELECT 1").Scan(&n)
		return n, err
	})
	if err != nil {
		t.Fatalf("QueryLocked() error = %v", err)
	}
	if result != 1 {
		t.Errorf("QueryLocked() = %d, want 1", result)
	}
}

func TestToNullString(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"hello", true},
		{"", false},
	}
	for _, tt := range tests {
		ns := ToNullString(tt.input)
		if ns.Valid != tt.valid {
			t.Errorf("ToNullString(%q).Valid = %v, want %v", tt.input, ns.Valid, tt.valid)
		}
		if tt.valid && ns.String != tt.input {
			t.Errorf("ToNullString(%q).String = %q", tt.input, ns.String)
		}
	}
}

func TestFromNullString(t *testing.T) {
	if got := FromNullString(sql.NullString{String: "hello", Valid: true}); got != "hello" {
		t.Errorf("FromNullString(valid) = %q", got)
	}
	if got := FromNullString(sql.NullString{Valid: false}); got != "" {
		t.Errorf("FromNullString(invalid) = %q", got)
	}
}

func TestToNullInt64(t *testing.T) {
	tests := []struct {
		input int
		valid bool
	}{
		{42, true},
		{0, false},
	}
	for _, tt := range tests {
		ni := ToNullInt64(tt.input)
		if ni.Valid != tt.valid {
			t.Errorf("ToNullInt64(%d).Valid = %v, want %v", tt.input, ni.Valid, tt.valid)
		}
		if tt.valid && ni.Int64 != int64(tt.input) {
			t.Errorf("ToNullInt64(%d).Int64 = %d", tt.input, ni.Int64)
		}
	}
}

func TestRunAnalyze(t *testing.T) {
	setupTestDB(t)
	if err := RunAnalyze(); err != nil {
		t.Errorf("RunAnalyze() error = %v", err)
	}
}

func TestOpen_invalidExtensionSchema(t *testing.T) {
	Reset()
	schemaMu.Lock()
	extensionSchemas["bad_ext"] = "THIS IS NOT VALID SQL"
	schemaMu.Unlock()

	err := Open(t.TempDir())
	if err == nil {
		t.Error("Open() should fail with invalid extension schema")
	}

	Reset()
	schemaMu.Lock()
	delete(extensionSchemas, "bad_ext")
	schemaMu.Unlock()
}

func TestOpen_corruptDbFile(t *testing.T) {
	Reset()
	dir := t.TempDir()
	// Pre-create a corrupt database file
	dbPath := filepath.Join(dir, "cache.db")
	os.WriteFile(dbPath, []byte("this is not a sqlite database!!!"), 0644)

	err := Open(dir)
	if err == nil {
		t.Error("Open() should fail with corrupt database file")
	}
	Reset()
}

func TestOpen_mkdirError(t *testing.T) {
	Reset()
	// Use a path under a file (not a directory) to make MkdirAll fail
	dir := t.TempDir()
	filePath := filepath.Join(dir, "blockfile")
	os.WriteFile(filePath, []byte("x"), 0644)

	err := Open(filepath.Join(filePath, "subdir"))
	if err == nil {
		t.Error("Open() should fail when MkdirAll fails")
	}
	Reset()
}

func TestClose(t *testing.T) {
	Reset()
	dir := t.TempDir()
	if err := Open(dir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
	Reset()
}

func TestClose_notOpen(t *testing.T) {
	Reset()
	if err := Close(); err != nil {
		t.Errorf("Close() when not open should not error, got %v", err)
	}
}

func TestRegisterSchema(t *testing.T) {
	Reset()
	RegisterSchema("test_ext", `CREATE TABLE IF NOT EXISTS test_ext_items (id TEXT PRIMARY KEY)`)

	dir := t.TempDir()
	if err := Open(dir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		Reset()
		schemaMu.Lock()
		delete(extensionSchemas, "test_ext")
		schemaMu.Unlock()
	}()

	// Verify the extension table was created
	_, err := QueryLocked(func(db *sql.DB) (int, error) {
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM test_ext_items").Scan(&count)
		return count, err
	})
	if err != nil {
		t.Errorf("Extension table not created: %v", err)
	}
}

// insertConcurrentCommit writes one row keyed on the writer and iteration.
func insertConcurrentCommit(writer, iteration int) error {
	return ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(
			`INSERT INTO core_commits (repo_url, hash, branch, message, timestamp)
			 VALUES ('https://example.com/repo', ?, 'main', 'concurrent write', '2026-01-01T00:00:00Z')`,
			fmt.Sprintf("w%02di%02d", writer, iteration))
		return err
	})
}

// countConcurrentCommits reads the row count the writers are filling in.
func countConcurrentCommits() (int, error) {
	return QueryLocked(func(db *sql.DB) (int, error) {
		var count int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_commits WHERE repo_url = 'https://example.com/repo'`).Scan(&count)
		return count, err
	})
}

// TestExecLocked_concurrentWritersAndReaders drives one cache from several goroutines at once.
func TestExecLocked_concurrentWritersAndReaders(t *testing.T) {
	setupTestDB(t)
	const writers, readers, perGoroutine = 4, 4, 25
	total := writers * perGoroutine

	errs := make(chan error, (writers+readers)*perGoroutine)
	var wg sync.WaitGroup
	for writer := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := range perGoroutine {
				if err := insertConcurrentCommit(writer, iteration); err != nil {
					errs <- fmt.Errorf("write %d/%d: %w", writer, iteration, err)
				}
			}
		}()
	}
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perGoroutine {
				count, err := countConcurrentCommits()
				if err != nil {
					errs <- fmt.Errorf("read: %w", err)
					continue
				}
				if count < 0 || count > total {
					errs <- fmt.Errorf("read: count = %d, want it within 0 and %d", count, total)
				}
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
	count, err := countConcurrentCommits()
	if err != nil {
		t.Fatalf("final count error = %v", err)
	}
	if count != total {
		t.Errorf("final count = %d, want %d", count, total)
	}
}
