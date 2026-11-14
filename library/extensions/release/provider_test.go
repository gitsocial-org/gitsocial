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
	if got, err := getNewReleaseNotifications(workspaceURL, workdir, true, 0); err != nil || len(got) != 0 {
		t.Errorf("getNewReleaseNotifications(unread) after the stale mark = %d, %v, want 0", len(got), err)
	}
}

func TestNotificationText_fields(t *testing.T) {
	setupTestDB(t)
	const workspaceURL, workdir, repoURL = "https://github.com/ws/text", "/workspace/text", "https://github.com/test/text-release"
	if err := cache.ExecLocked(func(db *sql.DB) error {
		if _, err := db.Exec(`INSERT INTO core_lists (id, name, source, version, workdir) VALUES (?, ?, ?, ?, ?)`, "text-follow", "Follow", "local", "0.1.0", workdir); err != nil {
			return err
		}
		_, err := db.Exec(`INSERT INTO core_list_repositories (list_id, repo_url, branch) VALUES (?, ?, ?)`, "text-follow", repoURL, "main")
		return err
	}); err != nil {
		t.Fatalf("seed list: %v", err)
	}
	cases := []struct {
		hash, message string
		item          ReleaseItem
		want          [3]string
	}{
		{"7e1000000001", "Faster sync\n\nNotes", ReleaseItem{Tag: cache.ToNullString("v1.2.0"), Version: cache.ToNullString("1.2.0")}, [3]string{"Faster sync", "released v1.2.0", "⏏"}},
		{"7e1000000002", "Try it", ReleaseItem{Version: cache.ToNullString("2.0.0-rc.1"), Prerelease: true}, [3]string{"Try it", "released v2.0.0-rc.1 (pre-release)", "⏏"}},
		{"7e1000000003", "v3.0.0", ReleaseItem{Tag: cache.ToNullString("v3.0.0")}, [3]string{"", "released v3.0.0", "⏏"}},
		{"7e1000000004", "No label", ReleaseItem{}, [3]string{"No label", "", "⏏"}},
	}
	for i, c := range cases {
		if err := cache.InsertCommits([]cache.Commit{{Hash: c.hash, RepoURL: repoURL, Branch: releaseTestBranch, AuthorName: "Rel", AuthorEmail: "rel@test.com", Message: c.message, Timestamp: time.Date(2025, 10, 21, 12, i, 0, 0, time.UTC)}}); err != nil {
			t.Fatalf("InsertCommits() error = %v", err)
		}
		item := c.item
		item.RepoURL, item.Hash, item.Branch = repoURL, c.hash, releaseTestBranch
		if err := InsertReleaseItem(item); err != nil {
			t.Fatalf("InsertReleaseItem() error = %v", err)
		}
	}
	items, err := getNewReleaseNotifications(workspaceURL, workdir, false, 0)
	if err != nil {
		t.Fatalf("getNewReleaseNotifications() error = %v", err)
	}
	got := make(map[string][3]string, len(items))
	for _, n := range items {
		got[n.Hash] = [3]string{n.Subject, n.Phrase, n.Glyph}
	}
	for _, c := range cases {
		if got[c.hash] != c.want {
			t.Errorf("%s = %q, want %q", c.hash, got[c.hash], c.want)
		}
	}
}
