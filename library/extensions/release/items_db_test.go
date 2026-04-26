// items_db_test.go - Tests for release item database operations
package release

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
)

const releaseTestBranch = "gitmsg/release"

func insertReleaseTestCommit(t *testing.T, repoURL, hash string) {
	t.Helper()
	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        hash,
		RepoURL:     repoURL,
		Branch:      releaseTestBranch,
		AuthorName:  "Test User",
		AuthorEmail: "test@test.com",
		Message:     "test commit",
		Timestamp:   time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
}

func TestInsertReleaseItem(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	hash := "ins123456789"
	branch := releaseTestBranch
	insertReleaseTestCommit(t, repoURL, hash)

	err := InsertReleaseItem(ReleaseItem{
		RepoURL:    repoURL,
		Hash:       hash,
		Branch:     branch,
		Tag:        cache.ToNullString("v1.0.0"),
		Version:    cache.ToNullString("1.0.0"),
		Prerelease: false,
	})
	if err != nil {
		t.Fatalf("InsertReleaseItem() error = %v", err)
	}
}

func TestInsertReleaseItem_upsert(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	hash := "ups123456789"
	branch := releaseTestBranch
	insertReleaseTestCommit(t, repoURL, hash)

	item := ReleaseItem{
		RepoURL: repoURL,
		Hash:    hash,
		Branch:  branch,
		Tag:     cache.ToNullString("v1.0.0"),
		Version: cache.ToNullString("1.0.0"),
	}
	if err := InsertReleaseItem(item); err != nil {
		t.Fatalf("first InsertReleaseItem() error = %v", err)
	}
	item.Version = cache.ToNullString("1.0.1")
	if err := InsertReleaseItem(item); err != nil {
		t.Fatalf("second InsertReleaseItem() error = %v", err)
	}
}

func TestGetReleaseItem_notFound(t *testing.T) {
	setupTestDB(t)
	_, err := GetReleaseItem("https://github.com/test/repo", "nonexistent12", "gitmsg/release")
	if err == nil {
		t.Error("GetReleaseItem() should return error for non-existent item")
	}
}

func TestGetReleaseItems_filterByRepo(t *testing.T) {
	setupTestDB(t)
	branch := releaseTestBranch
	repo1 := "https://github.com/user/repo1"
	repo2 := "https://github.com/user/repo2"
	insertReleaseTestCommit(t, repo1, "r1hash123456")
	insertReleaseTestCommit(t, repo2, "r2hash123456")
	InsertReleaseItem(ReleaseItem{RepoURL: repo1, Hash: "r1hash123456", Branch: branch, Tag: cache.ToNullString("v1.0"), Version: cache.ToNullString("1.0")})
	InsertReleaseItem(ReleaseItem{RepoURL: repo2, Hash: "r2hash123456", Branch: branch, Tag: cache.ToNullString("v2.0"), Version: cache.ToNullString("2.0")})

	items, err := GetReleaseItems(repo1, "", "", 0)
	if err != nil {
		t.Fatalf("GetReleaseItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item for repo1, got %d", len(items))
	}
}

func TestGetReleaseItems_limit(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	branch := releaseTestBranch
	hashes := []string{"lim1_1234567", "lim2_1234567", "lim3_1234567"}
	for _, hash := range hashes {
		insertReleaseTestCommit(t, repoURL, hash)
		InsertReleaseItem(ReleaseItem{RepoURL: repoURL, Hash: hash, Branch: branch})
	}

	items, err := GetReleaseItems(repoURL, branch, "", 2)
	if err != nil {
		t.Fatalf("GetReleaseItems() error = %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items with limit=2, got %d", len(items))
	}
}

func TestGetReleases_result(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	hash := "relres123456"
	branch := releaseTestBranch
	insertReleaseTestCommit(t, repoURL, hash)
	InsertReleaseItem(ReleaseItem{
		RepoURL: repoURL,
		Hash:    hash,
		Branch:  branch,
		Tag:     cache.ToNullString("v1.0.0"),
		Version: cache.ToNullString("1.0.0"),
	})

	result := GetReleases(repoURL, branch, "", 10)
	if !result.Success {
		t.Fatalf("GetReleases() failed: %s", result.Error.Message)
	}
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 release, got %d", len(result.Data))
	}
	if result.Data[0].Tag != "v1.0.0" {
		t.Errorf("Tag = %q, want v1.0.0", result.Data[0].Tag)
	}
}

func TestGetReleaseItemByRef(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	hash := "aef0e1234567"
	branch := releaseTestBranch
	insertReleaseTestCommit(t, repoURL, hash)
	InsertReleaseItem(ReleaseItem{RepoURL: repoURL, Hash: hash, Branch: branch, Tag: cache.ToNullString("v1.0.0")})

	refStr := "https://github.com/test/repo#commit:" + hash + "@gitmsg/release"
	item, err := GetReleaseItemByRef(refStr, repoURL)
	if err != nil {
		t.Fatalf("GetReleaseItemByRef() error = %v", err)
	}
	if item == nil {
		t.Fatal("GetReleaseItemByRef() returned nil")
	}
	if item.Hash != hash {
		t.Errorf("Hash = %q, want %q", item.Hash, hash)
	}
}

func TestGetReleaseItems_viewError(t *testing.T) {
	setupTestDB(t)
	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec("DROP VIEW IF EXISTS release_items_resolved")
		return nil
	})
	_, err := GetReleaseItems("", "", "", 10)
	if err == nil {
		t.Error("should fail when view is dropped")
	}
}

func TestGetReleaseItems_scanNullError(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/scan-null"
	hash := "a0b1c2d3e4f5"
	branch := releaseTestBranch
	insertReleaseTestCommit(t, repoURL, hash)
	InsertReleaseItem(ReleaseItem{RepoURL: repoURL, Hash: hash, Branch: branch})

	// Replace view with one that returns NULL for is_virtual (scanned into plain int)
	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec("DROP VIEW IF EXISTS release_items_resolved")
		_, err := db.Exec(`CREATE VIEW release_items_resolved AS
			SELECT r.repo_url, r.hash, r.branch,
			       r.effective_author_name AS author_name,
			       r.effective_author_email AS author_email,
			       r.effective_message AS resolved_message,
			       r.effective_timestamp AS timestamp,
			       p.tag, p.version, p.prerelease, p.artifacts, p.artifact_url,
			       p.checksums, p.signed_by,
			       r.edits, NULL as is_virtual, r.is_retracted, r.has_edits,
			       r.is_edit_commit,
			       0 as comments
			FROM core_commits r
			INNER JOIN release_items p ON r.repo_url = p.repo_url AND r.hash = p.hash AND r.branch = p.branch`)
		return err
	})

	_, err := GetReleaseItems(repoURL, branch, "", 10)
	if err == nil {
		t.Error("should fail when view returns NULL for int column")
	}
}

func TestGetReleaseItemByRef_emptyRef(t *testing.T) {
	setupTestDB(t)
	_, err := GetReleaseItemByRef("", "https://github.com/test/repo")
	if err == nil {
		t.Error("GetReleaseItemByRef() should return error for empty ref")
	}
}

func TestGetReleaseItemByRef_workspaceRelative(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	hash := "a0be01234567"
	branch := releaseTestBranch
	insertReleaseTestCommit(t, repoURL, hash)
	InsertReleaseItem(ReleaseItem{RepoURL: repoURL, Hash: hash, Branch: branch})

	refStr := "#commit:" + hash + "@" + branch
	item, err := GetReleaseItemByRef(refStr, repoURL)
	if err != nil {
		t.Fatalf("GetReleaseItemByRef() error = %v", err)
	}
	if item.Hash != hash {
		t.Errorf("Hash = %q, want %q", item.Hash, hash)
	}
}

func TestGetReleaseItemByRef_noBranch(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	hash := "a0be11234560"
	branch := "gitmsg/release"
	insertReleaseTestCommit(t, repoURL, hash)
	InsertReleaseItem(ReleaseItem{RepoURL: repoURL, Hash: hash, Branch: branch})

	refStr := repoURL + "#commit:" + hash
	item, err := GetReleaseItemByRef(refStr, repoURL)
	if err != nil {
		t.Fatalf("GetReleaseItemByRef() error = %v", err)
	}
	if item.Hash != hash {
		t.Errorf("Hash = %q, want %q", item.Hash, hash)
	}
}
