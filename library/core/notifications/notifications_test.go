// notifications_test.go - Tests for notification aggregation, read state, and mention processing
package notifications

import (
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

var baseRepoDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "notifications-test-base-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	git.Init(dir, "main")
	git.ExecGit(dir, []string{"config", "user.email", "alice@example.com"})
	git.ExecGit(dir, []string{"config", "user.name", "Test User"})
	git.CreateCommit(dir, git.CommitOptions{Message: "Initial commit", AllowEmpty: true})
	baseRepoDir = dir
	os.Exit(m.Run())
}

func cloneFixture(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	cmd := exec.Command("cp", "-a", baseRepoDir+"/.", dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cloneFixture: %v: %s", err, out)
	}
	return dst
}

func setupTestDB(t *testing.T) {
	t.Helper()
	cache.Reset()
	dir := t.TempDir()
	if err := cache.Open(dir); err != nil {
		t.Fatalf("cache.Open() error = %v", err)
	}
	t.Cleanup(func() { cache.Reset() })
}

// resetProviders saves and restores the global providers slice for test isolation
func resetProviders(t *testing.T) {
	t.Helper()
	mu.Lock()
	saved := make([]namedProvider, len(providers))
	copy(saved, providers)
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		providers = saved
		mu.Unlock()
	})
}

type mockProvider struct {
	notifications []Notification
	unreadCount   int
	err           error
}

func (m *mockProvider) GetNotifications(_ string, _ Filter) ([]Notification, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.notifications, nil
}

func (m *mockProvider) GetUnreadCount(_ string) (int, error) {
	if m.err != nil {
		return 0, m.err
	}
	return m.unreadCount, nil
}

func TestRegisterProvider(t *testing.T) {
	resetProviders(t)
	mock := &mockProvider{
		notifications: []Notification{
			{RepoURL: "https://github.com/test/repo", Hash: "abc123456789", Branch: "main", Type: "mention", Source: "test", Timestamp: time.Now()},
		},
		unreadCount: 1,
	}
	RegisterProvider("test", mock)

	items, err := GetAll("", Filter{})
	if err != nil {
		t.Fatalf("GetAll() error = %v", err)
	}
	found := false
	for _, item := range items {
		if item.Source == "test" {
			found = true
			break
		}
	}
	if !found {
		t.Error("registered provider notifications not found in GetAll")
	}
}

func TestGetAll_aggregatesProviders(t *testing.T) {
	resetProviders(t)
	now := time.Now()
	mock1 := &mockProvider{
		notifications: []Notification{
			{Hash: "aaa111222333", Source: "mock1", Timestamp: now.Add(-1 * time.Hour)},
			{Hash: "bbb111222333", Source: "mock1", Timestamp: now.Add(-3 * time.Hour)},
		},
	}
	mock2 := &mockProvider{
		notifications: []Notification{
			{Hash: "ccc111222333", Source: "mock2", Timestamp: now.Add(-2 * time.Hour)},
		},
	}
	RegisterProvider("mock1", mock1)
	RegisterProvider("mock2", mock2)

	items, err := GetAll("", Filter{})
	if err != nil {
		t.Fatalf("GetAll() error = %v", err)
	}
	// Count only our mock items (exclude init-registered providers that may return nil)
	mockCount := 0
	for _, item := range items {
		if item.Source == "mock1" || item.Source == "mock2" {
			mockCount++
		}
	}
	if mockCount != 3 {
		t.Errorf("expected 3 mock items, got %d", mockCount)
	}
	// Verify sorted by timestamp desc
	for i := 1; i < len(items); i++ {
		if items[i].Timestamp.After(items[i-1].Timestamp) {
			t.Errorf("items not sorted by timestamp desc at index %d", i)
		}
	}
}

func TestGetAll_withLimit(t *testing.T) {
	resetProviders(t)
	now := time.Now()
	mock := &mockProvider{
		notifications: []Notification{
			{Hash: "aaa111222333", Timestamp: now.Add(-1 * time.Hour)},
			{Hash: "bbb111222333", Timestamp: now.Add(-2 * time.Hour)},
			{Hash: "ccc111222333", Timestamp: now.Add(-3 * time.Hour)},
			{Hash: "ddd111222333", Timestamp: now.Add(-4 * time.Hour)},
			{Hash: "eee111222333", Timestamp: now.Add(-5 * time.Hour)},
		},
	}
	RegisterProvider("limitmock", mock)

	items, err := GetAll("", Filter{Limit: 2})
	if err != nil {
		t.Fatalf("GetAll() error = %v", err)
	}
	if len(items) > 2 {
		t.Errorf("limit=2 but got %d items", len(items))
	}
}

func TestGetAll_continuesOnError(t *testing.T) {
	resetProviders(t)
	errMock := &mockProvider{err: errors.New("provider failed")}
	goodMock := &mockProvider{
		notifications: []Notification{
			{Hash: "aaa111222333", Source: "good"},
		},
	}
	RegisterProvider("errprov", errMock)
	RegisterProvider("goodprov", goodMock)

	items, err := GetAll("", Filter{})
	if err != nil {
		t.Fatalf("GetAll() should not return error when provider fails: %v", err)
	}
	found := false
	for _, item := range items {
		if item.Source == "good" {
			found = true
		}
	}
	if !found {
		t.Error("good provider items not returned when other provider errors")
	}
}

func TestGetUnreadCount_aggregates(t *testing.T) {
	resetProviders(t)
	mock1 := &mockProvider{unreadCount: 3}
	mock2 := &mockProvider{unreadCount: 5}
	RegisterProvider("count1", mock1)
	RegisterProvider("count2", mock2)

	total, err := GetUnreadCount("")
	if err != nil {
		t.Fatalf("GetUnreadCount() error = %v", err)
	}
	// Total should be at least 8 (may include init-registered core provider returning 0)
	if total < 8 {
		t.Errorf("GetUnreadCount() = %d, want >= 8", total)
	}
}

func TestGetUnreadCount_continuesOnError(t *testing.T) {
	resetProviders(t)
	errMock := &mockProvider{err: errors.New("fail")}
	goodMock := &mockProvider{unreadCount: 7}
	RegisterProvider("errcount", errMock)
	RegisterProvider("goodcount", goodMock)

	total, err := GetUnreadCount("")
	if err != nil {
		t.Fatalf("GetUnreadCount() error = %v", err)
	}
	if total < 7 {
		t.Errorf("GetUnreadCount() = %d, want >= 7", total)
	}
}

func TestMarkAsRead(t *testing.T) {
	setupTestDB(t)
	err := MarkAsRead("https://github.com/test/repo", "abc123456789", "main")
	if err != nil {
		t.Fatalf("MarkAsRead() error = %v", err)
	}
	// Verify row exists
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_notification_reads WHERE repo_url = ? AND hash = ? AND branch = ?`,
			"https://github.com/test/repo", "abc123456789", "main").Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("query error = %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row in core_notification_reads, got %d", count)
	}
}

func TestMarkAsRead_idempotent(t *testing.T) {
	setupTestDB(t)
	err := MarkAsRead("https://github.com/test/repo", "abc123456789", "main")
	if err != nil {
		t.Fatalf("first MarkAsRead() error = %v", err)
	}
	err = MarkAsRead("https://github.com/test/repo", "abc123456789", "main")
	if err != nil {
		t.Fatalf("second MarkAsRead() error = %v", err)
	}
}

func TestMarkAsUnread(t *testing.T) {
	setupTestDB(t)
	_ = MarkAsRead("https://github.com/test/repo", "abc123456789", "main")
	err := MarkAsUnread("https://github.com/test/repo", "abc123456789", "main")
	if err != nil {
		t.Fatalf("MarkAsUnread() error = %v", err)
	}
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_notification_reads WHERE repo_url = ? AND hash = ? AND branch = ?`,
			"https://github.com/test/repo", "abc123456789", "main").Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("query error = %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows after MarkAsUnread, got %d", count)
	}
}

func TestMarkAsUnread_noop(t *testing.T) {
	setupTestDB(t)
	err := MarkAsUnread("https://github.com/test/repo", "nonexistent12", "main")
	if err != nil {
		t.Fatalf("MarkAsUnread() for non-existent should not error: %v", err)
	}
}

func TestMarkAllAsRead_empty(t *testing.T) {
	setupTestDB(t)
	resetProviders(t)
	mock := &mockProvider{notifications: nil}
	RegisterProvider("emptymock", mock)
	err := MarkAllAsRead("")
	if err != nil {
		t.Fatalf("MarkAllAsRead() with no notifications error = %v", err)
	}
}

func TestMarkAllAsRead_withItems(t *testing.T) {
	setupTestDB(t)
	resetProviders(t)
	now := time.Now()
	mock := &mockProvider{
		notifications: []Notification{
			{RepoURL: "https://github.com/a/b", Hash: "aaa111222333", Branch: "main", Timestamp: now},
			{RepoURL: "https://github.com/a/b", Hash: "bbb111222333", Branch: "main", Timestamp: now},
		},
	}
	RegisterProvider("readmock", mock)
	if err := MarkAllAsRead(""); err != nil {
		t.Fatalf("MarkAllAsRead() error = %v", err)
	}
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_notification_reads`).Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("query error = %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 read rows, got %d", count)
	}
}

func TestMarkAllAsUnread_empty(t *testing.T) {
	setupTestDB(t)
	resetProviders(t)
	mock := &mockProvider{notifications: nil}
	RegisterProvider("emptymock", mock)
	err := MarkAllAsUnread("")
	if err != nil {
		t.Fatalf("MarkAllAsUnread() with no notifications error = %v", err)
	}
}

func TestMarkAllAsUnread_withItems(t *testing.T) {
	setupTestDB(t)
	resetProviders(t)
	now := time.Now()
	n1 := Notification{RepoURL: "https://github.com/a/b", Hash: "aaa111222333", Branch: "main", Timestamp: now}
	n2 := Notification{RepoURL: "https://github.com/a/b", Hash: "bbb111222333", Branch: "main", Timestamp: now}
	mock := &mockProvider{notifications: []Notification{n1, n2}}
	RegisterProvider("unreadmock", mock)
	_ = MarkAsRead(n1.RepoURL, n1.Hash, n1.Branch)
	_ = MarkAsRead(n2.RepoURL, n2.Hash, n2.Branch)
	if err := MarkAllAsUnread(""); err != nil {
		t.Fatalf("MarkAllAsUnread() error = %v", err)
	}
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_notification_reads`).Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("query error = %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 read rows after MarkAllAsUnread, got %d", count)
	}
}

func TestMentionProcessor_insertsRows(t *testing.T) {
	setupTestDB(t)
	processor := MentionProcessor()
	commit := git.Commit{
		Hash:    "abc123456789",
		Message: "Hello @alice@example.com this is a test",
	}
	processor(commit, nil, "https://github.com/test/repo", "main")

	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_mentions WHERE repo_url = ? AND hash = ? AND email = ?`,
			"https://github.com/test/repo", "abc123456789", "alice@example.com").Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("query error = %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 mention row, got %d", count)
	}
}

func TestMentionProcessor_noMentions(t *testing.T) {
	setupTestDB(t)
	processor := MentionProcessor()
	commit := git.Commit{
		Hash:    "def456789abc",
		Message: "A regular message with no mentions",
	}
	processor(commit, nil, "https://github.com/test/repo", "main")

	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_mentions WHERE hash = ?`, "def456789abc").Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("query error = %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 mention rows, got %d", count)
	}
}

func TestMentionProcessor_usesMessageContent(t *testing.T) {
	setupTestDB(t)
	processor := MentionProcessor()
	commit := git.Commit{
		Hash:    "ghi789abcdef",
		Message: "commit message without mention",
	}
	msg := &protocol.Message{
		Content: "Hey @bob@example.com check this out",
		Header:  protocol.Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "post"}},
	}
	processor(commit, msg, "https://github.com/test/repo", "main")

	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_mentions WHERE hash = ? AND email = ?`,
			"ghi789abcdef", "bob@example.com").Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("query error = %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 mention from msg.Content, got %d", count)
	}
}

// setupGitRepo creates a temp git repo with user.email set and returns the workdir.
func setupGitRepo(t *testing.T) string {
	t.Helper()
	return cloneFixture(t)
}

// seedMentionData inserts a commit and a mention into the DB.
func seedMentionData(t *testing.T, hash, authorName, authorEmail string, ts time.Time) {
	t.Helper()
	repoURL := "https://github.com/x/y"
	branch := "main"
	mentionEmail := "alice@example.com"
	tsStr := ts.UTC().Format(time.RFC3339)
	err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`INSERT INTO core_commits (repo_url, hash, branch, author_name, author_email, message, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			repoURL, hash, branch, authorName, authorEmail, "test msg", tsStr)
		if err != nil {
			return err
		}
		_, err = db.Exec(`INSERT INTO core_mentions (repo_url, hash, branch, email) VALUES (?, ?, ?, ?)`,
			repoURL, hash, branch, mentionEmail)
		return err
	})
	if err != nil {
		t.Fatalf("seedMentionData error = %v", err)
	}
}

func TestMentionProvider_GetNotifications(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	now := time.Now()
	seedMentionData(t, "aaa111222333", "Bob", "bob@example.com", now)
	seedMentionData(t, "bbb111222333", "Carol", "carol@example.com", now.Add(-time.Hour))

	p := &mentionProvider{}
	items, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(items))
	}
	if items[0].Type != "mention" || items[0].Source != "core" {
		t.Errorf("unexpected type/source: %s/%s", items[0].Type, items[0].Source)
	}
	if items[0].Actor.Email != "bob@example.com" {
		t.Errorf("expected actor bob, got %s", items[0].Actor.Email)
	}
}

func TestMentionProvider_GetNotifications_excludesSelf(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	now := time.Now()
	seedMentionData(t, "self12345678", "Alice", "alice@example.com", now)

	p := &mentionProvider{}
	items, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 (self-mentions excluded), got %d", len(items))
	}
}

func TestMentionProvider_GetNotifications_unreadOnly(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	now := time.Now()
	seedMentionData(t, "aaa111222333", "Bob", "bob@example.com", now)
	seedMentionData(t, "bbb111222333", "Carol", "carol@example.com", now)
	_ = MarkAsRead("https://github.com/x/y", "aaa111222333", "main")

	p := &mentionProvider{}
	items, err := p.GetNotifications(workdir, Filter{UnreadOnly: true})
	if err != nil {
		t.Fatalf("GetNotifications(UnreadOnly) error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 unread, got %d", len(items))
	}
	if items[0].Hash != "bbb111222333" {
		t.Errorf("expected unread hash bbb111222333, got %s", items[0].Hash)
	}
}

func TestMentionProvider_GetNotifications_withLimit(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	now := time.Now()
	seedMentionData(t, "aaa111222333", "Bob", "bob@example.com", now)
	seedMentionData(t, "bbb111222333", "Carol", "carol@example.com", now.Add(-time.Hour))
	seedMentionData(t, "ccc111222333", "Dan", "dan@example.com", now.Add(-2*time.Hour))

	p := &mentionProvider{}
	items, err := p.GetNotifications(workdir, Filter{Limit: 2})
	if err != nil {
		t.Fatalf("GetNotifications(Limit) error = %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items with limit, got %d", len(items))
	}
}

func TestMentionProvider_GetNotifications_isReadFlag(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	now := time.Now()
	seedMentionData(t, "aaa111222333", "Bob", "bob@example.com", now)
	_ = MarkAsRead("https://github.com/x/y", "aaa111222333", "main")

	p := &mentionProvider{}
	items, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1, got %d", len(items))
	}
	if !items[0].IsRead {
		t.Error("expected IsRead=true for read notification")
	}
}

func TestMentionProvider_GetNotifications_emptyEmail(t *testing.T) {
	setupTestDB(t)
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s (%v)", out, err)
	}

	p := &mentionProvider{}
	items, err := p.GetNotifications(dir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if items != nil {
		t.Errorf("expected nil for empty email, got %v", items)
	}
}

func TestMentionProvider_GetUnreadCount(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	now := time.Now()
	seedMentionData(t, "aaa111222333", "Bob", "bob@example.com", now)
	seedMentionData(t, "bbb111222333", "Carol", "carol@example.com", now)
	_ = MarkAsRead("https://github.com/x/y", "aaa111222333", "main")

	p := &mentionProvider{}
	count, err := p.GetUnreadCount(workdir)
	if err != nil {
		t.Fatalf("GetUnreadCount() error = %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 unread, got %d", count)
	}
}

func TestMentionProvider_GetUnreadCount_emptyEmail(t *testing.T) {
	setupTestDB(t)
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s (%v)", out, err)
	}

	p := &mentionProvider{}
	count, err := p.GetUnreadCount(dir)
	if err != nil {
		t.Fatalf("GetUnreadCount() error = %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for empty email, got %d", count)
	}
}

func TestMentionProvider_GetNotifications_queryError(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	_ = cache.ExecLocked(func(db *sql.DB) error {
		_, _ = db.Exec(`DROP TABLE core_mentions`)
		return nil
	})

	p := &mentionProvider{}
	_, err := p.GetNotifications(workdir, Filter{})
	if err == nil {
		t.Fatal("expected error when core_mentions table is dropped")
	}
}

func TestMentionProvider_GetNotifications_scanError(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	now := time.Now().UTC().Format(time.RFC3339)
	_ = cache.ExecLocked(func(db *sql.DB) error {
		_, _ = db.Exec(`INSERT INTO core_commits (repo_url, hash, branch, author_name, author_email, message, timestamp) VALUES (?, ?, ?, NULL, ?, ?, ?)`,
			"https://github.com/x/y", "scan12345678", "main", "bob@example.com", "test", now)
		_, _ = db.Exec(`INSERT INTO core_mentions (repo_url, hash, branch, email) VALUES (?, ?, ?, ?)`,
			"https://github.com/x/y", "scan12345678", "main", "alice@example.com")
		return nil
	})

	p := &mentionProvider{}
	items, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	// The scan should fail for the NULL author_name row, so it gets skipped
	if len(items) != 0 {
		t.Errorf("expected 0 items (scan error skipped), got %d", len(items))
	}
}

func TestMentionProvider_GetUnreadCount_queryError(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	_ = cache.ExecLocked(func(db *sql.DB) error {
		_, _ = db.Exec(`DROP TABLE core_mentions`)
		return nil
	})

	p := &mentionProvider{}
	_, err := p.GetUnreadCount(workdir)
	if err == nil {
		t.Fatal("expected error when core_mentions table is dropped")
	}
}

func TestMarkAllAsRead_dbError(t *testing.T) {
	setupTestDB(t)
	resetProviders(t)
	now := time.Now()
	mock := &mockProvider{
		notifications: []Notification{
			{RepoURL: "https://github.com/a/b", Hash: "aaa111222333", Branch: "main", Timestamp: now},
		},
	}
	RegisterProvider("dbmock", mock)
	_ = cache.ExecLocked(func(db *sql.DB) error {
		_, _ = db.Exec(`DROP TABLE core_notification_reads`)
		return nil
	})

	err := MarkAllAsRead("")
	if err == nil {
		t.Fatal("expected error when core_notification_reads table is dropped")
	}
}

func TestMarkAllAsUnread_dbError(t *testing.T) {
	setupTestDB(t)
	resetProviders(t)
	now := time.Now()
	mock := &mockProvider{
		notifications: []Notification{
			{RepoURL: "https://github.com/a/b", Hash: "aaa111222333", Branch: "main", Timestamp: now},
		},
	}
	RegisterProvider("dbmock", mock)
	_ = cache.ExecLocked(func(db *sql.DB) error {
		_, _ = db.Exec(`DROP TABLE core_notification_reads`)
		return nil
	})

	err := MarkAllAsUnread("")
	if err == nil {
		t.Fatal("expected error when core_notification_reads table is dropped")
	}
}

func TestMentionProvider_GetNotifications_invalidTimestamp(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`INSERT INTO core_commits (repo_url, hash, branch, author_name, author_email, message, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			"https://github.com/x/y", "nts111222333", "main", "Bob", "bob@example.com", "test", "not-a-date")
		if err != nil {
			return err
		}
		_, err = db.Exec(`INSERT INTO core_mentions (repo_url, hash, branch, email) VALUES (?, ?, ?, ?)`,
			"https://github.com/x/y", "nts111222333", "main", "alice@example.com")
		return err
	})
	if err != nil {
		t.Fatalf("seed error = %v", err)
	}

	p := &mentionProvider{}
	items, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1, got %d", len(items))
	}
	if !items[0].Timestamp.IsZero() {
		t.Errorf("expected zero timestamp for invalid date, got %v", items[0].Timestamp)
	}
}
