// commits_test.go - Tests for commit storage and retrieval
package cache

import (
	"database/sql"
	"testing"
	"time"
)

func TestInsertCommits_empty(t *testing.T) {
	setupTestDB(t)
	if err := InsertCommits(nil); err != nil {
		t.Errorf("InsertCommits(nil) error = %v", err)
	}
}

func TestInsertCommits_basic(t *testing.T) {
	setupTestDB(t)

	commits := []Commit{
		{
			Hash:        "abc123456789",
			RepoURL:     "https://github.com/user/repo",
			Branch:      "main",
			AuthorName:  "Alice",
			AuthorEmail: "alice@example.com",
			Message:     "First commit",
			Timestamp:   time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
		},
		{
			Hash:        "def456789abc",
			RepoURL:     "https://github.com/user/repo",
			Branch:      "main",
			AuthorName:  "Bob",
			AuthorEmail: "bob@example.com",
			Message:     "Second commit",
			Timestamp:   time.Date(2025, 10, 21, 13, 0, 0, 0, time.UTC),
		},
	}

	if err := InsertCommits(commits); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}

	// Verify commits exist
	meta, err := GetRepositoryFetchMeta("https://github.com/user/repo")
	if err != nil {
		t.Fatalf("GetRepositoryFetchMeta() error = %v", err)
	}
	if meta.CommitCount != 2 {
		t.Errorf("CommitCount = %d, want 2", meta.CommitCount)
	}
}

func TestInsertCommits_defaultBranch(t *testing.T) {
	setupTestDB(t)

	commits := []Commit{
		{
			Hash:      "abc123456789",
			RepoURL:   "https://github.com/user/repo",
			Branch:    "", // empty should default to "main"
			Message:   "Commit with empty branch",
			Timestamp: time.Now().UTC(),
		},
	}

	if err := InsertCommits(commits); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}

	meta, err := GetRepositoryFetchMeta("https://github.com/user/repo")
	if err != nil {
		t.Fatalf("GetRepositoryFetchMeta() error = %v", err)
	}
	if meta.CommitCount != 1 {
		t.Errorf("CommitCount = %d, want 1", meta.CommitCount)
	}
}

func TestInsertCommits_duplicateIgnored(t *testing.T) {
	setupTestDB(t)

	commit := Commit{
		Hash:      "abc123456789",
		RepoURL:   "https://github.com/user/repo",
		Branch:    "main",
		Message:   "Original message",
		Timestamp: time.Now().UTC(),
	}

	InsertCommits([]Commit{commit})
	commit.Message = "Updated message"
	InsertCommits([]Commit{commit}) // should be ignored (INSERT OR IGNORE)

	meta, _ := GetRepositoryFetchMeta("https://github.com/user/repo")
	if meta.CommitCount != 1 {
		t.Errorf("Duplicate insert should be ignored, got count = %d", meta.CommitCount)
	}
}

func TestInsertCommits_notOpen(t *testing.T) {
	Reset()
	err := InsertCommits([]Commit{{Hash: "abc", RepoURL: "url", Message: "msg", Timestamp: time.Now()}})
	if err != ErrNotOpen {
		t.Errorf("InsertCommits() error = %v, want ErrNotOpen", err)
	}
}

func TestGetContributors(t *testing.T) {
	setupTestDB(t)

	commits := []Commit{
		{Hash: "aaa111222333", RepoURL: "https://github.com/user/repo", Branch: "main", AuthorName: "Alice", AuthorEmail: "alice@example.com", Message: "m1", Timestamp: time.Date(2025, 10, 20, 12, 0, 0, 0, time.UTC)},
		{Hash: "bbb111222333", RepoURL: "https://github.com/user/repo", Branch: "main", AuthorName: "Bob", AuthorEmail: "bob@example.com", Message: "m2", Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC)},
	}
	InsertCommits(commits)

	contributors, err := GetContributors("https://github.com/user/repo")
	if err != nil {
		t.Fatalf("GetContributors() error = %v", err)
	}
	if len(contributors) != 2 {
		t.Fatalf("len(contributors) = %d, want 2", len(contributors))
	}
	// Bob should be first (most recent)
	if contributors[0].Name != "Bob" {
		t.Errorf("contributors[0].Name = %q, want Bob", contributors[0].Name)
	}
}

func TestGetAllContributors(t *testing.T) {
	setupTestDB(t)

	commits := []Commit{
		{Hash: "aaa111222333", RepoURL: "https://github.com/user/repo1", Branch: "main", AuthorName: "Alice", AuthorEmail: "alice@example.com", Message: "m1", Timestamp: time.Date(2025, 10, 20, 12, 0, 0, 0, time.UTC)},
		{Hash: "bbb111222333", RepoURL: "https://github.com/user/repo2", Branch: "main", AuthorName: "Bob", AuthorEmail: "bob@example.com", Message: "m2", Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC)},
	}
	InsertCommits(commits)

	contributors, err := GetAllContributors()
	if err != nil {
		t.Fatalf("GetAllContributors() error = %v", err)
	}
	if len(contributors) != 2 {
		t.Errorf("len(contributors) = %d, want 2", len(contributors))
	}
}

func TestFilterUnfetchedCommits(t *testing.T) {
	setupTestDB(t)

	InsertCommits([]Commit{
		{Hash: "aaa111222333", RepoURL: "https://github.com/user/repo", Branch: "main", Message: "m1", Timestamp: time.Now().UTC()},
	})

	unfetched, err := FilterUnfetchedCommits("https://github.com/user/repo", "main", []string{"aaa111222333", "bbb222333444"})
	if err != nil {
		t.Fatalf("FilterUnfetchedCommits() error = %v", err)
	}
	if len(unfetched) != 1 {
		t.Fatalf("len(unfetched) = %d, want 1", len(unfetched))
	}
	if unfetched[0] != "bbb222333444" {
		t.Errorf("unfetched[0] = %q, want %q", unfetched[0], "bbb222333444")
	}
}

func TestFilterUnfetchedCommits_empty(t *testing.T) {
	setupTestDB(t)
	unfetched, err := FilterUnfetchedCommits("https://github.com/user/repo", "main", nil)
	if err != nil {
		t.Fatalf("FilterUnfetchedCommits() error = %v", err)
	}
	if unfetched != nil {
		t.Errorf("Expected nil for empty input, got %v", unfetched)
	}
}

func TestFilterUnfetchedCommits_notOpen(t *testing.T) {
	Reset()
	_, err := FilterUnfetchedCommits("https://github.com/user/repo", "main", []string{"abc"})
	if err != ErrNotOpen {
		t.Errorf("FilterUnfetchedCommits() error = %v, want ErrNotOpen", err)
	}
}

func TestGetContributors_notOpen(t *testing.T) {
	Reset()
	_, err := GetContributors("https://github.com/user/repo")
	if err != ErrNotOpen {
		t.Errorf("GetContributors() error = %v, want ErrNotOpen", err)
	}
}

func TestGetAllContributors_notOpen(t *testing.T) {
	Reset()
	_, err := GetAllContributors()
	if err != ErrNotOpen {
		t.Errorf("GetAllContributors() error = %v, want ErrNotOpen", err)
	}
}

func TestInsertCommits_withEditsAndCanonical(t *testing.T) {
	setupTestDB(t)

	// Insert canonical commit first
	InsertCommits([]Commit{
		{Hash: "aabb00112233", RepoURL: "https://github.com/user/repo", Branch: "main",
			Message: "Original\n\nGitMsg: ext=\"social\"; type=\"post\"; v=\"0.1.0\"", Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC)},
	})

	// Insert edit with workspace-relative ref (no repo URL, no branch) to cover both fallbacks
	InsertCommits([]Commit{
		{Hash: "ccdd44556677", RepoURL: "https://github.com/user/repo", Branch: "main",
			Message:   "Edited\n\nGitMsg: ext=\"social\"; type=\"post\"; edits=\"#commit:aabb00112233\"; v=\"0.1.0\"",
			Timestamp: time.Date(2025, 10, 21, 13, 0, 0, 0, time.UTC)},
	})

	// Verify version record was created
	has, err := HasEdits("https://github.com/user/repo", "aabb00112233", "main")
	if err != nil {
		t.Fatalf("HasEdits() error = %v", err)
	}
	if !has {
		t.Error("canonical should have edits after InsertCommits with edits field")
	}
}

func TestInsertCommits_withEditsCanonicalMissing(t *testing.T) {
	setupTestDB(t)

	// Insert edit commit with edits ref pointing to nonexistent canonical
	// The version record should NOT be created
	InsertCommits([]Commit{
		{Hash: "ccdd44556677", RepoURL: "https://github.com/user/repo", Branch: "main",
			Message:   "Edited\n\nGitMsg: ext=\"social\"; type=\"post\"; edits=\"#commit:aabb00112233\"; v=\"0.1.0\"",
			Timestamp: time.Date(2025, 10, 21, 13, 0, 0, 0, time.UTC)},
	})

	// No version record should exist since canonical doesn't exist
	has, _ := HasEdits("https://github.com/user/repo", "aabb00112233", "main")
	if has {
		t.Error("should not have edits when canonical doesn't exist in cache")
	}
}

func TestInsertCommits_withNonGitMsgMessage(t *testing.T) {
	setupTestDB(t)

	// A plain commit without GitMsg header — no edits extraction
	InsertCommits([]Commit{
		{Hash: "plain1234567", RepoURL: "https://github.com/user/repo", Branch: "main",
			Message: "Just a plain commit message without any header", Timestamp: time.Now().UTC()},
	})

	meta, _ := GetRepositoryFetchMeta("https://github.com/user/repo")
	if meta.CommitCount != 1 {
		t.Errorf("CommitCount = %d, want 1", meta.CommitCount)
	}
}

func TestFilterUnfetchedCommits_allFetched(t *testing.T) {
	setupTestDB(t)

	InsertCommits([]Commit{
		{Hash: "aaa111222333", RepoURL: "https://github.com/user/repo", Branch: "main", Message: "m1", Timestamp: time.Now().UTC()},
		{Hash: "bbb222333444", RepoURL: "https://github.com/user/repo", Branch: "main", Message: "m2", Timestamp: time.Now().UTC()},
	})

	unfetched, err := FilterUnfetchedCommits("https://github.com/user/repo", "main", []string{"aaa111222333", "bbb222333444"})
	if err != nil {
		t.Fatalf("FilterUnfetchedCommits() error = %v", err)
	}
	if len(unfetched) != 0 {
		t.Errorf("len(unfetched) = %d, want 0", len(unfetched))
	}
}

func TestGetContributors_queryError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_commits"); return err })
	_, err := GetContributors("https://github.com/user/repo")
	if err == nil {
		t.Error("GetContributors() should fail when table is dropped")
	}
}

func TestGetAllContributors_queryError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_commits"); return err })
	_, err := GetAllContributors()
	if err == nil {
		t.Error("GetAllContributors() should fail when table is dropped")
	}
}

func TestFilterUnfetchedCommits_queryError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_commits"); return err })
	_, err := FilterUnfetchedCommits("https://github.com/user/repo", "main", []string{"abc"})
	if err == nil {
		t.Error("FilterUnfetchedCommits() should fail when table is dropped")
	}
}

func TestInsertCommits_prepareError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_commits"); return err })
	err := InsertCommits([]Commit{{Hash: "abc", RepoURL: "url", Message: "msg", Timestamp: time.Now()}})
	if err == nil {
		t.Error("InsertCommits() should fail when table is dropped")
	}
}

func TestGetRepositoryFetchMeta_queryError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_commits"); return err })
	_, err := GetRepositoryFetchMeta("https://github.com/user/repo")
	if err == nil {
		t.Error("GetRepositoryFetchMeta() should fail when table is dropped")
	}
}

func TestInsertCommits_withEditsRetracted(t *testing.T) {
	setupTestDB(t)

	// Insert canonical commit first
	InsertCommits([]Commit{
		{Hash: "aabb00112233", RepoURL: "https://github.com/user/repo", Branch: "main",
			Message: "Original\n\nGitMsg: ext=\"social\"; type=\"post\"; v=\"0.1.0\"", Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC)},
	})

	// Insert retracted edit
	InsertCommits([]Commit{
		{Hash: "eeff88990011", RepoURL: "https://github.com/user/repo", Branch: "main",
			Message:   "Retracted\n\nGitMsg: ext=\"social\"; type=\"post\"; edits=\"#commit:aabb00112233\"; retracted=\"true\"; v=\"0.1.0\"",
			Timestamp: time.Date(2025, 10, 21, 14, 0, 0, 0, time.UTC)},
	})

	// Verify version was created with retracted flag
	v, err := GetCanonical("https://github.com/user/repo", "eeff88990011", "main")
	if err != nil {
		t.Fatalf("GetCanonical() error = %v", err)
	}
	if v == nil {
		t.Fatal("GetCanonical() returned nil, expected version record")
	}
	if !v.IsRetracted {
		t.Error("IsRetracted should be true")
	}
}

// staleByBranch returns, per branch, whether the row of a hash in the test repository is stale.
func staleByBranch(t *testing.T, hash string) map[string]bool {
	t.Helper()
	repoURL := "https://github.com/user/repo"
	stale, err := QueryLocked(func(db *sql.DB) (map[string]bool, error) {
		rows, err := db.Query("SELECT branch, stale_since IS NOT NULL FROM core_commits WHERE repo_url = ? AND hash = ?", repoURL, hash)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := make(map[string]bool)
		for rows.Next() {
			var branch string
			var isStale bool
			if err := rows.Scan(&branch, &isStale); err != nil {
				return nil, err
			}
			out[branch] = isStale
		}
		return out, rows.Err()
	})
	if err != nil {
		t.Fatalf("query stale rows: %v", err)
	}
	return stale
}

func TestMarkCommitsStaleByHome_rowIsLiveOnlyUnderItsHome(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/user/repo"
	now := time.Now()
	if err := InsertCommits([]Commit{
		{Hash: "aaa111111111", RepoURL: repoURL, Branch: "feature/x", Message: "moved", Timestamp: now},
		{Hash: "aaa111111111", RepoURL: repoURL, Branch: "main", Message: "moved", Timestamp: now},
		{Hash: "bbb222222222", RepoURL: repoURL, Branch: "main", Message: "gone", Timestamp: now},
	}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	staled, err := MarkCommitsStaleByHome(repoURL, map[string]string{"aaa111111111": "main"}, nil)
	if err != nil {
		t.Fatalf("MarkCommitsStaleByHome() error = %v", err)
	}
	if staled != 2 {
		t.Errorf("staled = %d, want 2", staled)
	}
	moved := staleByBranch(t, "aaa111111111")
	if moved["main"] || !moved["feature/x"] {
		t.Errorf("stale by branch = %v, want main live and feature/x stale", moved)
	}
	if gone := staleByBranch(t, "bbb222222222"); !gone["main"] {
		t.Errorf("a commit with no home is not stale: %v", gone)
	}
	// The home moves back: the old row is live again and the other goes stale.
	if _, err := MarkCommitsStaleByHome(repoURL, map[string]string{"aaa111111111": "feature/x"}, nil); err != nil {
		t.Fatalf("MarkCommitsStaleByHome() error = %v", err)
	}
	back := staleByBranch(t, "aaa111111111")
	if !back["main"] || back["feature/x"] {
		t.Errorf("stale by branch = %v, want feature/x live and main stale", back)
	}
}

func TestMarkCommitsStaleByHome_readMarkerFollowsTheCommit(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/user/repo"
	now := time.Now()
	if err := InsertCommits([]Commit{
		{Hash: "aaa111111111", RepoURL: repoURL, Branch: "feature/x", Message: "moved", Timestamp: now},
		{Hash: "aaa111111111", RepoURL: repoURL, Branch: "main", Message: "moved", Timestamp: now},
	}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	if err := ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec("INSERT INTO core_notification_reads (repo_url, hash, branch, read_at) VALUES (?, ?, ?, ?)",
			repoURL, "aaa111111111", "feature/x", "2026-01-01T00:00:00Z")
		return err
	}); err != nil {
		t.Fatalf("insert read marker: %v", err)
	}
	if _, err := MarkCommitsStaleByHome(repoURL, map[string]string{"aaa111111111": "main"}, nil); err != nil {
		t.Fatalf("MarkCommitsStaleByHome() error = %v", err)
	}
	read, err := QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow("SELECT COUNT(*) FROM core_notification_reads WHERE repo_url = ? AND hash = ? AND branch = ?",
			repoURL, "aaa111111111", "main").Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("query read marker: %v", err)
	}
	if read != 1 {
		t.Errorf("read markers under the new home = %d, want 1", read)
	}
}

func TestGetCommitOnAnyBranch_prefersLive(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/user/repo"
	now := time.Now()
	if err := InsertCommits([]Commit{
		{Hash: "aaa111111111", RepoURL: repoURL, Branch: "a-feature", Message: "moved", Timestamp: now},
		{Hash: "aaa111111111", RepoURL: repoURL, Branch: "main", Message: "moved", Timestamp: now},
	}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	if _, err := MarkCommitsStaleByHome(repoURL, map[string]string{"aaa111111111": "main"}, nil); err != nil {
		t.Fatalf("MarkCommitsStaleByHome() error = %v", err)
	}
	got, err := GetCommitOnAnyBranch(repoURL, "aaa111111111")
	if err != nil {
		t.Fatalf("GetCommitOnAnyBranch() error = %v", err)
	}
	if got.Branch != "main" {
		t.Errorf("branch = %q, want the live row under main", got.Branch)
	}
}

func TestMarkCommitsStaleByHome_settledBranchIsNotRead(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/user/repo"
	now := time.Now()
	if err := InsertCommits([]Commit{
		{Hash: "aaa111111111", RepoURL: repoURL, Branch: "main", Message: "old history", Timestamp: now},
		{Hash: "bbb222222222", RepoURL: repoURL, Branch: "feature/x", Message: "gone", Timestamp: now},
	}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	if _, err := MarkCommitsStaleByHome(repoURL, map[string]string{}, []string{"main"}); err != nil {
		t.Fatalf("MarkCommitsStaleByHome() error = %v", err)
	}
	if staleByBranch(t, "aaa111111111")["main"] {
		t.Error("a row of a settled branch went stale")
	}
	if !staleByBranch(t, "bbb222222222")["feature/x"] {
		t.Error("a row with no home under a branch that is not settled stayed live")
	}
}

// TestUpsertVirtualCommit_skipsWhenRowExists pins invariant 9: no virtual row is written for a hash that has a row in the repository.
func TestUpsertVirtualCommit_skipsWhenRowExists(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/user/repo"
	if err := InsertCommits([]Commit{{Hash: "aaa111111111", RepoURL: repoURL, Branch: "main", Message: "real", Timestamp: time.Now()}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	cases := []struct {
		hash, branch string
		want         bool
	}{
		{"aaa111111111", "feature/x", false},
		{"aaa111111111", "main", false},
		{"bbb222222222", "feature/x", true},
		{"bbb222222222", "main", false},
	}
	for _, tc := range cases {
		var inserted bool
		err := ExecLocked(func(db *sql.DB) error {
			var err error
			inserted, err = UpsertVirtualCommit(db, VirtualCommit{RepoURL: repoURL, Hash: tc.hash, Branch: tc.branch, Message: "snapshot", Timestamp: time.Now()})
			return err
		})
		if err != nil {
			t.Fatalf("UpsertVirtualCommit(%s@%s) error = %v", tc.hash, tc.branch, err)
		}
		if inserted != tc.want {
			t.Errorf("UpsertVirtualCommit(%s@%s) = %v, want %v", tc.hash, tc.branch, inserted, tc.want)
		}
	}
	if rows := staleByBranch(t, "aaa111111111"); len(rows) != 1 {
		t.Errorf("rows of a fetched hash = %v, want only the fetched row", rows)
	}
}

// TestDetectExtension_liveFirst pins invariant 12: the live, fetched row of a hash is the first hit, by full hash or by prefix.
func TestDetectExtension_liveFirst(t *testing.T) {
	schemaMu.Lock()
	extensionSchemas["memo"] = `CREATE TABLE IF NOT EXISTS memo_items (repo_url TEXT NOT NULL, hash TEXT NOT NULL, branch TEXT NOT NULL, type TEXT, PRIMARY KEY (repo_url, hash, branch));`
	schemaMu.Unlock()
	t.Cleanup(func() {
		schemaMu.Lock()
		delete(extensionSchemas, "memo")
		schemaMu.Unlock()
	})
	setupTestDBWithAllSchemas(t)
	repoURL := "https://github.com/user/repo"
	const hash = "abcdef123456"
	if err := ExecLocked(func(db *sql.DB) error {
		for _, row := range []struct {
			branch     string
			virtual    int
			staleSince interface{}
		}{
			{"a-virtual", 1, nil},
			{"b-stale", 0, "2025-10-21T00:00:00Z"},
			{"main", 0, nil},
		} {
			if _, err := db.Exec(`INSERT INTO core_commits (repo_url, hash, branch, message, timestamp, is_virtual, stale_since) VALUES (?, ?, ?, 'm', '2025-10-20T12:00:00Z', ?, ?)`,
				repoURL, hash, row.branch, row.virtual, row.staleSince); err != nil {
				return err
			}
			if _, err := db.Exec(`INSERT INTO social_items (repo_url, hash, branch, type) VALUES (?, ?, ?, 'post')`, repoURL, hash, row.branch); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed rows: %v", err)
	}
	for _, prefix := range []string{hash, "abcdef12", "ABCDEF12"} {
		hits, err := DetectExtension(prefix)
		if err != nil {
			t.Fatalf("DetectExtension(%s) error = %v", prefix, err)
		}
		if len(hits) != 3 || hits[0].Branch != "main" {
			t.Errorf("DetectExtension(%s) = %+v, want 3 hits with the live row under main first", prefix, hits)
		}
	}
}

// TestHashPrefixMatch pins invariant 4: a full hash matches by equality, a prefix by a range, both in lowercase.
func TestHashPrefixMatch(t *testing.T) {
	cond, args := HashPrefixMatch("hash", "ABCDEF123456")
	if cond != "hash = ?" || len(args) != 1 || args[0] != "abcdef123456" {
		t.Errorf("HashPrefixMatch(full) = %q %v, want an equality on the lowercase hash", cond, args)
	}
	cond, args = HashPrefixMatch("c.hash", "AbC")
	if cond != "c.hash >= ? AND c.hash < ?" || len(args) != 2 || args[0] != "abc" || args[1] != "abcg" {
		t.Errorf("HashPrefixMatch(prefix) = %q %v, want a range from abc to abcg", cond, args)
	}
	if cond, args = HashPrefixMatch("hash", ""); cond != "hash = ?" || len(args) != 1 || args[0] != "" {
		t.Errorf("HashPrefixMatch(empty) = %q %v, want a term that matches no hash", cond, args)
	}
}
