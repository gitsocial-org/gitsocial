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
