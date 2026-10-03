// provider_test.go - Tests for the release notification provider
package release

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
)

// TestNewReleaseNotifications_excludesStaleCommit pins invariant 1 for the provider: a stale release of a followed repository does not notify.
func TestNewReleaseNotifications_excludesStaleCommit(t *testing.T) {
	setupTestDB(t)
	const workspaceURL, workdir = "https://github.com/ws/releases", "/workspace/releases"
	const repoURL, hash = "https://github.com/test/stale-new-release", "4e1ea5e00001"
	if err := cache.ExecLocked(func(db *sql.DB) error {
		if _, err := db.Exec(`INSERT INTO core_lists (id, name, source, version, workdir) VALUES (?, ?, ?, ?, ?)`,
			"release-follow", "Follow", "local", "0.1.0", workdir); err != nil {
			return err
		}
		_, err := db.Exec(`INSERT INTO core_list_repositories (list_id, repo_url, branch) VALUES (?, ?, ?)`, "release-follow", repoURL, "main")
		return err
	}); err != nil {
		t.Fatalf("seed list: %v", err)
	}
	insertReleaseTestCommit(t, repoURL, hash)
	if err := InsertReleaseItem(ReleaseItem{RepoURL: repoURL, Hash: hash, Branch: releaseTestBranch, Tag: cache.ToNullString("v1.0.0")}); err != nil {
		t.Fatalf("InsertReleaseItem() error = %v", err)
	}
	if got, err := getNewReleaseNotifications(workspaceURL, workdir, false, 0); err != nil || len(got) != 1 {
		t.Fatalf("getNewReleaseNotifications() before the stale mark = %d, %v, want 1", len(got), err)
	}
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE core_commits SET stale_since = ? WHERE repo_url = ? AND hash = ?`, time.Now().UTC().Format(time.RFC3339), repoURL, hash)
		return err
	}); err != nil {
		t.Fatalf("mark stale: %v", err)
	}
	if got, err := getNewReleaseNotifications(workspaceURL, workdir, false, 0); err != nil || len(got) != 0 {
		t.Errorf("getNewReleaseNotifications() after the stale mark = %d, %v, want 0", len(got), err)
	}
	if count, err := countUnreadReleases(workspaceURL, workdir); err != nil || count != 0 {
		t.Errorf("countUnreadReleases() = %d, %v, want 0", count, err)
	}
}
