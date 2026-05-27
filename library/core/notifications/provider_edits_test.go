// provider_edits_test.go - Tests for the edit notification provider
package notifications

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
)

// seedEditPair inserts a canonical commit and an edit commit linked via
// core_commits_version. Returns the edit's hash for read-state tests.
func seedEditPair(t *testing.T, canonicalHash, canonicalAuthor, editHash, editAuthorName, editAuthorEmail string, ts time.Time) {
	t.Helper()
	repoURL := "https://github.com/x/y"
	branch := "main"
	tsStr := ts.UTC().Format(time.RFC3339)
	err := cache.ExecLocked(func(db *sql.DB) error {
		// Canonical
		if _, err := db.Exec(`INSERT INTO core_commits (repo_url, hash, branch, author_name, author_email, message, timestamp)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			repoURL, canonicalHash, branch, "Canonical Author", canonicalAuthor, "canonical msg", tsStr); err != nil {
			return err
		}
		// Edit (use a slightly later timestamp so ORDER BY DESC is stable)
		editTs := ts.Add(time.Minute).UTC().Format(time.RFC3339)
		if _, err := db.Exec(`INSERT INTO core_commits (repo_url, hash, branch, author_name, author_email, message, timestamp)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			repoURL, editHash, branch, editAuthorName, editAuthorEmail, "edit msg", editTs); err != nil {
			return err
		}
		// Version link
		_, err := db.Exec(`INSERT INTO core_commits_version
			(edit_repo_url, edit_hash, edit_branch, canonical_repo_url, canonical_hash, canonical_branch, is_retracted)
			VALUES (?, ?, ?, ?, ?, ?, 0)`,
			repoURL, editHash, branch, repoURL, canonicalHash, branch)
		return err
	})
	if err != nil {
		t.Fatalf("seedEditPair error = %v", err)
	}
}

func TestEditProvider_GetNotifications(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t) // alice@example.com
	now := time.Now()
	// alice's canonical, bob edits it
	seedEditPair(t, "can111111111", "alice@example.com", "edit11111111", "Bob", "bob@example.com", now)

	p := &editProvider{}
	items, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(items))
	}
	got := items[0]
	if got.Type != "edit" || got.Source != "core" {
		t.Errorf("unexpected type/source: %s/%s", got.Type, got.Source)
	}
	if got.Hash != "edit11111111" {
		t.Errorf("expected edit hash, got %s", got.Hash)
	}
	if got.Actor.Email != "bob@example.com" {
		t.Errorf("expected actor bob, got %s", got.Actor.Email)
	}
	en, ok := got.Item.(EditNotification)
	if !ok {
		t.Fatalf("expected Item to be EditNotification, got %T", got.Item)
	}
	if en.CanonicalHash != "can111111111" {
		t.Errorf("expected canonical hash in item, got %s", en.CanonicalHash)
	}
	if en.IsRetracted {
		t.Error("expected IsRetracted=false")
	}
}

func TestEditProvider_GetNotifications_excludesSelfEdits(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t) // alice@example.com
	now := time.Now()
	// alice edits alice's own canonical — should NOT notify alice
	seedEditPair(t, "can222222222", "alice@example.com", "edit22222222", "Alice", "alice@example.com", now)

	p := &editProvider{}
	items, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 (self-edits excluded), got %d", len(items))
	}
}

func TestEditProvider_GetNotifications_excludesEditsToOthersCanonical(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t) // alice@example.com
	now := time.Now()
	// bob's canonical, carol edits — alice shouldn't see this (not her canonical)
	seedEditPair(t, "can333333333", "bob@example.com", "edit33333333", "Carol", "carol@example.com", now)

	p := &editProvider{}
	items, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 (not alice's canonical), got %d", len(items))
	}
}

func TestEditProvider_GetNotifications_unreadOnly(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	now := time.Now()
	seedEditPair(t, "can444444444", "alice@example.com", "edit44444444", "Bob", "bob@example.com", now)
	seedEditPair(t, "can555555555", "alice@example.com", "edit55555555", "Carol", "carol@example.com", now)
	_ = MarkAsRead("https://github.com/x/y", "edit44444444", "main")

	p := &editProvider{}
	items, err := p.GetNotifications(workdir, Filter{UnreadOnly: true})
	if err != nil {
		t.Fatalf("GetNotifications(UnreadOnly) error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 unread, got %d", len(items))
	}
	if items[0].Hash != "edit55555555" {
		t.Errorf("expected unread hash edit55555555, got %s", items[0].Hash)
	}
}

func TestEditProvider_GetNotifications_isReadFlag(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	now := time.Now()
	seedEditPair(t, "can666666666", "alice@example.com", "edit66666666", "Bob", "bob@example.com", now)
	_ = MarkAsRead("https://github.com/x/y", "edit66666666", "main")

	p := &editProvider{}
	items, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1, got %d", len(items))
	}
	if !items[0].IsRead {
		t.Error("expected IsRead=true after MarkAsRead")
	}
}

func TestEditProvider_GetNotifications_withLimit(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	now := time.Now()
	seedEditPair(t, "can777777777", "alice@example.com", "edit77777777", "Bob", "bob@example.com", now)
	seedEditPair(t, "can888888888", "alice@example.com", "edit88888888", "Carol", "carol@example.com", now.Add(-time.Hour))
	seedEditPair(t, "can999999999", "alice@example.com", "edit99999999", "Dan", "dan@example.com", now.Add(-2*time.Hour))

	p := &editProvider{}
	items, err := p.GetNotifications(workdir, Filter{Limit: 2})
	if err != nil {
		t.Fatalf("GetNotifications(Limit) error = %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items with limit, got %d", len(items))
	}
}

func TestEditProvider_GetUnreadCount(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	now := time.Now()
	seedEditPair(t, "canAAAAAAAAA", "alice@example.com", "editAAAAAAAA", "Bob", "bob@example.com", now)
	seedEditPair(t, "canBBBBBBBBB", "alice@example.com", "editBBBBBBBB", "Carol", "carol@example.com", now)
	// One read, one unread
	_ = MarkAsRead("https://github.com/x/y", "editAAAAAAAA", "main")

	p := &editProvider{}
	count, err := p.GetUnreadCount(workdir)
	if err != nil {
		t.Fatalf("GetUnreadCount() error = %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 unread, got %d", count)
	}
}

func TestEditProvider_GetNotifications_emptyEmail(t *testing.T) {
	setupTestDB(t)
	dir := t.TempDir() // no git repo → no user.email
	p := &editProvider{}
	items, err := p.GetNotifications(dir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if items != nil {
		t.Errorf("expected nil when no user.email, got %v", items)
	}
}

func TestEditProvider_GetNotifications_retractedEdit(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	now := time.Now()
	repoURL := "https://github.com/x/y"
	branch := "main"
	// Manually seed a retracted edit (seedEditPair uses is_retracted=0)
	err := cache.ExecLocked(func(db *sql.DB) error {
		if _, err := db.Exec(`INSERT INTO core_commits (repo_url, hash, branch, author_name, author_email, message, timestamp)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			repoURL, "canCCCCCCCCC", branch, "Alice", "alice@example.com", "canonical msg", now.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
		if _, err := db.Exec(`INSERT INTO core_commits (repo_url, hash, branch, author_name, author_email, message, timestamp)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			repoURL, "editCCCCCCCC", branch, "Bob", "bob@example.com", "retract msg", now.Add(time.Minute).UTC().Format(time.RFC3339)); err != nil {
			return err
		}
		_, err := db.Exec(`INSERT INTO core_commits_version
			(edit_repo_url, edit_hash, edit_branch, canonical_repo_url, canonical_hash, canonical_branch, is_retracted)
			VALUES (?, ?, ?, ?, ?, ?, 1)`,
			repoURL, "editCCCCCCCC", branch, repoURL, "canCCCCCCCCC", branch)
		return err
	})
	if err != nil {
		t.Fatalf("seed error = %v", err)
	}

	p := &editProvider{}
	items, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(items))
	}
	en, ok := items[0].Item.(EditNotification)
	if !ok {
		t.Fatalf("expected EditNotification, got %T", items[0].Item)
	}
	if !en.IsRetracted {
		t.Error("expected IsRetracted=true on retracted edit")
	}
}
