// repo_lists_test.go - Tests for external repository list caching
package cache

import (
	"database/sql"
	"testing"
	"time"
)

const socialSchemaForTest = `
CREATE TABLE IF NOT EXISTS social_items (
    repo_url TEXT NOT NULL, hash TEXT NOT NULL, branch TEXT NOT NULL, type TEXT NOT NULL,
    original_repo_url TEXT, original_hash TEXT, original_branch TEXT,
    reply_to_repo_url TEXT, reply_to_hash TEXT, reply_to_branch TEXT,
    PRIMARY KEY (repo_url, hash, branch)
);
CREATE TABLE IF NOT EXISTS social_interactions (
    repo_url TEXT NOT NULL, hash TEXT NOT NULL, branch TEXT NOT NULL,
    comments INTEGER DEFAULT 0, reposts INTEGER DEFAULT 0, quotes INTEGER DEFAULT 0,
    PRIMARY KEY (repo_url, hash, branch)
);
CREATE VIEW IF NOT EXISTS social_items_resolved AS
SELECT r.*, COALESCE(s.type, 'post') as type,
    s.original_repo_url, s.original_hash, s.original_branch,
    s.reply_to_repo_url, s.reply_to_hash, s.reply_to_branch,
    COALESCE(i.comments, 0) as comments, COALESCE(i.reposts, 0) as reposts, COALESCE(i.quotes, 0) as quotes
FROM core_commits r
LEFT JOIN social_items s ON r.repo_url = s.repo_url AND r.hash = s.hash AND r.branch = s.branch
LEFT JOIN social_interactions i ON r.repo_url = i.repo_url AND r.hash = i.hash AND r.branch = i.branch;
CREATE TABLE IF NOT EXISTS social_notification_reads (
    repo_url TEXT NOT NULL, hash TEXT NOT NULL, branch TEXT NOT NULL, read_at TEXT,
    PRIMARY KEY (repo_url, hash, branch)
);
CREATE TABLE IF NOT EXISTS social_followers (
    repo_url TEXT NOT NULL, workspace_url TEXT NOT NULL, detected_at TEXT, list_id TEXT, commit_hash TEXT,
    PRIMARY KEY (repo_url, workspace_url)
);
CREATE TABLE IF NOT EXISTS social_repo_lists (
    repo_url TEXT NOT NULL, list_id TEXT NOT NULL, name TEXT NOT NULL, version TEXT DEFAULT '0.1.0',
    commit_hash TEXT, cached_at TEXT NOT NULL,
    PRIMARY KEY (repo_url, list_id)
);
CREATE TABLE IF NOT EXISTS social_repo_list_repositories (
    owner_repo_url TEXT NOT NULL, list_id TEXT NOT NULL, repo_url TEXT NOT NULL, branch TEXT DEFAULT 'main',
    PRIMARY KEY (owner_repo_url, list_id, repo_url)
);
`

func setupTestDBWithSocialSchema(t *testing.T) {
	t.Helper()
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
}

func TestInsertExternalRepoList(t *testing.T) {
	setupTestDBWithSocialSchema(t)

	err := InsertExternalRepoList(ExternalRepoList{
		RepoURL:    "https://github.com/user/repo",
		ListID:     "list-1",
		Name:       "following",
		Version:    "0.1.0",
		CommitHash: "abc123",
		CachedAt:   time.Now().UTC(),
		Repositories: []ListRepository{
			{ListID: "list-1", RepoURL: "https://github.com/other/repo", Branch: "main"},
		},
	})
	if err != nil {
		t.Fatalf("InsertExternalRepoList() error = %v", err)
	}
}

func TestInsertExternalRepoList_execError(t *testing.T) {
	setupTestDBWithSocialSchema(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE social_repo_lists"); return err })
	err := InsertExternalRepoList(ExternalRepoList{
		RepoURL: "https://github.com/user/repo", ListID: "list-1", Name: "a", CachedAt: time.Now().UTC(),
	})
	if err == nil {
		t.Error("InsertExternalRepoList() should fail when table is dropped")
	}
}

func TestInsertExternalRepoList_repoInsertError(t *testing.T) {
	setupTestDBWithSocialSchema(t)
	// Replace with a table that has owner_repo_url and list_id (so DELETE succeeds)
	// but missing repo_url/branch (so INSERT fails)
	ExecLocked(func(db *sql.DB) error {
		db.Exec("DROP TABLE social_repo_list_repositories")
		_, err := db.Exec("CREATE TABLE social_repo_list_repositories (owner_repo_url TEXT, list_id TEXT)")
		return err
	})
	err := InsertExternalRepoList(ExternalRepoList{
		RepoURL: "https://github.com/user/repo", ListID: "list-1", Name: "a", CachedAt: time.Now().UTC(),
		Repositories: []ListRepository{{ListID: "list-1", RepoURL: "url", Branch: "main"}},
	})
	if err == nil {
		t.Error("InsertExternalRepoList() should fail when repo table has wrong schema")
	}
}

func TestInsertExternalRepoList_deleteError(t *testing.T) {
	setupTestDBWithSocialSchema(t)
	// Drop only social_repo_list_repositories so first INSERT succeeds but DELETE fails
	ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec("DROP TABLE social_repo_list_repositories")
		return err
	})
	err := InsertExternalRepoList(ExternalRepoList{
		RepoURL: "https://github.com/user/repo", ListID: "list-1", Name: "a", CachedAt: time.Now().UTC(),
		Repositories: []ListRepository{{ListID: "list-1", RepoURL: "url", Branch: "main"}},
	})
	if err == nil {
		t.Error("InsertExternalRepoList() should fail when repo table is dropped")
	}
}

func TestGetExternalRepoLists_queryError(t *testing.T) {
	setupTestDBWithSocialSchema(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE social_repo_lists"); return err })
	_, err := GetExternalRepoLists("https://github.com/user/repo")
	if err == nil {
		t.Error("GetExternalRepoLists() should fail when table is dropped")
	}
}

func TestGetExternalRepoLists_repoQueryError(t *testing.T) {
	setupTestDBWithSocialSchema(t)
	// Insert data first
	InsertExternalRepoList(ExternalRepoList{
		RepoURL: "https://github.com/user/repo", ListID: "list-1", Name: "a", CachedAt: time.Now().UTC(),
		Repositories: []ListRepository{{ListID: "list-1", RepoURL: "url", Branch: "main"}},
	})
	// Drop social_repo_list_repositories so second query fails
	ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec("DROP TABLE social_repo_list_repositories")
		return err
	})
	_, err := GetExternalRepoLists("https://github.com/user/repo")
	if err == nil {
		t.Error("GetExternalRepoLists() should fail when repo table is dropped")
	}
}

func TestInsertExternalRepoList_notOpen(t *testing.T) {
	Reset()
	err := InsertExternalRepoList(ExternalRepoList{
		RepoURL:  "https://github.com/user/repo",
		ListID:   "list-1",
		Name:     "following",
		CachedAt: time.Now().UTC(),
	})
	if err != ErrNotOpen {
		t.Errorf("InsertExternalRepoList() error = %v, want ErrNotOpen", err)
	}
}

func TestGetExternalRepoLists(t *testing.T) {
	setupTestDBWithSocialSchema(t)

	now := time.Now().UTC().Truncate(time.Second)
	InsertExternalRepoList(ExternalRepoList{
		RepoURL:    "https://github.com/user/repo",
		ListID:     "list-1",
		Name:       "following",
		Version:    "0.1.0",
		CommitHash: "abc123",
		CachedAt:   now,
		Repositories: []ListRepository{
			{ListID: "list-1", RepoURL: "https://github.com/other/repo1", Branch: "main"},
			{ListID: "list-1", RepoURL: "https://github.com/other/repo2", Branch: "dev"},
		},
	})

	lists, err := GetExternalRepoLists("https://github.com/user/repo")
	if err != nil {
		t.Fatalf("GetExternalRepoLists() error = %v", err)
	}
	if len(lists) != 1 {
		t.Fatalf("len(lists) = %d, want 1", len(lists))
	}
	if lists[0].Name != "following" {
		t.Errorf("Name = %q, want following", lists[0].Name)
	}
	if lists[0].CommitHash != "abc123" {
		t.Errorf("CommitHash = %q, want abc123", lists[0].CommitHash)
	}
	if len(lists[0].Repositories) != 2 {
		t.Errorf("len(Repositories) = %d, want 2", len(lists[0].Repositories))
	}
}

func TestGetExternalRepoLists_empty(t *testing.T) {
	setupTestDBWithSocialSchema(t)

	lists, err := GetExternalRepoLists("https://github.com/nonexistent/repo")
	if err != nil {
		t.Fatalf("GetExternalRepoLists() error = %v", err)
	}
	if len(lists) != 0 {
		t.Errorf("len(lists) = %d, want 0", len(lists))
	}
}

func TestGetExternalRepoLists_notOpen(t *testing.T) {
	Reset()
	_, err := GetExternalRepoLists("https://github.com/user/repo")
	if err != ErrNotOpen {
		t.Errorf("GetExternalRepoLists() error = %v, want ErrNotOpen", err)
	}
}

func TestGetExternalRepoListCount(t *testing.T) {
	setupTestDBWithSocialSchema(t)

	now := time.Now().UTC()
	InsertExternalRepoList(ExternalRepoList{
		RepoURL: "https://github.com/user/repo", ListID: "list-1", Name: "a", CachedAt: now,
	})
	InsertExternalRepoList(ExternalRepoList{
		RepoURL: "https://github.com/user/repo", ListID: "list-2", Name: "b", CachedAt: now,
	})

	count, err := GetExternalRepoListCount("https://github.com/user/repo")
	if err != nil {
		t.Fatalf("GetExternalRepoListCount() error = %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestGetExternalRepoListCount_notOpen(t *testing.T) {
	Reset()
	_, err := GetExternalRepoListCount("https://github.com/user/repo")
	if err != ErrNotOpen {
		t.Errorf("GetExternalRepoListCount() error = %v, want ErrNotOpen", err)
	}
}

func TestDeleteExternalRepoLists(t *testing.T) {
	setupTestDBWithSocialSchema(t)

	now := time.Now().UTC()
	InsertExternalRepoList(ExternalRepoList{
		RepoURL: "https://github.com/user/repo", ListID: "list-1", Name: "a", CachedAt: now,
	})

	if err := DeleteExternalRepoLists("https://github.com/user/repo"); err != nil {
		t.Fatalf("DeleteExternalRepoLists() error = %v", err)
	}

	count, _ := GetExternalRepoListCount("https://github.com/user/repo")
	if count != 0 {
		t.Errorf("count after delete = %d, want 0", count)
	}
}

func TestDeleteExternalRepoLists_notOpen(t *testing.T) {
	Reset()
	err := DeleteExternalRepoLists("https://github.com/user/repo")
	if err != ErrNotOpen {
		t.Errorf("DeleteExternalRepoLists() error = %v, want ErrNotOpen", err)
	}
}
