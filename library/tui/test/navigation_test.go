// navigation_test.go - View-to-view navigation tests
package test

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

func TestNavigation(t *testing.T) {
	f := getFixture(t)
	h := New(t, f.Workdir, f.CacheDir)

	t.Run("GlobalKeys", func(t *testing.T) {
		tests := []struct {
			key  string
			path string
		}{
			{"S", "/social/timeline"},
			{"P", "/pm/board"},
			{"R", "/review/prs"},
			{"V", "/release/list"},
			{"M", "/memo/project"},
		}
		for _, tt := range tests {
			t.Run("key_"+tt.key, func(t *testing.T) {
				h.Navigate("/settings")
				h.SendKey(tt.key)
				got := h.CurrentPath()
				if got != tt.path {
					t.Errorf("after %q: path = %q, want %q", tt.key, got, tt.path)
				}
				assertNotEmpty(t, h.Rendered())
			})
		}
	})
	t.Run("Back", func(t *testing.T) {
		h.Navigate("/social/timeline")
		h.Navigate("/settings")
		if h.CurrentPath() != "/settings" {
			t.Fatalf("expected /settings, got %q", h.CurrentPath())
		}
		h.SendKey("esc")
		if h.CurrentPath() != "/social/timeline" {
			t.Errorf("after esc: path = %q, want /social/timeline", h.CurrentPath())
		}
	})
	t.Run("SiteEditToggle", func(t *testing.T) {
		h.Navigate("/config/site")
		if h.CurrentPath() != "/config/site" {
			t.Fatalf("expected /config/site, got %q", h.CurrentPath())
		}
		h.SendKey("e") // enter edit mode on the first field
		assertNotEmpty(t, h.Rendered())
		h.SendKey("esc") // esc cancels edit, stays on the view
		if h.CurrentPath() != "/config/site" {
			t.Errorf("after edit esc: path = %q, want /config/site", h.CurrentPath())
		}
	})
	t.Run("MultiLevelBack", func(t *testing.T) {
		h.Navigate("/social/timeline")
		h.Navigate("/settings")
		h.Navigate("/cache")
		h.SendKey("esc")
		if h.CurrentPath() != "/settings" {
			t.Errorf("first esc: path = %q, want /settings", h.CurrentPath())
		}
		h.SendKey("esc")
		if h.CurrentPath() != "/social/timeline" {
			t.Errorf("second esc: path = %q, want /social/timeline", h.CurrentPath())
		}
	})
	t.Run("Detail", func(t *testing.T) {
		h.Navigate("/pm/issues")
		listPath := h.CurrentPath()
		h.SendKey("enter")
		detailPath := h.CurrentPath()
		if detailPath == listPath {
			t.Log("enter did not navigate to detail — may be empty list or view-specific behavior")
		}
		h.SendKey("esc")
		assertNotEmpty(t, h.Rendered())
	})
	t.Run("Search", func(t *testing.T) {
		h.Navigate("/social/timeline")
		h.SendKey("/")
		if h.CurrentPath() != "/search" {
			t.Errorf("after /: path = %q, want /search", h.CurrentPath())
		}
		assertNotEmpty(t, h.Rendered())
	})
	t.Run("Help", func(t *testing.T) {
		h.Navigate("/social/timeline")
		h.SendKey("?")
		if h.CurrentPath() != "/help" {
			t.Errorf("after ?: path = %q, want /help", h.CurrentPath())
		}
		assertNotEmpty(t, h.Rendered())
	})
	t.Run("Notifications", func(t *testing.T) {
		h.Navigate("/social/timeline")
		h.SendKey("@")
		if h.CurrentPath() != "/notifications" {
			t.Errorf("after @: path = %q, want /notifications", h.CurrentPath())
		}
		assertNotEmpty(t, h.Rendered())
	})
}

// seedMovedCommit caches a commit whose home moved from feature/x to main, and a comment that names the old branch.
func seedMovedCommit(t *testing.T, repoURL, hash, commentHash, comment string) {
	t.Helper()
	at := time.Date(2025, 10, 20, 12, 0, 0, 0, time.UTC)
	commits := []cache.Commit{
		{Hash: hash, RepoURL: repoURL, Branch: "feature/x", AuthorName: "Coder", AuthorEmail: "coder@test.com", Message: "Moved code change", Timestamp: at},
		{Hash: hash, RepoURL: repoURL, Branch: "main", AuthorName: "Coder", AuthorEmail: "coder@test.com", Message: "Moved code change", Timestamp: at},
		{Hash: commentHash, RepoURL: repoURL, Branch: "gitmsg/social", AuthorName: "Reader", AuthorEmail: "reader@test.com", Message: comment, Timestamp: at.Add(time.Hour)},
	}
	if err := cache.InsertCommits(commits); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE core_commits SET stale_since = '2025-10-21T00:00:00Z' WHERE repo_url = ? AND hash = ? AND branch = 'feature/x'`, repoURL, hash)
		return err
	}); err != nil {
		t.Fatalf("mark the old row stale: %v", err)
	}
	if err := social.InsertSocialItem(social.SocialItem{
		RepoURL: repoURL, Hash: commentHash, Branch: "gitmsg/social", Type: "comment",
		OriginalRepoURL: cache.ToNullString(repoURL),
		OriginalHash:    cache.ToNullString(hash),
		OriginalBranch:  cache.ToNullString("feature/x"),
	}); err != nil {
		t.Fatalf("InsertSocialItem() error = %v", err)
	}
}

// TestPostView_movedRoot pins invariant 11: a reference with the old branch opens the thread of the live row.
func TestPostView_movedRoot(t *testing.T) {
	f := SetupFixture(t)
	h := New(t, f.Workdir, f.CacheDir)
	repo := "https://github.com/moved/view"
	seedMovedCommit(t, repo, "f00d00000001", "f00d00000002", "Comment on the moved commit")

	h.NavigateTo(tuicore.LocDetail(protocol.CreateRef(protocol.RefTypeCommit, "f00d00000001", repo, "feature/x")))
	out := renderedAfterLoad(h, []string{"Moved code change", "Comment on the moved commit"})
	for _, want := range []string{"Moved code change", "Comment on the moved commit"} {
		if !strings.Contains(out, want) {
			t.Errorf("thread view is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "post not found in thread") {
		t.Errorf("thread view did not find the moved root:\n%s", out)
	}
}
