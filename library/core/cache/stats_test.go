// stats_test.go - Tests for cache statistics and formatting
package cache

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0B"},
		{500, "0B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1048576, "1.0MB"},
		{1073741824, "1.0GB"},
		{1572864, "1.5MB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := FormatBytes(tt.input)
			if got != tt.want {
				t.Errorf("FormatBytes(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatBytesMB(t *testing.T) {
	got := FormatBytesMB(1048576)
	if got != "    1.00MB" {
		t.Errorf("FormatBytesMB(1048576) = %q", got)
	}

	got = FormatBytesMB(0)
	if got != "    0.00MB" {
		t.Errorf("FormatBytesMB(0) = %q", got)
	}
}

func TestGetStats(t *testing.T) {
	setupTestDB(t)

	stats, err := GetStats(t.TempDir())
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}
	if stats == nil {
		t.Fatal("GetStats() returned nil")
	}
}

func TestGetLastFetch_noRepos(t *testing.T) {
	setupTestDB(t)

	ts, err := GetLastFetch()
	if err != nil {
		t.Fatalf("GetLastFetch() error = %v", err)
	}
	if !ts.IsZero() {
		t.Errorf("GetLastFetch() should return zero time when no repos, got %v", ts)
	}
}

func TestGetLastFetch_withData(t *testing.T) {
	setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Second)
	InsertRepository(Repository{URL: "https://github.com/user/repo", Branch: "main", StoragePath: "/tmp/test"})
	UpdateRepositoryLastFetch("https://github.com/user/repo")

	ts, err := GetLastFetch()
	if err != nil {
		t.Fatalf("GetLastFetch() error = %v", err)
	}
	// Should be within a few seconds of now
	diff := ts.Sub(now)
	if diff < -5*time.Second || diff > 5*time.Second {
		t.Errorf("GetLastFetch() = %v, expected close to %v", ts, now)
	}
}

func TestGetExtensionStats_dbNotOpen(t *testing.T) {
	Reset()
	stats, err := GetExtensionStats("social")
	if err != nil {
		t.Fatalf("GetExtensionStats() error = %v", err)
	}
	if stats.Extension != "social" {
		t.Errorf("Extension = %q, want social", stats.Extension)
	}
	if stats.Items != 0 {
		t.Errorf("Items = %d, want 0", stats.Items)
	}
}

func TestGetExtensionStats_social(t *testing.T) {
	schemaMu.Lock()
	extensionSchemas["social"] = socialSchemaForTest
	schemaMu.Unlock()
	Reset()
	dir := t.TempDir()
	if err := Open(dir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		Reset()
		schemaMu.Lock()
		delete(extensionSchemas, "social")
		schemaMu.Unlock()
	})

	repoURL := "https://github.com/user/repo"
	InsertCommits([]Commit{
		{Hash: "post_hash_01", RepoURL: repoURL, Branch: "main", Message: "post", Timestamp: time.Now().UTC()},
		{Hash: "comment_h_01", RepoURL: repoURL, Branch: "main", Message: "comment", Timestamp: time.Now().UTC()},
	})
	ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO social_items (repo_url, hash, branch, type) VALUES (?, ?, 'main', 'post')`, repoURL, "post_hash_01")
		db.Exec(`INSERT INTO social_items (repo_url, hash, branch, type) VALUES (?, ?, 'main', 'comment')`, repoURL, "comment_h_01")
		return nil
	})

	stats, err := GetExtensionStats("social")
	if err != nil {
		t.Fatalf("GetExtensionStats() error = %v", err)
	}
	if stats.Items != 2 {
		t.Errorf("Items = %d, want 2", stats.Items)
	}
	if stats.ByType["post"] != 1 {
		t.Errorf("ByType[post] = %d, want 1", stats.ByType["post"])
	}
	if stats.ByType["comment"] != 1 {
		t.Errorf("ByType[comment] = %d, want 1", stats.ByType["comment"])
	}
	if stats.BySource[repoURL] != 2 {
		t.Errorf("BySource[%s] = %d, want 2", repoURL, stats.BySource[repoURL])
	}
}

func TestGetStats_dbNotOpen(t *testing.T) {
	Reset()
	stats, err := GetStats(t.TempDir())
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}
	if stats.Items != 0 {
		t.Errorf("Items = %d, want 0 when db not open", stats.Items)
	}
}

func TestGetLastFetch_notOpen(t *testing.T) {
	Reset()
	ts, err := GetLastFetch()
	if err != nil {
		t.Fatalf("GetLastFetch() error = %v", err)
	}
	if !ts.IsZero() {
		t.Errorf("should return zero time when db not open, got %v", ts)
	}
}

func TestCountCommitsInBareRepo(t *testing.T) {
	dir := t.TempDir()

	// Create mock git objects directory structure
	objectsDir := filepath.Join(dir, "objects")
	os.MkdirAll(objectsDir, 0755)

	// Create a 2-char subdir with some files (loose objects)
	subdir := filepath.Join(objectsDir, "ab")
	os.MkdirAll(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "cdef1234567890"), []byte("blob"), 0644)
	os.WriteFile(filepath.Join(subdir, "1234567890abcd"), []byte("blob"), 0644)

	// Create another subdir
	subdir2 := filepath.Join(objectsDir, "cd")
	os.MkdirAll(subdir2, 0755)
	os.WriteFile(filepath.Join(subdir2, "ef12345678"), []byte("blob"), 0644)

	// Skip non-2-char dirs like "info" and "pack"
	os.MkdirAll(filepath.Join(objectsDir, "info"), 0755)

	// Create pack directory with an idx file
	packDir := filepath.Join(objectsDir, "pack")
	os.MkdirAll(packDir, 0755)
	// Create an idx file of 240 bytes → 240/24 = 10 estimated objects
	os.WriteFile(filepath.Join(packDir, "pack-abc123.idx"), make([]byte, 240), 0644)
	// Non-idx file should be ignored
	os.WriteFile(filepath.Join(packDir, "pack-abc123.pack"), make([]byte, 1000), 0644)

	count := countCommitsInBareRepo(dir)
	// 3 loose objects + 10 from pack idx = 13
	if count != 13 {
		t.Errorf("countCommitsInBareRepo() = %d, want 13", count)
	}
}

func TestCountCommitsInBareRepo_noObjectsDir(t *testing.T) {
	count := countCommitsInBareRepo("/nonexistent/path")
	if count != 0 {
		t.Errorf("countCommitsInBareRepo() = %d, want 0 for nonexistent dir", count)
	}
}

func TestGetTopRepositoriesBySize_invalidLastFetch(t *testing.T) {
	setupTestDB(t)

	dir := t.TempDir()
	reposDir := filepath.Join(dir, "repositories")
	repoDir := filepath.Join(reposDir, "repo1")
	os.MkdirAll(repoDir, 0755)
	os.WriteFile(filepath.Join(repoDir, "file"), []byte("data"), 0644)

	// Insert repository with storage_path matching repoDir, then set invalid last_fetch
	InsertRepository(Repository{URL: "https://github.com/user/repo1", Branch: "main", StoragePath: repoDir})
	ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec("UPDATE core_repositories SET last_fetch = 'not-a-valid-date' WHERE url = ?", "https://github.com/user/repo1")
		return err
	})

	repos := getTopRepositoriesBySize(dir, 0)
	if len(repos) != 1 {
		t.Fatalf("len(repos) = %d, want 1", len(repos))
	}
	// LastFetch should be zero since time.Parse fails on invalid date
	if !repos[0].LastFetch.IsZero() {
		t.Error("LastFetch should be zero for invalid date format")
	}
}

func TestGetTopRepositoriesBySize_queryError(t *testing.T) {
	setupTestDB(t)

	dir := t.TempDir()
	reposDir := filepath.Join(dir, "repositories")
	repoDir := filepath.Join(reposDir, "repo1")
	os.MkdirAll(repoDir, 0755)
	os.WriteFile(filepath.Join(repoDir, "file"), []byte("data"), 0644)

	// Drop core_repositories to trigger query error in lastFetchMap building
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_repositories"); return err })

	repos := getTopRepositoriesBySize(dir, 0)
	if len(repos) != 1 {
		t.Fatalf("len(repos) = %d, want 1", len(repos))
	}
	// LastFetch should be zero since query failed
	if !repos[0].LastFetch.IsZero() {
		t.Error("LastFetch should be zero when db query fails")
	}
}

func TestGetTopRepositoriesBySize_withData(t *testing.T) {
	setupTestDB(t)

	dir := t.TempDir()
	reposDir := filepath.Join(dir, "repositories")

	// Create two repo directories with different sizes
	repo1 := filepath.Join(reposDir, "repo1")
	os.MkdirAll(repo1, 0755)
	os.WriteFile(filepath.Join(repo1, "file1"), make([]byte, 1000), 0644)

	repo2 := filepath.Join(reposDir, "repo2")
	os.MkdirAll(repo2, 0755)
	os.WriteFile(filepath.Join(repo2, "file2"), make([]byte, 500), 0644)

	// Insert a repository record with storage_path and last_fetch
	InsertRepository(Repository{URL: "https://github.com/user/repo1", Branch: "main", StoragePath: repo1})
	UpdateRepositoryLastFetch("https://github.com/user/repo1")

	repos := getTopRepositoriesBySize(dir, 0)
	if len(repos) != 2 {
		t.Fatalf("len(repos) = %d, want 2", len(repos))
	}
	// First should be larger
	if repos[0].Size < repos[1].Size {
		t.Error("repos should be sorted by size descending")
	}
}

func TestGetTopRepositoriesBySize_withLimit(t *testing.T) {
	setupTestDB(t)

	dir := t.TempDir()
	reposDir := filepath.Join(dir, "repositories")

	for _, name := range []string{"repo1", "repo2", "repo3"} {
		d := filepath.Join(reposDir, name)
		os.MkdirAll(d, 0755)
		os.WriteFile(filepath.Join(d, "f"), []byte("data"), 0644)
	}

	repos := getTopRepositoriesBySize(dir, 2)
	if len(repos) != 2 {
		t.Errorf("len(repos) = %d, want 2 (limited)", len(repos))
	}
}

func TestCountCommitsInBareRepo_unreadableSubdir(t *testing.T) {
	dir := t.TempDir()
	objectsDir := filepath.Join(dir, "objects")
	os.MkdirAll(objectsDir, 0755)

	// Create a 2-char subdir that's not readable
	subdir := filepath.Join(objectsDir, "ab")
	os.MkdirAll(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "file1"), []byte("x"), 0644)
	os.Chmod(subdir, 0000)
	t.Cleanup(func() { os.Chmod(subdir, 0755) })

	// Should not panic; the unreadable subdir is skipped
	count := countCommitsInBareRepo(dir)
	if count != 0 {
		t.Errorf("countCommitsInBareRepo() = %d, want 0 (unreadable subdir skipped)", count)
	}
}

func TestCountCommitsInBareRepo_noPacks(t *testing.T) {
	dir := t.TempDir()
	objectsDir := filepath.Join(dir, "objects")
	os.MkdirAll(objectsDir, 0755)

	// Only loose objects, no pack dir
	subdir := filepath.Join(objectsDir, "cd")
	os.MkdirAll(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "file1"), []byte("x"), 0644)

	count := countCommitsInBareRepo(dir)
	if count != 1 {
		t.Errorf("countCommitsInBareRepo() = %d, want 1", count)
	}
}

func TestGetRepositoriesSize_withNonDirEntries(t *testing.T) {
	dir := t.TempDir()
	reposDir := filepath.Join(dir, "repositories")
	os.MkdirAll(reposDir, 0755)

	// Place a regular file alongside directories — should be skipped
	os.WriteFile(filepath.Join(reposDir, "not-a-dir"), []byte("x"), 0644)
	repoDir := filepath.Join(reposDir, "real-repo")
	os.MkdirAll(repoDir, 0755)
	os.WriteFile(filepath.Join(repoDir, "file"), []byte("data"), 0644)

	size, count := getRepositoriesSize(dir)
	if count != 1 {
		t.Errorf("count = %d, want 1 (only directories)", count)
	}
	if size == 0 {
		t.Error("size should be > 0")
	}
}

func TestGetExtensionStats_nonSocial(t *testing.T) {
	setupTestDB(t)

	stats, err := GetExtensionStats("pm")
	if err != nil {
		t.Fatalf("GetExtensionStats() error = %v", err)
	}
	if stats.Extension != "pm" {
		t.Errorf("Extension = %q, want pm", stats.Extension)
	}
	if stats.Items != 0 {
		t.Errorf("Items = %d, want 0", stats.Items)
	}
}

func TestGetStats_withDbAndRepos(t *testing.T) {
	setupTestDB(t)

	dir := t.TempDir()
	// Create db file to get DbSizeBytes
	dbPath := filepath.Join(dir, "cache.db")
	os.WriteFile(dbPath, make([]byte, 1024), 0644)

	stats, err := GetStats(dir)
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}
	if stats.DbSizeBytes == 0 {
		t.Error("DbSizeBytes should be > 0 with db file")
	}
	if stats.MemoryBytes == 0 {
		t.Error("MemoryBytes should be > 0")
	}
}

func TestGetStats_withRepos(t *testing.T) {
	setupTestDB(t)

	dir := t.TempDir()
	reposDir := filepath.Join(dir, "repositories")
	repoDir := filepath.Join(reposDir, "testrepo")
	os.MkdirAll(repoDir, 0755)
	os.WriteFile(filepath.Join(repoDir, "testfile"), []byte("hello"), 0644)

	stats, err := GetStats(dir)
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}
	if stats.Repositories != 1 {
		t.Errorf("Repositories = %d, want 1", stats.Repositories)
	}
	if stats.RepoSizeBytes == 0 {
		t.Error("RepoSizeBytes should be > 0")
	}
}
