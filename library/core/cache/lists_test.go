// lists_test.go - Tests for list metadata and repository membership
package cache

import (
	"database/sql"
	"testing"
	"time"
)

func TestInsertList(t *testing.T) {
	setupTestDB(t)

	list := CachedList{
		ID:        "list1",
		Name:      "Reading List",
		Version:   "0.1.0",
		Workdir:   "/workspace",
		CreatedAt: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
		Repositories: []ListRepository{
			{ListID: "list1", RepoURL: "https://github.com/user/repo1", Branch: "main", AddedAt: time.Now()},
			{ListID: "list1", RepoURL: "https://github.com/user/repo2", Branch: "main", AddedAt: time.Now()},
		},
	}

	if err := InsertList(list); err != nil {
		t.Fatalf("InsertList() error = %v", err)
	}

	lists, err := GetLists("/workspace")
	if err != nil {
		t.Fatalf("GetLists() error = %v", err)
	}
	if len(lists) != 1 {
		t.Fatalf("len(lists) = %d, want 1", len(lists))
	}
	if lists[0].Name != "Reading List" {
		t.Errorf("Name = %q", lists[0].Name)
	}
	if len(lists[0].Repositories) != 2 {
		t.Errorf("len(Repositories) = %d, want 2", len(lists[0].Repositories))
	}
}

func TestInsertList_replacesExisting(t *testing.T) {
	setupTestDB(t)

	list := CachedList{
		ID: "list1", Name: "Old Name", Version: "0.1.0",
		Workdir: "/workspace", CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Repositories: []ListRepository{
			{ListID: "list1", RepoURL: "https://github.com/user/repo1", Branch: "main", AddedAt: time.Now()},
		},
	}
	InsertList(list)

	list.Name = "New Name"
	list.Repositories = []ListRepository{
		{ListID: "list1", RepoURL: "https://github.com/user/repo2", Branch: "main", AddedAt: time.Now()},
	}
	InsertList(list)

	lists, _ := GetLists("/workspace")
	if len(lists) != 1 {
		t.Fatalf("len(lists) = %d, want 1", len(lists))
	}
	if lists[0].Name != "New Name" {
		t.Errorf("Name = %q, want New Name", lists[0].Name)
	}
	if len(lists[0].Repositories) != 1 {
		t.Errorf("len(Repositories) = %d, want 1 (old repos should be replaced)", len(lists[0].Repositories))
	}
}

func TestGetLists_emptyWorkdir(t *testing.T) {
	setupTestDB(t)

	InsertList(CachedList{
		ID: "list1", Name: "List", Version: "0.1.0",
		Workdir: "/workspace", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	// Empty workdir returns all lists
	lists, err := GetLists("")
	if err != nil {
		t.Fatalf("GetLists() error = %v", err)
	}
	if len(lists) != 1 {
		t.Errorf("len(lists) = %d, want 1", len(lists))
	}
}

func TestGetLists_filteredByWorkdir(t *testing.T) {
	setupTestDB(t)

	InsertList(CachedList{ID: "l1", Name: "List1", Version: "0.1.0", Workdir: "/ws1", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	InsertList(CachedList{ID: "l2", Name: "List2", Version: "0.1.0", Workdir: "/ws2", CreatedAt: time.Now(), UpdatedAt: time.Now()})

	lists, _ := GetLists("/ws1")
	if len(lists) != 1 {
		t.Errorf("len(lists) = %d, want 1", len(lists))
	}
}

func TestAddRepositoryToList(t *testing.T) {
	setupTestDB(t)

	InsertList(CachedList{ID: "list1", Name: "List", Version: "0.1.0", Workdir: "/ws", CreatedAt: time.Now(), UpdatedAt: time.Now()})

	if err := AddRepositoryToList("list1", "https://github.com/user/repo", "main"); err != nil {
		t.Fatalf("AddRepositoryToList() error = %v", err)
	}

	lists, _ := GetLists("/ws")
	if len(lists[0].Repositories) != 1 {
		t.Errorf("len(Repositories) = %d, want 1", len(lists[0].Repositories))
	}
}

func TestRemoveRepositoryFromList(t *testing.T) {
	setupTestDB(t)

	InsertList(CachedList{
		ID: "list1", Name: "List", Version: "0.1.0", Workdir: "/ws",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Repositories: []ListRepository{
			{ListID: "list1", RepoURL: "https://github.com/user/repo", Branch: "main", AddedAt: time.Now()},
		},
	})

	if err := RemoveRepositoryFromList("list1", "https://github.com/user/repo"); err != nil {
		t.Fatalf("RemoveRepositoryFromList() error = %v", err)
	}

	lists, _ := GetLists("/ws")
	if len(lists[0].Repositories) != 0 {
		t.Errorf("len(Repositories) = %d, want 0 after removal", len(lists[0].Repositories))
	}
}

func TestInsertList_execError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_lists"); return err })
	err := InsertList(CachedList{ID: "list1", Name: "L", Version: "0.1.0", Workdir: "/ws", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err == nil {
		t.Error("InsertList() should fail when table is dropped")
	}
}

func TestInsertList_deleteError(t *testing.T) {
	setupTestDB(t)
	// Drop only core_list_repositories so the first INSERT into core_lists succeeds
	// but the DELETE FROM core_list_repositories fails
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_list_repositories"); return err })
	err := InsertList(CachedList{
		ID: "list1", Name: "L", Version: "0.1.0", Workdir: "/ws",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Repositories: []ListRepository{{ListID: "list1", RepoURL: "url", Branch: "main", AddedAt: time.Now()}},
	})
	if err == nil {
		t.Error("InsertList() should fail when core_list_repositories is dropped")
	}
}

func TestInsertList_repoInsertError(t *testing.T) {
	setupTestDB(t)
	// Replace core_list_repositories with a table that has list_id (so DELETE succeeds)
	// but missing repo_url/branch/added_at (so INSERT fails)
	ExecLocked(func(db *sql.DB) error {
		db.Exec("DROP TABLE core_list_repositories")
		_, err := db.Exec("CREATE TABLE core_list_repositories (list_id TEXT)")
		return err
	})
	err := InsertList(CachedList{
		ID: "list1", Name: "L", Version: "0.1.0", Workdir: "/ws",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Repositories: []ListRepository{{ListID: "list1", RepoURL: "url", Branch: "main", AddedAt: time.Now()}},
	})
	if err == nil {
		t.Error("InsertList() should fail when core_list_repositories has wrong schema")
	}
}

func TestGetLists_repoQueryError(t *testing.T) {
	setupTestDB(t)
	// Insert a list first
	InsertList(CachedList{ID: "list1", Name: "L", Version: "0.1.0", Workdir: "/ws",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Repositories: []ListRepository{{ListID: "list1", RepoURL: "url", Branch: "main", AddedAt: time.Now()}},
	})
	// Drop core_list_repositories so the second query fails
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_list_repositories"); return err })
	_, err := GetLists("/ws")
	if err == nil {
		t.Error("GetLists() should fail when core_list_repositories is dropped")
	}
}

func TestGetLists_queryError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_lists"); return err })
	_, err := GetLists("/ws")
	if err == nil {
		t.Error("GetLists() should fail when table is dropped")
	}
}

func TestAddRepositoryToList_execError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_list_repositories"); return err })
	err := AddRepositoryToList("list1", "url", "main")
	if err == nil {
		t.Error("AddRepositoryToList() should fail when table is dropped")
	}
}

func TestRemoveRepositoryFromList_execError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_list_repositories"); return err })
	err := RemoveRepositoryFromList("list1", "url")
	if err == nil {
		t.Error("RemoveRepositoryFromList() should fail when table is dropped")
	}
}

func TestGetListIDs_queryError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_lists"); return err })
	_, err := GetListIDs("/ws")
	if err == nil {
		t.Error("GetListIDs() should fail when table is dropped")
	}
}

func TestInsertList_notOpen(t *testing.T) {
	Reset()
	err := InsertList(CachedList{ID: "list1", Name: "L", Version: "0.1.0", Workdir: "/ws", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err != ErrNotOpen {
		t.Errorf("InsertList() error = %v, want ErrNotOpen", err)
	}
}

func TestGetLists_notOpen(t *testing.T) {
	Reset()
	_, err := GetLists("/ws")
	if err != ErrNotOpen {
		t.Errorf("GetLists() error = %v, want ErrNotOpen", err)
	}
}

func TestGetLists_noListsForWorkdir(t *testing.T) {
	setupTestDB(t)

	// Query a workdir that has no lists — covers len(listIDs)==0 early return
	lists, err := GetLists("/nonexistent-workdir")
	if err != nil {
		t.Fatalf("GetLists() error = %v", err)
	}
	if len(lists) != 0 {
		t.Errorf("len(lists) = %d, want 0", len(lists))
	}
}

func TestAddRepositoryToList_notOpen(t *testing.T) {
	Reset()
	err := AddRepositoryToList("list1", "url", "main")
	if err != ErrNotOpen {
		t.Errorf("AddRepositoryToList() error = %v, want ErrNotOpen", err)
	}
}

func TestRemoveRepositoryFromList_notOpen(t *testing.T) {
	Reset()
	err := RemoveRepositoryFromList("list1", "url")
	if err != ErrNotOpen {
		t.Errorf("RemoveRepositoryFromList() error = %v, want ErrNotOpen", err)
	}
}

func TestGetListIDs_notOpen(t *testing.T) {
	Reset()
	_, err := GetListIDs("/ws")
	if err != ErrNotOpen {
		t.Errorf("GetListIDs() error = %v, want ErrNotOpen", err)
	}
}

func TestGetListIDs(t *testing.T) {
	setupTestDB(t)

	InsertList(CachedList{ID: "list1", Name: "L1", Version: "0.1.0", Workdir: "/ws", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	InsertList(CachedList{ID: "list2", Name: "L2", Version: "0.1.0", Workdir: "/ws", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	InsertList(CachedList{ID: "list3", Name: "L3", Version: "0.1.0", Workdir: "/other", CreatedAt: time.Now(), UpdatedAt: time.Now()})

	ids, err := GetListIDs("/ws")
	if err != nil {
		t.Fatalf("GetListIDs() error = %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("len(ids) = %d, want 2", len(ids))
	}

	allIDs, _ := GetListIDs("")
	if len(allIDs) != 3 {
		t.Errorf("len(allIDs) = %d, want 3", len(allIDs))
	}
}
