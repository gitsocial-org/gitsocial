// provider_edits_test.go - Tests for the edit notification provider
package notifications

import (
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

const editTestRepoURL = "https://github.com/x/y"
const editTestBranch = "main"

// seedEditPair writes a canonical commit and an edit of it.
func seedEditPair(t *testing.T, canonicalHash, canonicalAuthor, editHash, editAuthorName, editAuthorEmail string, ts time.Time) {
	t.Helper()
	seedEdit(t, canonicalHash, canonicalAuthor, editHash, editAuthorName, editAuthorEmail, ts, false)
}

// seedEdit writes the pair through cache.InsertCommits, the path that records the version from the edit's header.
func seedEdit(t *testing.T, canonicalHash, canonicalAuthor, editHash, editAuthorName, editAuthorEmail string, ts time.Time, retracted bool) {
	t.Helper()
	fields := map[string]string{"type": "post", "edits": "#commit:" + canonicalHash}
	if retracted {
		fields["retracted"] = "true"
	}
	editMessage := protocol.FormatMessage("edit msg", protocol.Header{
		Ext: "social", V: "0.1.0", Fields: fields,
	}, nil)
	// The edit is a minute later, so ORDER BY timestamp DESC is stable.
	err := cache.InsertCommits([]cache.Commit{
		{
			RepoURL: editTestRepoURL, Hash: canonicalHash, Branch: editTestBranch,
			AuthorName: "Canonical Author", AuthorEmail: canonicalAuthor,
			Message: "canonical msg", Timestamp: ts,
		},
		{
			RepoURL: editTestRepoURL, Hash: editHash, Branch: editTestBranch,
			AuthorName: editAuthorName, AuthorEmail: editAuthorEmail,
			Message: editMessage, Timestamp: ts.Add(time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
}

func TestEditProvider_GetNotifications(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t) // alice@example.com
	now := time.Now()
	// alice's canonical, bob edits it
	seedEditPair(t, "ca0111111111", "alice@example.com", "ed0111111111", "Bob", "bob@example.com", now)

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
	if got.Hash != "ed0111111111" {
		t.Errorf("expected edit hash, got %s", got.Hash)
	}
	if got.Actor.Email != "bob@example.com" {
		t.Errorf("expected actor bob, got %s", got.Actor.Email)
	}
	en, ok := got.Item.(EditNotification)
	if !ok {
		t.Fatalf("expected Item to be EditNotification, got %T", got.Item)
	}
	if en.CanonicalHash != "ca0111111111" {
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
	seedEditPair(t, "ca0222222222", "alice@example.com", "ed0222222222", "Alice", "alice@example.com", now)

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
	seedEditPair(t, "ca0333333333", "bob@example.com", "ed0333333333", "Carol", "carol@example.com", now)

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
	seedEditPair(t, "ca0444444444", "alice@example.com", "ed0444444444", "Bob", "bob@example.com", now)
	seedEditPair(t, "ca0555555555", "alice@example.com", "ed0555555555", "Carol", "carol@example.com", now)
	_ = MarkAsRead(editTestRepoURL, "ed0444444444", editTestBranch)

	p := &editProvider{}
	items, err := p.GetNotifications(workdir, Filter{UnreadOnly: true})
	if err != nil {
		t.Fatalf("GetNotifications(UnreadOnly) error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 unread, got %d", len(items))
	}
	if items[0].Hash != "ed0555555555" {
		t.Errorf("expected unread hash ed0555555555, got %s", items[0].Hash)
	}
}

func TestEditProvider_GetNotifications_isReadFlag(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	now := time.Now()
	seedEditPair(t, "ca0666666666", "alice@example.com", "ed0666666666", "Bob", "bob@example.com", now)
	_ = MarkAsRead(editTestRepoURL, "ed0666666666", editTestBranch)

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
	seedEditPair(t, "ca0777777777", "alice@example.com", "ed0777777777", "Bob", "bob@example.com", now)
	seedEditPair(t, "ca0888888888", "alice@example.com", "ed0888888888", "Carol", "carol@example.com", now.Add(-time.Hour))
	seedEditPair(t, "ca0999999999", "alice@example.com", "ed0999999999", "Dan", "dan@example.com", now.Add(-2*time.Hour))

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
	seedEditPair(t, "ca0aaaaaaaaa", "alice@example.com", "ed0aaaaaaaaa", "Bob", "bob@example.com", now)
	seedEditPair(t, "ca0bbbbbbbbb", "alice@example.com", "ed0bbbbbbbbb", "Carol", "carol@example.com", now)
	// One read, one unread
	_ = MarkAsRead(editTestRepoURL, "ed0aaaaaaaaa", editTestBranch)

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
	seedEdit(t, "ca0ccccccccc", "alice@example.com", "ed0ccccccccc", "Bob", "bob@example.com", now, true)

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
