// clear_test.go - Tests for cache clearing operations
package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClearDatabase(t *testing.T) {
	dir := t.TempDir()
	if err := Open(dir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer Reset()

	dbPath := filepath.Join(dir, "cache.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("cache.db should exist after Open")
	}

	if err := ClearDatabase(dir); err != nil {
		t.Fatalf("ClearDatabase() error = %v", err)
	}

	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Error("cache.db should be removed after ClearDatabase")
	}
}

func TestClearDatabase_noFile(t *testing.T) {
	dir := t.TempDir()
	if err := ClearDatabase(dir); err != nil {
		t.Errorf("ClearDatabase() on non-existent file should not error, got %v", err)
	}
}

func TestClearRepositories(t *testing.T) {
	dir := t.TempDir()
	reposDir := filepath.Join(dir, "repositories")
	os.MkdirAll(filepath.Join(reposDir, "somerepo"), 0755)

	if err := ClearRepositories(dir); err != nil {
		t.Fatalf("ClearRepositories() error = %v", err)
	}

	if _, err := os.Stat(reposDir); !os.IsNotExist(err) {
		t.Error("repositories/ should be removed after ClearRepositories")
	}
}

func TestClearRepositories_noDir(t *testing.T) {
	dir := t.TempDir()
	if err := ClearRepositories(dir); err != nil {
		t.Errorf("ClearRepositories() on non-existent dir should not error, got %v", err)
	}
}

func TestClearAll_databaseError(t *testing.T) {
	dir := t.TempDir()
	// Create cache.db as a directory so os.Remove fails with a non-IsNotExist error
	dbPath := filepath.Join(dir, "cache.db")
	os.MkdirAll(filepath.Join(dbPath, "subdir"), 0755)

	err := ClearAll(dir)
	if err == nil {
		t.Error("ClearAll() should fail when cache.db is a directory")
	}
}

func TestClearRepositories_removeError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping as root")
	}
	dir := t.TempDir()
	reposDir := filepath.Join(dir, "repositories")
	innerDir := filepath.Join(reposDir, "inner")
	os.MkdirAll(innerDir, 0755)
	os.WriteFile(filepath.Join(innerDir, "file"), []byte("x"), 0644)
	// Make repositories dir non-readable so RemoveAll can't list contents
	os.Chmod(reposDir, 0000)
	t.Cleanup(func() { os.Chmod(reposDir, 0755) })

	err := ClearRepositories(dir)
	if err == nil {
		t.Error("ClearRepositories() should fail when directory is not readable")
	}
}

func TestClearAll(t *testing.T) {
	dir := t.TempDir()
	Reset()
	if err := Open(dir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer Reset()

	reposDir := filepath.Join(dir, "repositories")
	os.MkdirAll(filepath.Join(reposDir, "somerepo"), 0755)

	if err := ClearAll(dir); err != nil {
		t.Fatalf("ClearAll() error = %v", err)
	}

	dbPath := filepath.Join(dir, "cache.db")
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Error("cache.db should be removed")
	}
	if _, err := os.Stat(reposDir); !os.IsNotExist(err) {
		t.Error("repositories/ should be removed")
	}
}
