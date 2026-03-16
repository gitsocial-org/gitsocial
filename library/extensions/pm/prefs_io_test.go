// prefs_io_test.go - Tests for user preferences file I/O
package pm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadUserPrefs_roundtrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	repoURL := "https://github.com/test/prefs-repo"
	prefs := UserPrefs{
		CollapsedColumns:   []string{"Done"},
		WIPOverrides:       map[string]int{"In Progress": 5},
		SwimlaneField:      "priority",
		CollapsedSwimlanes: []string{"low"},
	}
	if err := SaveUserPrefs(repoURL, prefs); err != nil {
		t.Fatalf("SaveUserPrefs() error = %v", err)
	}

	loaded := GetUserPrefs(repoURL)
	if len(loaded.CollapsedColumns) != 1 || loaded.CollapsedColumns[0] != "Done" {
		t.Errorf("CollapsedColumns = %v, want [Done]", loaded.CollapsedColumns)
	}
	if loaded.WIPOverrides["In Progress"] != 5 {
		t.Errorf("WIPOverrides = %v, want In Progress=5", loaded.WIPOverrides)
	}
	if loaded.SwimlaneField != "priority" {
		t.Errorf("SwimlaneField = %q, want priority", loaded.SwimlaneField)
	}
	if len(loaded.CollapsedSwimlanes) != 1 || loaded.CollapsedSwimlanes[0] != "low" {
		t.Errorf("CollapsedSwimlanes = %v, want [low]", loaded.CollapsedSwimlanes)
	}
}

func TestGetUserPrefs_nonexistentPath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	prefs := GetUserPrefs("https://github.com/nonexistent/repo")
	if len(prefs.CollapsedColumns) != 0 {
		t.Errorf("expected empty prefs, got %+v", prefs)
	}
}

func TestGetUserPrefs_corruptJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	repoURL := "https://github.com/test/corrupt-repo"
	path := userPrefsPath(repoURL)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	prefs := GetUserPrefs(repoURL)
	if len(prefs.CollapsedColumns) != 0 {
		t.Errorf("corrupt JSON should return empty prefs, got %+v", prefs)
	}
}

func TestUserPrefsPath_deterministic(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	repoURL := "https://github.com/test/deterministic"
	path1 := userPrefsPath(repoURL)
	path2 := userPrefsPath(repoURL)
	if path1 != path2 {
		t.Errorf("userPrefsPath should be deterministic: %q != %q", path1, path2)
	}
	if !filepath.IsAbs(path1) {
		t.Errorf("path should be absolute: %q", path1)
	}

	// Different repos should produce different paths
	path3 := userPrefsPath("https://github.com/other/repo")
	if path1 == path3 {
		t.Error("different repos should have different prefs paths")
	}
}
