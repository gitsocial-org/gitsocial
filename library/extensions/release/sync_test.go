// sync_test.go - Tests for release commit processing
package release

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

var repoTemplate string
var testCacheDir string

func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "release-test-template-*")
	git.Init(dir, "main")
	git.ExecGit(dir, []string{"config", "user.email", "test@test.com"})
	git.ExecGit(dir, []string{"config", "user.name", "Test User"})
	git.CreateCommit(dir, git.CommitOptions{Message: "Initial commit", AllowEmpty: true})
	git.ExecGit(dir, []string{"remote", "add", "origin", "https://github.com/test/repo.git"})
	repoTemplate = dir

	cacheDir, _ := os.MkdirTemp("", "release-test-cache-*")
	cache.Open(cacheDir)
	testCacheDir = cacheDir

	code := m.Run()
	cache.Reset()
	os.RemoveAll(cacheDir)
	os.RemoveAll(dir)
	os.Exit(code)
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				return err
			}
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		srcFile, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		dstFile, err := os.Create(dstPath)
		if err != nil {
			srcFile.Close()
			return err
		}
		_, err = io.Copy(dstFile, srcFile)
		srcFile.Close()
		dstFile.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func setupTestDB(t *testing.T) {
	t.Helper()
	cache.Reset()
	dir := t.TempDir()
	if err := cache.Open(dir); err != nil {
		t.Fatalf("cache.Open() error = %v", err)
	}
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})
}

const relSyncTestRepoURL = "https://github.com/test/repo"
const relSyncTestBranch = "gitmsg/release"

func insertTestCommit(t *testing.T, hash, message string) {
	t.Helper()
	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        hash,
		RepoURL:     relSyncTestRepoURL,
		Branch:      relSyncTestBranch,
		AuthorName:  "Test User",
		AuthorEmail: "test@test.com",
		Message:     message,
		Timestamp:   time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
}

func TestProcessReleaseCommit_nilMessage(t *testing.T) {
	setupTestDB(t)
	gc := git.Commit{Hash: "abc123456789"}
	processReleaseCommit(gc, nil, "https://github.com/test/repo", "gitmsg/release")
	count := countReleaseItems(t)
	if count != 0 {
		t.Errorf("expected 0 release_items, got %d", count)
	}
}

func TestProcessReleaseCommit_wrongExtension(t *testing.T) {
	setupTestDB(t)
	msg := &protocol.Message{
		Content: "A social post",
		Header:  protocol.Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "post"}},
	}
	gc := git.Commit{Hash: "abc123456789"}
	processReleaseCommit(gc, msg, "https://github.com/test/repo", "gitmsg/release")
	count := countReleaseItems(t)
	if count != 0 {
		t.Errorf("expected 0 release_items for wrong ext, got %d", count)
	}
}

func TestProcessReleaseCommit_basicRelease(t *testing.T) {
	setupTestDB(t)
	repoURL := relSyncTestRepoURL
	hash := "rel123456789"
	branch := relSyncTestBranch
	content := "Release v1.0.0\n\nNew features and bug fixes\n\n" + `--- GitMsg: ext="release"; tag="v1.0.0"; version="1.0.0"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processReleaseCommit(gc, msg, repoURL, branch)

	item := queryReleaseItem(t, repoURL, hash, branch)
	if !item.Tag.Valid || item.Tag.String != "v1.0.0" {
		t.Errorf("Tag = %v, want v1.0.0", item.Tag)
	}
	if !item.Version.Valid || item.Version.String != "1.0.0" {
		t.Errorf("Version = %v, want 1.0.0", item.Version)
	}
}

func TestProcessReleaseCommit_prerelease(t *testing.T) {
	setupTestDB(t)
	repoURL := relSyncTestRepoURL
	hash := "pre123456789"
	branch := relSyncTestBranch
	content := "Beta release\n\n" + `--- GitMsg: ext="release"; tag="v2.0.0-beta.1"; version="2.0.0-beta.1"; prerelease="true"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processReleaseCommit(gc, msg, repoURL, branch)

	item := queryReleaseItem(t, repoURL, hash, branch)
	if !item.Prerelease {
		t.Error("Prerelease should be true")
	}
}

func TestProcessReleaseCommit_withArtifacts(t *testing.T) {
	setupTestDB(t)
	repoURL := relSyncTestRepoURL
	hash := "art123456789"
	branch := relSyncTestBranch
	content := "Release with artifacts\n\n" + `--- GitMsg: ext="release"; tag="v1.0.0"; version="1.0.0"; artifacts="app-linux,app-darwin,app-windows"; artifact-url="https://releases.example.com/v1.0.0"; checksums="sha256:abc123"; signed-by="release@example.com"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processReleaseCommit(gc, msg, repoURL, branch)

	item := queryReleaseItem(t, repoURL, hash, branch)
	if !item.Artifacts.Valid || item.Artifacts.String != "app-linux,app-darwin,app-windows" {
		t.Errorf("Artifacts = %v", item.Artifacts)
	}
	if !item.ArtifactURL.Valid || item.ArtifactURL.String != "https://releases.example.com/v1.0.0" {
		t.Errorf("ArtifactURL = %v", item.ArtifactURL)
	}
	if !item.Checksums.Valid || item.Checksums.String != "sha256:abc123" {
		t.Errorf("Checksums = %v", item.Checksums)
	}
	if !item.SignedBy.Valid || item.SignedBy.String != "release@example.com" {
		t.Errorf("SignedBy = %v", item.SignedBy)
	}
}

func TestProcessReleaseCommit_withEditsRef(t *testing.T) {
	setupTestDB(t)
	repoURL := relSyncTestRepoURL
	branch := relSyncTestBranch
	canonicalHash := "ca001e234567"
	editHash := "edithash12345"

	canonContent := "Original release\n\n" + `--- GitMsg: ext="release"; tag="v1.0.0"; version="1.0.0"; v="0.1.0" ---`
	insertTestCommit(t, canonicalHash, canonContent)
	msg := protocol.ParseMessage(canonContent)
	gc := git.Commit{Hash: canonicalHash, Timestamp: time.Now()}
	processReleaseCommit(gc, msg, repoURL, branch)

	editContent := "Updated release\n\n" + `--- GitMsg: ext="release"; tag="v1.0.0"; version="1.0.0"; edits="#commit:ca001e234567@gitmsg/release"; v="0.1.0" ---`
	insertTestCommit(t, editHash, editContent)
	editMsg := protocol.ParseMessage(editContent)
	editGc := git.Commit{Hash: editHash, Timestamp: time.Now()}
	processReleaseCommit(editGc, editMsg, repoURL, branch)

	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_commits_version WHERE edit_hash = ? AND canonical_hash = ?`,
			editHash, canonicalHash).Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("query error = %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 version row, got %d", count)
	}
}

func TestSyncWorkspaceToCache(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	git.CreateCommitOnBranch(dir, "gitmsg/release", "Release v1.0.0\n\n"+`--- GitMsg: ext="release"; tag="v1.0.0"; version="1.0.0"; v="0.1.0" ---`)
	git.CreateCommitOnBranch(dir, "gitmsg/release", "Release v2.0.0\n\n"+`--- GitMsg: ext="release"; tag="v2.0.0"; version="2.0.0"; v="0.1.0" ---`)

	if err := SyncWorkspaceToCache(dir); err != nil {
		t.Fatalf("SyncWorkspaceToCache() error = %v", err)
	}

	res := GetReleases("https://github.com/test/repo", "gitmsg/release", "", 10)
	if !res.Success {
		t.Fatalf("GetReleases() failed: %s", res.Error.Message)
	}
	if len(res.Data) < 2 {
		t.Errorf("expected at least 2 releases, got %d", len(res.Data))
	}
}

func TestProcessReleaseCommit_dbError(t *testing.T) {
	cache.Reset()
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})

	content := "Release v1.0\n\n" + `--- GitMsg: ext="release"; tag="v1.0.0"; v="0.1.0" ---`
	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: "abc123456789", Timestamp: time.Now()}
	processReleaseCommit(gc, msg, "https://github.com/test/repo", "gitmsg/release")
}

func TestSyncWorkspaceToCache_cacheError(t *testing.T) {
	dir := initTestRepo(t)
	git.CreateCommitOnBranch(dir, "gitmsg/release", "Release\n\n"+`--- GitMsg: ext="release"; v="0.1.0" ---`)

	cache.Reset()
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})

	err := SyncWorkspaceToCache(dir)
	if err == nil {
		t.Error("should fail with cache not open")
	}
}

func TestProcessReleaseCommit_crossRepoEdit(t *testing.T) {
	setupTestDB(t)
	repoURL := relSyncTestRepoURL
	branch := relSyncTestBranch
	canonHash := "c0a01e234567"
	editHash := "c0a02e234567"

	insertTestCommit(t, canonHash, "Original")
	insertTestCommit(t, editHash, "Edited")

	editContent := "Edited\n\n" + `--- GitMsg: ext="release"; tag="v1.0.1"; edits="https://github.com/other/repo#commit:` + canonHash + `@` + branch + `"; v="0.1.0" ---`
	msg := protocol.ParseMessage(editContent)
	gc := git.Commit{Hash: editHash, Timestamp: time.Now()}
	processReleaseCommit(gc, msg, repoURL, branch)
}

// Helper functions

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := copyDir(repoTemplate, dir); err != nil {
		t.Fatalf("copyDir() error = %v", err)
	}
	return dir
}

func countReleaseItems(t *testing.T) int {
	t.Helper()
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM release_items`).Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("countReleaseItems query error = %v", err)
	}
	return count
}

func queryReleaseItem(t *testing.T, repoURL, hash, branch string) ReleaseItem {
	t.Helper()
	item, err := cache.QueryLocked(func(db *sql.DB) (ReleaseItem, error) {
		var r ReleaseItem
		var prerelease int
		err := db.QueryRow(`SELECT repo_url, hash, branch, tag, version, prerelease,
			artifacts, artifact_url, checksums, signed_by
			FROM release_items WHERE repo_url = ? AND hash = ? AND branch = ?`,
			repoURL, hash, branch).Scan(
			&r.RepoURL, &r.Hash, &r.Branch, &r.Tag, &r.Version, &prerelease,
			&r.Artifacts, &r.ArtifactURL, &r.Checksums, &r.SignedBy,
		)
		r.Prerelease = prerelease == 1
		return r, err
	})
	if err != nil {
		t.Fatalf("queryReleaseItem error = %v", err)
	}
	return item
}
