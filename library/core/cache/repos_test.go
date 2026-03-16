// repos_test.go - Tests for repository metadata storage
package cache

import (
	"database/sql"
	"testing"
	"time"
)

func TestInsertRepository(t *testing.T) {
	setupTestDB(t)

	repo := Repository{
		URL:         "https://github.com/user/repo",
		Branch:      "main",
		StoragePath: "/tmp/storage/abc123",
	}

	if err := InsertRepository(repo); err != nil {
		t.Fatalf("InsertRepository() error = %v", err)
	}

	got, err := GetRepository("https://github.com/user/repo")
	if err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}
	if got.URL != "https://github.com/user/repo" {
		t.Errorf("URL = %q", got.URL)
	}
	if got.Branch != "main" {
		t.Errorf("Branch = %q", got.Branch)
	}
}

func TestInsertRepository_replace(t *testing.T) {
	setupTestDB(t)

	repo := Repository{URL: "https://github.com/user/repo", Branch: "main", StoragePath: "/old"}
	InsertRepository(repo)

	repo.StoragePath = "/new"
	InsertRepository(repo)

	got, _ := GetRepository("https://github.com/user/repo")
	if got.StoragePath != "/new" {
		t.Errorf("StoragePath = %q, want /new (should be replaced)", got.StoragePath)
	}
}

func TestGetRepository_notFound(t *testing.T) {
	setupTestDB(t)

	_, err := GetRepository("https://github.com/nonexistent/repo")
	if err != sql.ErrNoRows {
		t.Errorf("GetRepository() error = %v, want sql.ErrNoRows", err)
	}
}

func TestGetRepositories(t *testing.T) {
	setupTestDB(t)

	InsertRepository(Repository{URL: "https://github.com/user/repo1", Branch: "main", StoragePath: "/p1"})
	InsertRepository(Repository{URL: "https://github.com/user/repo2", Branch: "main", StoragePath: "/p2"})

	repos, err := GetRepositories()
	if err != nil {
		t.Fatalf("GetRepositories() error = %v", err)
	}
	if len(repos) != 2 {
		t.Errorf("len(repos) = %d, want 2", len(repos))
	}
}

func TestGetRepositories_empty(t *testing.T) {
	setupTestDB(t)

	repos, err := GetRepositories()
	if err != nil {
		t.Fatalf("GetRepositories() error = %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("len(repos) = %d, want 0", len(repos))
	}
}

func TestUpdateRepositoryLastFetch(t *testing.T) {
	setupTestDB(t)

	InsertRepository(Repository{URL: "https://github.com/user/repo", Branch: "main", StoragePath: "/p"})

	if err := UpdateRepositoryLastFetch("https://github.com/user/repo"); err != nil {
		t.Fatalf("UpdateRepositoryLastFetch() error = %v", err)
	}

	got, _ := GetRepository("https://github.com/user/repo")
	if !got.LastFetch.Valid {
		t.Error("LastFetch should be valid after update")
	}
}

func TestGetRepositoryFetchMeta(t *testing.T) {
	setupTestDB(t)

	InsertCommits([]Commit{
		{Hash: "aaa111222333", RepoURL: "https://github.com/user/repo", Branch: "main", Message: "m1", Timestamp: time.Date(2025, 10, 20, 12, 0, 0, 0, time.UTC)},
		{Hash: "bbb111222333", RepoURL: "https://github.com/user/repo", Branch: "main", Message: "m2", Timestamp: time.Date(2025, 10, 22, 12, 0, 0, 0, time.UTC)},
	})

	meta, err := GetRepositoryFetchMeta("https://github.com/user/repo")
	if err != nil {
		t.Fatalf("GetRepositoryFetchMeta() error = %v", err)
	}
	if !meta.HasCommits {
		t.Error("HasCommits should be true")
	}
	if meta.CommitCount != 2 {
		t.Errorf("CommitCount = %d, want 2", meta.CommitCount)
	}
	if meta.OldestCommitTime.IsZero() {
		t.Error("OldestCommitTime should not be zero")
	}
}

func TestGetRepositoryFetchMeta_empty(t *testing.T) {
	setupTestDB(t)

	meta, err := GetRepositoryFetchMeta("https://github.com/nonexistent/repo")
	if err != nil {
		t.Fatalf("GetRepositoryFetchMeta() error = %v", err)
	}
	if meta.HasCommits {
		t.Error("HasCommits should be false for empty repo")
	}
	if meta.CommitCount != 0 {
		t.Errorf("CommitCount = %d, want 0", meta.CommitCount)
	}
}

func TestGetRepositories_queryError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_repositories"); return err })
	_, err := GetRepositories()
	if err == nil {
		t.Error("GetRepositories() should fail when table is dropped")
	}
}

func TestInsertRepository_execError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_repositories"); return err })
	err := InsertRepository(Repository{URL: "url", Branch: "main", StoragePath: "/p"})
	if err == nil {
		t.Error("InsertRepository() should fail when table is dropped")
	}
}

func TestGetRepository_queryError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_repositories"); return err })
	_, err := GetRepository("url")
	if err == nil {
		t.Error("GetRepository() should fail when table is dropped")
	}
}

func TestUpdateRepositoryLastFetch_execError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_repositories"); return err })
	err := UpdateRepositoryLastFetch("url")
	if err == nil {
		t.Error("UpdateRepositoryLastFetch() should fail when table is dropped")
	}
}

func TestGetRepositoryFetchMeta_notOpen(t *testing.T) {
	Reset()
	_, err := GetRepositoryFetchMeta("https://github.com/user/repo")
	if err != ErrNotOpen {
		t.Errorf("GetRepositoryFetchMeta() error = %v, want ErrNotOpen", err)
	}
}

func TestInsertRepository_notOpen(t *testing.T) {
	Reset()
	err := InsertRepository(Repository{URL: "url", Branch: "main", StoragePath: "/p"})
	if err != ErrNotOpen {
		t.Errorf("InsertRepository() error = %v, want ErrNotOpen", err)
	}
}

func TestGetRepository_notOpen(t *testing.T) {
	Reset()
	_, err := GetRepository("https://github.com/user/repo")
	if err != ErrNotOpen {
		t.Errorf("GetRepository() error = %v, want ErrNotOpen", err)
	}
}

func TestGetRepositories_notOpen(t *testing.T) {
	Reset()
	_, err := GetRepositories()
	if err != ErrNotOpen {
		t.Errorf("GetRepositories() error = %v, want ErrNotOpen", err)
	}
}

func TestUpdateRepositoryLastFetch_notOpen(t *testing.T) {
	Reset()
	err := UpdateRepositoryLastFetch("https://github.com/user/repo")
	if err != ErrNotOpen {
		t.Errorf("UpdateRepositoryLastFetch() error = %v, want ErrNotOpen", err)
	}
}

func TestIsRepositoryInAnyList_notOpen(t *testing.T) {
	Reset()
	if IsRepositoryInAnyList("https://github.com/user/repo", "/ws") {
		t.Error("IsRepositoryInAnyList() should return false when db not open")
	}
}

func TestIsRepositoryInAnyList(t *testing.T) {
	setupTestDB(t)

	InsertList(CachedList{
		ID: "list1", Name: "My List", Version: "0.1.0",
		CreatedAt: time.Now(), UpdatedAt: time.Now(), Workdir: "/workspace",
		Repositories: []ListRepository{{ListID: "list1", RepoURL: "https://github.com/user/repo", Branch: "main", AddedAt: time.Now()}},
	})

	if !IsRepositoryInAnyList("https://github.com/user/repo", "/workspace") {
		t.Error("Should be in list")
	}
	if IsRepositoryInAnyList("https://github.com/other/repo", "/workspace") {
		t.Error("Should not be in list")
	}
	if IsRepositoryInAnyList("https://github.com/user/repo", "/other-workspace") {
		t.Error("Should not be in list for different workdir")
	}
}
