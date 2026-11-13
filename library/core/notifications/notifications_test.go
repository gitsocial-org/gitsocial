// notifications_test.go - Tests for notification aggregation, read state, and mention processing
package notifications

import (
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

var baseRepoDir string

func TestMain(m *testing.M) {
	dir, err := testutil.NewRepoTemplate()
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	git.ExecGit(dir, []string{"config", "user.email", "alice@example.com"})
	baseRepoDir = dir
	os.Exit(m.Run())
}

func cloneFixture(t *testing.T) string {
	t.Helper()
	return testutil.CopyRepo(t, baseRepoDir)
}

func setupTestDB(t *testing.T) {
	t.Helper()
	testutil.OpenTempCache(t, "")
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
	err           error
}

func (m *mockProvider) GetNotifications(_ string, _ Filter) ([]Notification, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.notifications, nil
}

func TestRegisterProvider(t *testing.T) {
	resetProviders(t)
	mock := &mockProvider{
		notifications: []Notification{
			{RepoURL: "https://github.com/test/repo", Hash: "abc123456789", Branch: "main", Type: "mention", Source: "test", Timestamp: time.Now()},
		},
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

// TestUnreadCount_equalsFeed checks that the count is the number of rows of the unread feed, and that a failing provider is skipped.
func TestUnreadCount_equalsFeed(t *testing.T) {
	setupTestDB(t)
	resetProviders(t)
	mu.Lock()
	providers = nil
	mu.Unlock()
	now := time.Now()
	repo := "https://github.com/a/b"
	RegisterProvider("failing", &mockProvider{err: errors.New("fail")})
	RegisterProvider("core", &mockProvider{notifications: []Notification{
		{RepoURL: repo, Hash: "aaa111222333", Type: "edit", Source: "core", Timestamp: now},
		{RepoURL: repo, Hash: "bbb111222333", Type: "mention", Source: "core", Timestamp: now},
	}})
	RegisterProvider("pm", &mockProvider{notifications: []Notification{
		{RepoURL: repo, Hash: "aaa111222333", Type: "issue-closed", Source: "pm", Timestamp: now},
		{RepoURL: repo, Type: "branch-diverged", Condition: true, Timestamp: now},
	}})
	feed, _ := GetAll("", Filter{UnreadOnly: true})
	count, err := GetUnreadCount("")
	if err != nil || count != 3 || len(feed) != 3 {
		t.Errorf("GetUnreadCount() = %d, %v with %d feed rows, want 3 and 3", count, err, len(feed))
	}
}

// TestGather_oneEventPerKey checks that one commit gives one event of the type that decision order keeps, also with a type filter and a limit.
func TestGather_oneEventPerKey(t *testing.T) {
	setupTestDB(t)
	resetProviders(t)
	mu.Lock()
	providers = nil
	mu.Unlock()
	now := time.Now()
	repo := "https://github.com/a/b"
	at := func(hash, typ, source string, age time.Duration) Notification {
		return Notification{RepoURL: repo, Hash: hash, Type: typ, Source: source, Timestamp: now.Add(-age)}
	}
	RegisterProvider("core", &mockProvider{notifications: []Notification{
		at("aaa111222333", "edit", "core", 0),
		at("aaa111222333", "mention", "core", 0),
		at("bbb111222333", "reference", "core", time.Minute),
		at("bbb111222333", "edit", "core", time.Minute),
		at("ccc111222333", "mention", "core", 2*time.Minute),
		at("ddd111222333", "edit", "core", 3*time.Minute),
		at("eee111222333", "reference", "core", 4*time.Minute),
		at("fff111222333", "edit", "core", 5*time.Minute),
	}})
	RegisterProvider("social", &mockProvider{notifications: []Notification{
		at("ccc111222333", "comment", "social", 2*time.Minute),
	}})
	RegisterProvider("ext", &mockProvider{notifications: []Notification{
		at("ccc111222333", "memo-comment", "memo", 2*time.Minute),
		at("ddd111222333", "review-requested", "review", 3*time.Minute),
		at("ddd111222333", "fork-pr", "review", 3*time.Minute),
		at("eee111222333", "feedback", "review", 4*time.Minute),
		at("fff111222333", "pr-merged", "review", 5*time.Minute),
	}})
	tests := []struct {
		name   string
		filter Filter
		want   []string
	}{
		{"all", Filter{}, []string{"mention", "reference", "memo-comment", "review-requested", "feedback", "pr-merged"}},
		{"limit", Filter{Limit: 2}, []string{"mention", "reference"}},
		{"type filter runs first", Filter{Types: []string{"mention", "edit"}}, []string{"mention", "edit", "mention", "edit", "edit"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := GetAll("", tt.filter)
			if err != nil {
				t.Fatalf("GetAll() error = %v", err)
			}
			got := make([]string, len(items))
			for i, n := range items {
				got[i] = n.Type
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("types = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestOnePerKey_workspaceRowWins checks that a commit that is in the workspace and in a fork gives the event of the workspace, and that two follows stay.
func TestOnePerKey_workspaceRowWins(t *testing.T) {
	ws, fork := "https://github.com/a/b", "https://github.com/fork/b"
	got := onePerKey([]Notification{
		{RepoURL: fork, Hash: "aaa111222333", Type: "issue-assigned", Source: "pm"},
		{RepoURL: ws, Hash: "aaa111222333", Type: "mention", Source: "core"},
		{RepoURL: fork, Hash: "bbb111222333", Type: "mention", Source: "core"},
		{RepoURL: ws, Hash: "follow", Type: "follow", Source: "social"},
		{RepoURL: fork, Hash: "follow", Type: "follow", Source: "social"},
	}, ws)
	want := []string{ws + " mention", fork + " mention", ws + " follow", fork + " follow"}
	keys := make([]string, len(got))
	for i, n := range got {
		keys[i] = n.RepoURL + " " + n.Type
	}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("onePerKey() = %v, want %v", keys, want)
	}
}

// TestOnePerKey_activityRanksLast checks that a commit with a type of the default level keeps that type, also when only a fork row has it.
func TestOnePerKey_activityRanksLast(t *testing.T) {
	ws, fork := "https://github.com/a/b", "https://github.com/fork/b"
	got := onePerKey([]Notification{
		{RepoURL: ws, Hash: "aaa111222333", Type: "activity", Source: "social"},
		{RepoURL: ws, Hash: "aaa111222333", Type: "edit", Source: "core"},
		{RepoURL: ws, Hash: "bbb111222333", Type: "activity", Source: "social"},
		{RepoURL: fork, Hash: "bbb111222333", Type: "fork-issue", Source: "pm"},
		{RepoURL: ws, Hash: "ccc111222333", Type: "comment", Source: "social"},
		{RepoURL: ws, Hash: "ccc111222333", Type: "activity", Source: "social"},
		{RepoURL: ws, Hash: "ddd111222333", Type: "activity", Source: "social"},
	}, ws)
	want := []string{ws + " edit", fork + " fork-issue", ws + " comment", ws + " activity"}
	keys := make([]string, len(got))
	for i, n := range got {
		keys[i] = n.RepoURL + " " + n.Type
	}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("onePerKey() = %v, want %v", keys, want)
	}
}

// TestClaim_copyOfCommit checks that the state of a commit holds for its copy in a second repository: no second claim, and one unread mark for both.
func TestClaim_copyOfCommit(t *testing.T) {
	setupTestDB(t)
	ws, fork := "https://github.com/a/b", "https://github.com/fork/b"
	if claimed, err := Claim([]Notification{{RepoURL: fork, Hash: "aaa111222333"}, {RepoURL: fork, Hash: "follow"}}); err != nil || len(claimed) != 2 {
		t.Fatalf("Claim() = %d, %v, want 2", len(claimed), err)
	}
	claimed, err := Claim([]Notification{{RepoURL: ws, Hash: "aaa111222333"}, {RepoURL: ws, Hash: "follow"}})
	if err != nil || len(claimed) != 1 || claimed[0].Hash != "follow" {
		t.Errorf("Claim() of the copies = %+v, %v, want only the follow of the second repository", claimed, err)
	}
	if err := MarkAsRead(fork, "aaa111222333"); err != nil {
		t.Fatalf("MarkAsRead() error = %v", err)
	}
	if err := MarkAsUnread(ws, "aaa111222333"); err != nil {
		t.Fatalf("MarkAsUnread() error = %v", err)
	}
	if _, read := stateCounts(t); read != 0 {
		t.Errorf("read rows after the unread mark through the copy = %d, want 0", read)
	}
}

// TestCondition_noState checks that a condition hides no event and no other condition, and that "read all" and a claim leave it without a state row.
func TestCondition_noState(t *testing.T) {
	setupTestDB(t)
	resetProviders(t)
	mu.Lock()
	providers = nil
	mu.Unlock()
	now := time.Now()
	repo := "https://github.com/a/b"
	RegisterProvider("review", &mockProvider{notifications: []Notification{
		{RepoURL: repo, Hash: "aaa111222333", Type: "review-requested", Source: "review", Timestamp: now},
		{RepoURL: repo, Hash: "aaa111222333", Type: "head-advanced", Source: "review", Condition: true, Timestamp: now},
		{RepoURL: repo, Hash: "aaa111222333", Type: "base-advanced", Source: "review", Condition: true, Timestamp: now},
		{RepoURL: repo, Type: "branch-diverged", Condition: true, Timestamp: now},
		{RepoURL: repo, Type: "branch-diverged", Condition: true, Timestamp: now},
	}})
	if items, _ := GetAll("", Filter{}); len(items) != 5 {
		t.Fatalf("GetAll() = %d rows, want the event and the 4 conditions", len(items))
	}
	if err := MarkAllAsRead(""); err != nil {
		t.Fatalf("MarkAllAsRead() error = %v", err)
	}
	items, _ := GetAll("", Filter{})
	if claimed, err := Claim(items); err != nil || len(claimed) != 0 {
		t.Errorf("Claim() = %d, %v, want 0: the event is read and a condition is never claimed", len(claimed), err)
	}
	if rows, read := stateCounts(t); rows != 1 || read != 1 {
		t.Errorf("state = %d rows with %d read, want the 1 row of the event", rows, read)
	}
}

func TestMarkAsRead(t *testing.T) {
	setupTestDB(t)
	err := MarkAsRead("https://github.com/test/repo", "abc123456789")
	if err != nil {
		t.Fatalf("MarkAsRead() error = %v", err)
	}
	// Verify row exists
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_notification_state WHERE repo_url = ? AND hash = ? AND read_at IS NOT NULL`,
			"https://github.com/test/repo", "abc123456789").Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("query error = %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row in core_notification_state, got %d", count)
	}
}

func TestMarkAsRead_idempotent(t *testing.T) {
	setupTestDB(t)
	err := MarkAsRead("https://github.com/test/repo", "abc123456789")
	if err != nil {
		t.Fatalf("first MarkAsRead() error = %v", err)
	}
	err = MarkAsRead("https://github.com/test/repo", "abc123456789")
	if err != nil {
		t.Fatalf("second MarkAsRead() error = %v", err)
	}
}

func TestMarkAsUnread(t *testing.T) {
	setupTestDB(t)
	_ = MarkAsRead("https://github.com/test/repo", "abc123456789")
	err := MarkAsUnread("https://github.com/test/repo", "abc123456789")
	if err != nil {
		t.Fatalf("MarkAsUnread() error = %v", err)
	}
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_notification_state WHERE repo_url = ? AND hash = ? AND read_at IS NOT NULL`,
			"https://github.com/test/repo", "abc123456789").Scan(&c)
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
	err := MarkAsUnread("https://github.com/test/repo", "nonexistent12")
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
		err := db.QueryRow(`SELECT COUNT(*) FROM core_notification_state WHERE read_at IS NOT NULL`).Scan(&c)
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
	_ = MarkAsRead(n1.RepoURL, n1.Hash)
	_ = MarkAsRead(n2.RepoURL, n2.Hash)
	if err := MarkAllAsUnread(""); err != nil {
		t.Fatalf("MarkAllAsUnread() error = %v", err)
	}
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_notification_state WHERE read_at IS NOT NULL`).Scan(&c)
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
	_ = MarkAsRead("https://github.com/x/y", "aaa111222333")

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
	_ = MarkAsRead("https://github.com/x/y", "aaa111222333")

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
	_ = MarkAsRead("https://github.com/x/y", "aaa111222333")

	p := &mentionProvider{}
	count, err := unreadOf(p, workdir)
	if err != nil {
		t.Fatalf("GetUnreadCount() error = %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 unread, got %d", count)
	}
}

// TestMentionProvider_excludesStaleCommit: a mention in a stale commit gives no notification and no unread count.
func TestMentionProvider_excludesStaleCommit(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	now := time.Now()
	seedMentionData(t, "aaa111222333", "Bob", "bob@example.com", now)
	seedMentionData(t, "bbb111222333", "Carol", "carol@example.com", now)
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE core_commits SET stale_since = ? WHERE hash = ?`, now.UTC().Format(time.RFC3339), "aaa111222333")
		return err
	}); err != nil {
		t.Fatalf("mark stale: %v", err)
	}
	p := &mentionProvider{}
	items, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if len(items) != 1 || items[0].Actor.Email != "carol@example.com" {
		t.Errorf("notifications = %+v, want the one from the live commit", items)
	}
	count, err := unreadOf(p, workdir)
	if err != nil {
		t.Fatalf("GetUnreadCount() error = %v", err)
	}
	if count != 1 {
		t.Errorf("unread count = %d, want 1", count)
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
	count, err := unreadOf(p, dir)
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

func TestMentionProvider_GetUnreadCount_queryError(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	_ = cache.ExecLocked(func(db *sql.DB) error {
		_, _ = db.Exec(`DROP TABLE core_mentions`)
		return nil
	})

	p := &mentionProvider{}
	_, err := unreadOf(p, workdir)
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
		_, _ = db.Exec(`DROP TABLE core_notification_state`)
		return nil
	})

	err := MarkAllAsRead("")
	if err == nil {
		t.Fatal("expected error when core_notification_state table is dropped")
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
		_, _ = db.Exec(`DROP TABLE core_notification_state`)
		return nil
	})

	err := MarkAllAsUnread("")
	if err == nil {
		t.Fatal("expected error when core_notification_state table is dropped")
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

// TestClaim_staysUnread checks that a claim gives each event one time and writes no read time.
func TestClaim_staysUnread(t *testing.T) {
	setupTestDB(t)
	items := []Notification{
		{RepoURL: "https://github.com/a/b", Hash: "aaa111222333"},
		{RepoURL: "https://github.com/a/b", Hash: "bbb111222333"},
		{RepoURL: "https://github.com/a/b", Type: "branch-diverged", Condition: true},
	}
	first, err := Claim(items)
	if err != nil || len(first) != 2 {
		t.Fatalf("Claim() = %d events, %v, want the 2 events and not the condition", len(first), err)
	}
	second, err := Claim(items)
	if err != nil || len(second) != 0 {
		t.Errorf("second Claim() = %d events, %v, want 0", len(second), err)
	}
	rows, read := stateCounts(t)
	if rows != 2 || read != 0 {
		t.Errorf("state = %d rows with %d read, want 2 rows and 0 read", rows, read)
	}
}

// TestClaim_readThenUnread checks that an event that was read is not claimed after it is marked unread.
func TestClaim_readThenUnread(t *testing.T) {
	setupTestDB(t)
	n := Notification{RepoURL: "https://github.com/a/b", Hash: "aaa111222333"}
	if err := MarkAsRead(n.RepoURL, n.Hash); err != nil {
		t.Fatalf("MarkAsRead() error = %v", err)
	}
	if err := MarkAsUnread(n.RepoURL, n.Hash); err != nil {
		t.Fatalf("MarkAsUnread() error = %v", err)
	}
	claimed, err := Claim([]Notification{n})
	if err != nil || len(claimed) != 0 {
		t.Errorf("Claim() = %d events, %v, want 0", len(claimed), err)
	}
	if rows, read := stateCounts(t); rows != 1 || read != 0 {
		t.Errorf("state = %d rows with %d read, want 1 unread row", rows, read)
	}
}

// TestMarkAsRead_claimedEvent checks that a mark as read writes the read time of an event that was only sent.
func TestMarkAsRead_claimedEvent(t *testing.T) {
	setupTestDB(t)
	n := Notification{RepoURL: "https://github.com/a/b", Hash: "aaa111222333"}
	if _, err := Claim([]Notification{n}); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if err := MarkAsRead(n.RepoURL, n.Hash); err != nil {
		t.Fatalf("MarkAsRead() error = %v", err)
	}
	if rows, read := stateCounts(t); rows != 1 || read != 1 {
		t.Errorf("state = %d rows with %d read, want 1 read row", rows, read)
	}
}

// stateCounts returns the number of state rows and the number of them that are read.
func stateCounts(t *testing.T) (rows, read int) {
	t.Helper()
	err := cache.ExecLocked(func(db *sql.DB) error {
		return db.QueryRow(`SELECT COUNT(*), COUNT(read_at) FROM core_notification_state`).Scan(&rows, &read)
	})
	if err != nil {
		t.Fatalf("count state: %v", err)
	}
	return rows, read
}

// unreadOf returns the number of unread notifications of one provider.
func unreadOf(p Provider, workdir string) (int, error) {
	items, err := p.GetNotifications(workdir, Filter{UnreadOnly: true})
	return len(items), err
}
