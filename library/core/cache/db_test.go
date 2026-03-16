// db_test.go - Tests for SQLite initialization, locking, and helpers
package cache

import (
	"database/sql"
	"os"
	"path/filepath"
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
