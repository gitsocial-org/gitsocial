// items_db_test.go - Tests for release item database operations
package release

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
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

// markStale sets stale_since on one cached row.
func markStale(t *testing.T, repoURL, hash, branch string) {
	t.Helper()
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE core_commits SET stale_since = '2025-10-22T00:00:00Z' WHERE repo_url = ? AND hash = ? AND branch = ?`, repoURL, hash, branch)
		return err
	}); err != nil {
		t.Fatalf("mark stale: %v", err)
	}
}

// insertReleaseTestCommitOn caches one commit under the given branch.
func insertReleaseTestCommitOn(t *testing.T, repoURL, hash, branch string) {
	t.Helper()
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: repoURL, Branch: branch, AuthorName: "Test User", AuthorEmail: "test@test.com",
		Message: "test commit", Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
}

// TestGetReleases_excludesStaleCommit pins invariant 1: the release list and its count leave out a stale row.
func TestGetReleases_excludesStaleCommit(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/stale-release"
	for i, hash := range []string{"5a1e00000001", "5a1e00000002"} {
		insertReleaseTestCommit(t, repoURL, hash)
		if err := InsertReleaseItem(ReleaseItem{RepoURL: repoURL, Hash: hash, Branch: releaseTestBranch, Tag: cache.ToNullString("v1." + string(rune('0'+i)))}); err != nil {
			t.Fatalf("InsertReleaseItem() error = %v", err)
		}
	}
	markStale(t, repoURL, "5a1e00000002", releaseTestBranch)

	res := GetReleases(repoURL, releaseTestBranch, "", 0)
	if !res.Success || len(res.Data) != 1 || res.Data[0].Tag != "v1.0" {
		t.Errorf("GetReleases() = %+v, want the one live release", res)
	}
	if count, err := CountReleases(repoURL, releaseTestBranch); err != nil || count != 1 {
		t.Errorf("CountReleases() = %d, %v, want 1", count, err)
	}
}

// TestGetReleaseItemByHashPrefix_liveFirst pins invariant 2: a prefix lookup opens the live row of a moved release, and still finds a stale-only one.
func TestGetReleaseItemByHashPrefix_liveFirst(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/prefix-release"
	const moved, gone = "11fe00000001", "11fe00000002"
	for _, row := range []struct{ hash, branch string }{{moved, "feature/x"}, {moved, releaseTestBranch}, {gone, "feature/x"}} {
		insertReleaseTestCommitOn(t, repoURL, row.hash, row.branch)
		if err := InsertReleaseItem(ReleaseItem{RepoURL: repoURL, Hash: row.hash, Branch: row.branch, Tag: cache.ToNullString("v" + row.hash)}); err != nil {
			t.Fatalf("InsertReleaseItem() error = %v", err)
		}
	}
	markStale(t, repoURL, moved, "feature/x")
	markStale(t, repoURL, gone, "feature/x")

	if _, err := GetReleaseItemByHashPrefix("11fe000000"); err == nil {
		t.Fatal("GetReleaseItemByHashPrefix() accepted a prefix two releases share")
	}
	item, err := GetReleaseItemByHashPrefix(moved)
	if err != nil || item.Branch != releaseTestBranch {
		t.Errorf("GetReleaseItemByHashPrefix(moved) = %+v, %v, want the live row under %s", item, err, releaseTestBranch)
	}
	item, err = GetReleaseItemByHashPrefix(gone)
	if err != nil || item.Branch != "feature/x" {
		t.Errorf("GetReleaseItemByHashPrefix(gone) = %+v, %v, want the stale row", item, err)
	}
}
