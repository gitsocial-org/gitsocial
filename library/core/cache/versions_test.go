// versions_test.go - Tests for message versioning and edit tracking
package cache

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/protocol"
)

func insertCanonicalAndEdit(t *testing.T) {
	t.Helper()
	// Insert canonical commit
	InsertCommits([]Commit{
		{
			Hash:      "canonical1234",
			RepoURL:   "https://github.com/user/repo",
			Branch:    "main",
			Message:   "Original post\n\nGitMsg: ext=\"social\"; type=\"post\"; v=\"0.1.0\"",
			Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
		},
	})
	// Insert edit commit
	InsertCommits([]Commit{
		{
			Hash:      "edit12345678",
			RepoURL:   "https://github.com/user/repo",
			Branch:    "main",
			Message:   "Edited post\n\nGitMsg: ext=\"social\"; type=\"post\"; edits=\"#commit:canonical1234@main\"; v=\"0.1.0\"",
			Timestamp: time.Date(2025, 10, 21, 13, 0, 0, 0, time.UTC),
		},
	})
}

func TestInsertVersion(t *testing.T) {
	setupTestDB(t)

	// First insert the canonical commit
	InsertCommits([]Commit{
		{Hash: "canonical1234", RepoURL: "https://github.com/user/repo", Branch: "main", Message: "Original", Timestamp: time.Now().UTC()},
		{Hash: "edit12345678", RepoURL: "https://github.com/user/repo", Branch: "main", Message: "Edit", Timestamp: time.Now().UTC()},
	})

	err := InsertVersion("https://github.com/user/repo", "edit12345678", "main",
		"https://github.com/user/repo", "canonical1234", "main", false)
	if err != nil {
		t.Fatalf("InsertVersion() error = %v", err)
	}
}

func TestInsertVersion_canonicalNotFound(t *testing.T) {
	setupTestDB(t)

	err := InsertVersion("https://github.com/user/repo", "edit12345678", "main",
		"https://github.com/user/repo", "nonexistent12", "main", false)
	if err == nil {
		t.Error("InsertVersion() should fail when canonical doesn't exist")
	}
}

func TestGetLatestVersion_noEdits(t *testing.T) {
	setupTestDB(t)

	InsertCommits([]Commit{
		{Hash: "canonical1234", RepoURL: "https://github.com/user/repo", Branch: "main", Message: "Original", Timestamp: time.Now().UTC()},
	})

	result, err := GetLatestVersion("https://github.com/user/repo", "canonical1234", "main")
	if err != nil {
		t.Fatalf("GetLatestVersion() error = %v", err)
	}
	if result.HasEdits {
		t.Error("HasEdits should be false when no edits exist")
	}
	if result.Hash != "canonical1234" {
		t.Errorf("Hash = %q, want canonical", result.Hash)
	}
}

func TestGetLatestVersion_withEdit(t *testing.T) {
	setupTestDB(t)
	insertCanonicalAndEdit(t)

	// Manually insert version since auto-insert depends on canonical existing at commit time
	InsertVersion("https://github.com/user/repo", "edit12345678", "main",
		"https://github.com/user/repo", "canonical1234", "main", false)

	result, err := GetLatestVersion("https://github.com/user/repo", "canonical1234", "main")
	if err != nil {
		t.Fatalf("GetLatestVersion() error = %v", err)
	}
	if !result.HasEdits {
		t.Error("HasEdits should be true")
	}
	if result.Hash != "edit12345678" {
		t.Errorf("Hash = %q, want edit12345678", result.Hash)
	}
}

func TestHasEdits(t *testing.T) {
	setupTestDB(t)
	insertCanonicalAndEdit(t)

	InsertVersion("https://github.com/user/repo", "edit12345678", "main",
		"https://github.com/user/repo", "canonical1234", "main", false)

	has, err := HasEdits("https://github.com/user/repo", "canonical1234", "main")
	if err != nil {
		t.Fatalf("HasEdits() error = %v", err)
	}
	if !has {
		t.Error("HasEdits should be true")
	}

	has, _ = HasEdits("https://github.com/user/repo", "nonexistent12", "main")
	if has {
		t.Error("HasEdits should be false for nonexistent commit")
	}
}

func TestIsEdit(t *testing.T) {
	setupTestDB(t)
	insertCanonicalAndEdit(t)

	InsertVersion("https://github.com/user/repo", "edit12345678", "main",
		"https://github.com/user/repo", "canonical1234", "main", false)

	isEdit, err := IsEdit("https://github.com/user/repo", "edit12345678", "main")
	if err != nil {
		t.Fatalf("IsEdit() error = %v", err)
	}
	if !isEdit {
		t.Error("IsEdit should be true for edit commit")
	}

	isEdit, _ = IsEdit("https://github.com/user/repo", "canonical1234", "main")
	if isEdit {
		t.Error("IsEdit should be false for canonical commit")
	}
}

func TestGetCanonical(t *testing.T) {
	setupTestDB(t)
	insertCanonicalAndEdit(t)

	InsertVersion("https://github.com/user/repo", "edit12345678", "main",
		"https://github.com/user/repo", "canonical1234", "main", false)

	v, err := GetCanonical("https://github.com/user/repo", "edit12345678", "main")
	if err != nil {
		t.Fatalf("GetCanonical() error = %v", err)
	}
	if v == nil {
		t.Fatal("GetCanonical() returned nil")
	}
	if v.CanonicalHash != "canonical1234" {
		t.Errorf("CanonicalHash = %q, want canonical1234", v.CanonicalHash)
	}
}

func TestGetCanonical_notAnEdit(t *testing.T) {
	setupTestDB(t)
	InsertCommits([]Commit{
		{Hash: "canonical1234", RepoURL: "https://github.com/user/repo", Branch: "main", Message: "Original", Timestamp: time.Now().UTC()},
	})

	v, err := GetCanonical("https://github.com/user/repo", "canonical1234", "main")
	if err != nil {
		t.Fatalf("GetCanonical() error = %v", err)
	}
	if v != nil {
		t.Error("GetCanonical() should return nil for canonical commit")
	}
}

func TestResolveToCanonical(t *testing.T) {
	setupTestDB(t)
	insertCanonicalAndEdit(t)

	InsertVersion("https://github.com/user/repo", "edit12345678", "main",
		"https://github.com/user/repo", "canonical1234", "main", false)

	repo, hash, branch, err := ResolveToCanonical("https://github.com/user/repo", "edit12345678", "main")
	if err != nil {
		t.Fatalf("ResolveToCanonical() error = %v", err)
	}
	if hash != "canonical1234" {
		t.Errorf("hash = %q, want canonical1234", hash)
	}
	if repo != "https://github.com/user/repo" {
		t.Errorf("repo = %q", repo)
	}
	if branch != "main" {
		t.Errorf("branch = %q", branch)
	}
}

func TestResolveToCanonical_alreadyCanonical(t *testing.T) {
	setupTestDB(t)
	InsertCommits([]Commit{
		{Hash: "canonical1234", RepoURL: "https://github.com/user/repo", Branch: "main", Message: "Original", Timestamp: time.Now().UTC()},
	})

	_, hash, _, err := ResolveToCanonical("https://github.com/user/repo", "canonical1234", "main")
	if err != nil {
		t.Fatalf("ResolveToCanonical() error = %v", err)
	}
	if hash != "canonical1234" {
		t.Errorf("hash = %q, want canonical1234 (unchanged)", hash)
	}
}

func TestGetVersionHistory(t *testing.T) {
	setupTestDB(t)
	insertCanonicalAndEdit(t)

	InsertVersion("https://github.com/user/repo", "edit12345678", "main",
		"https://github.com/user/repo", "canonical1234", "main", false)

	versions, err := GetVersionHistory("https://github.com/user/repo", "canonical1234", "main")
	if err != nil {
		t.Fatalf("GetVersionHistory() error = %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("len(versions) = %d, want 1", len(versions))
	}
	if versions[0].EditHash != "edit12345678" {
		t.Errorf("EditHash = %q", versions[0].EditHash)
	}
}

func TestGetLatestContent(t *testing.T) {
	setupTestDB(t)
	insertCanonicalAndEdit(t)

	InsertVersion("https://github.com/user/repo", "edit12345678", "main",
		"https://github.com/user/repo", "canonical1234", "main", false)

	msg, hasEdits, err := GetLatestContent("https://github.com/user/repo", "canonical1234", "main")
	if err != nil {
		t.Fatalf("GetLatestContent() error = %v", err)
	}
	if !hasEdits {
		t.Error("hasEdits should be true")
	}
	if msg == "" {
		t.Error("msg should not be empty")
	}
}

func TestGetLatestContent_noEdits(t *testing.T) {
	setupTestDB(t)
	InsertCommits([]Commit{
		{Hash: "canonical1234", RepoURL: "https://github.com/user/repo", Branch: "main", Message: "Original content", Timestamp: time.Now().UTC()},
	})

	msg, hasEdits, err := GetLatestContent("https://github.com/user/repo", "canonical1234", "main")
	if err != nil {
		t.Fatalf("GetLatestContent() error = %v", err)
	}
	if hasEdits {
		t.Error("hasEdits should be false")
	}
	if msg != "Original content" {
		t.Errorf("msg = %q, want %q", msg, "Original content")
	}
}

func TestResolveRefToCanonical(t *testing.T) {
	setupTestDB(t)

	// Use hex-only hashes since ParseRef validates with [a-f0-9]+
	InsertCommits([]Commit{
		{Hash: "aabbccdd1234", RepoURL: "https://github.com/user/repo", Branch: "main",
			Message: "Original", Timestamp: time.Now().UTC()},
		{Hash: "eeff00112233", RepoURL: "https://github.com/user/repo", Branch: "main",
			Message: "Edit", Timestamp: time.Now().UTC()},
	})
	InsertVersion("https://github.com/user/repo", "eeff00112233", "main",
		"https://github.com/user/repo", "aabbccdd1234", "main", false)

	// Test with edit ref → should resolve to canonical
	editRef := protocol.CreateRef(protocol.RefTypeCommit, "eeff00112233", "https://github.com/user/repo", "main")
	resolved := ResolveRefToCanonical(editRef)
	parsed := protocol.ParseRef(resolved)
	if parsed.Value != "aabbccdd1234" {
		t.Errorf("resolved hash = %q, want aabbccdd1234", parsed.Value)
	}

	// Test with canonical ref → should return unchanged
	canonicalRef := protocol.CreateRef(protocol.RefTypeCommit, "aabbccdd1234", "https://github.com/user/repo", "main")
	resolved = ResolveRefToCanonical(canonicalRef)
	parsed = protocol.ParseRef(resolved)
	if parsed.Value != "aabbccdd1234" {
		t.Errorf("canonical ref should remain unchanged, got %q", parsed.Value)
	}

	// Test with empty value ref → should return original
	resolved = ResolveRefToCanonical("")
	if resolved != "" {
		t.Errorf("empty ref should return empty, got %q", resolved)
	}
}

func TestReconcileVersions(t *testing.T) {
	setupTestDB(t)

	// Use hex-only hashes for ParseRef compatibility
	editsRef := protocol.CreateRef(protocol.RefTypeCommit, "aabb00112233", "", "main")

	// Insert edit commit first (before canonical) so InsertCommits can't link them
	InsertCommits([]Commit{
		{Hash: "ccdd44556677", RepoURL: "https://github.com/user/repo", Branch: "main",
			Message:   "Edited\n\nGitMsg: ext=\"social\"; type=\"post\"; edits=\"" + editsRef + "\"; v=\"0.1.0\"",
			Timestamp: time.Now().UTC()},
	})

	// Now insert canonical commit
	InsertCommits([]Commit{
		{Hash: "aabb00112233", RepoURL: "https://github.com/user/repo", Branch: "main",
			Message: "Original\n\nGitMsg: ext=\"social\"; type=\"post\"; v=\"0.1.0\"", Timestamp: time.Now().UTC()},
	})

	// ReconcileVersions should find the edit and create version record
	created, err := ReconcileVersions()
	if err != nil {
		t.Fatalf("ReconcileVersions() error = %v", err)
	}
	if created != 1 {
		t.Errorf("ReconcileVersions() created = %d, want 1", created)
	}

	// Verify version was created
	has, _ := HasEdits("https://github.com/user/repo", "aabb00112233", "main")
	if !has {
		t.Error("canonical should now have edits")
	}
}

func TestInsertVersion_queryError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_commits"); return err })
	err := InsertVersion("url", "edit", "main", "url", "canonical", "main", false)
	if err == nil {
		t.Error("InsertVersion() should fail when core_commits is dropped")
	}
}

func TestGetLatestVersion_queryError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_commits_version"); return err })
	_, err := GetLatestVersion("url", "hash", "main")
	if err == nil {
		t.Error("GetLatestVersion() should fail when table is dropped")
	}
}

func TestGetVersionHistory_queryError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_commits_version"); return err })
	_, err := GetVersionHistory("url", "hash", "main")
	if err == nil {
		t.Error("GetVersionHistory() should fail when table is dropped")
	}
}

func TestGetCanonical_queryError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_commits_version"); return err })
	_, err := GetCanonical("url", "hash", "main")
	if err == nil {
		t.Error("GetCanonical() should fail when table is dropped")
	}
}

func TestResolveToCanonical_queryError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_commits_version"); return err })
	_, _, _, err := ResolveToCanonical("url", "hash", "main")
	if err == nil {
		t.Error("ResolveToCanonical() should fail when table is dropped")
	}
}

func TestGetLatestContent_firstQueryError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_commits_version"); return err })
	_, _, err := GetLatestContent("url", "hash", "main")
	if err == nil {
		t.Error("GetLatestContent() should fail when version table is dropped")
	}
}

func TestGetLatestContent_secondQueryError(t *testing.T) {
	setupTestDB(t)
	insertCanonicalAndEdit(t)
	InsertVersion("https://github.com/user/repo", "edit12345678", "main",
		"https://github.com/user/repo", "canonical1234", "main", false)

	// Drop core_commits so the JOIN in the second query fails
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_commits"); return err })
	_, _, err := GetLatestContent("https://github.com/user/repo", "canonical1234", "main")
	if err == nil {
		t.Error("GetLatestContent() should fail when core_commits is dropped")
	}
}

func TestReconcileVersions_queryError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_commits"); return err })
	_, err := ReconcileVersions()
	if err == nil {
		t.Error("ReconcileVersions() should fail when core_commits is dropped")
	}
}

func TestInsertVersion_retracted(t *testing.T) {
	setupTestDB(t)

	InsertCommits([]Commit{
		{Hash: "canonical1234", RepoURL: "https://github.com/user/repo", Branch: "main", Message: "Original", Timestamp: time.Now().UTC()},
		{Hash: "edit12345678", RepoURL: "https://github.com/user/repo", Branch: "main", Message: "Retracted", Timestamp: time.Now().UTC()},
	})

	err := InsertVersion("https://github.com/user/repo", "edit12345678", "main",
		"https://github.com/user/repo", "canonical1234", "main", true)
	if err != nil {
		t.Fatalf("InsertVersion() error = %v", err)
	}

	v, _ := GetCanonical("https://github.com/user/repo", "edit12345678", "main")
	if v == nil {
		t.Fatal("version should exist")
	}
	if !v.IsRetracted {
		t.Error("IsRetracted should be true")
	}
}

func TestInsertVersion_notOpen(t *testing.T) {
	Reset()
	err := InsertVersion("url", "edit", "main", "url", "canonical", "main", false)
	if err != ErrNotOpen {
		t.Errorf("InsertVersion() error = %v, want ErrNotOpen", err)
	}
}

func TestGetLatestVersion_notOpen(t *testing.T) {
	Reset()
	_, err := GetLatestVersion("url", "hash", "main")
	if err != ErrNotOpen {
		t.Errorf("GetLatestVersion() error = %v, want ErrNotOpen", err)
	}
}

func TestGetVersionHistory_notOpen(t *testing.T) {
	Reset()
	_, err := GetVersionHistory("url", "hash", "main")
	if err != ErrNotOpen {
		t.Errorf("GetVersionHistory() error = %v, want ErrNotOpen", err)
	}
}

func TestGetCanonical_notOpen(t *testing.T) {
	Reset()
	_, err := GetCanonical("url", "hash", "main")
	if err != ErrNotOpen {
		t.Errorf("GetCanonical() error = %v, want ErrNotOpen", err)
	}
}

func TestResolveToCanonical_notOpen(t *testing.T) {
	Reset()
	_, _, _, err := ResolveToCanonical("url", "hash", "main")
	if err != ErrNotOpen {
		t.Errorf("ResolveToCanonical() error = %v, want ErrNotOpen", err)
	}
}

func TestGetLatestContent_notOpen(t *testing.T) {
	Reset()
	_, _, err := GetLatestContent("url", "hash", "main")
	if err != ErrNotOpen {
		t.Errorf("GetLatestContent() error = %v, want ErrNotOpen", err)
	}
}

func TestReconcileVersions_notOpen(t *testing.T) {
	Reset()
	_, err := ReconcileVersions()
	if err != ErrNotOpen {
		t.Errorf("ReconcileVersions() error = %v, want ErrNotOpen", err)
	}
}

func TestResolveRefToCanonical_errorPath(t *testing.T) {
	Reset()
	// DB not open → ResolveToCanonical returns error → original ref returned
	ref := protocol.CreateRef(protocol.RefTypeCommit, "aabbccdd1234", "https://github.com/user/repo", "main")
	resolved := ResolveRefToCanonical(ref)
	if resolved != ref {
		t.Errorf("should return original ref on error, got %q", resolved)
	}
}

func TestResolveRefToCanonical_emptyCanonical(t *testing.T) {
	setupTestDB(t)

	// Workspace-relative ref with no repo URL and no branch
	// ResolveToCanonical returns ("", hash, "") since no version record exists
	// This exercises the canonicalRepoURL=="" and canonicalBranch=="" fallbacks
	ref := "#commit:aabbccdd1234"
	resolved := ResolveRefToCanonical(ref)
	parsed := protocol.ParseRef(resolved)
	if parsed.Value != "aabbccdd1234" {
		t.Errorf("hash = %q, want aabbccdd1234", parsed.Value)
	}
}

func TestReconcileVersions_withRetracted(t *testing.T) {
	setupTestDB(t)

	editsRef := protocol.CreateRef(protocol.RefTypeCommit, "aabb00112233", "", "main")

	// Insert retracted edit first (before canonical)
	InsertCommits([]Commit{
		{Hash: "ccdd44556677", RepoURL: "https://github.com/user/repo", Branch: "main",
			Message:   "Retracted\n\nGitMsg: ext=\"social\"; type=\"post\"; edits=\"" + editsRef + "\"; retracted=\"true\"; v=\"0.1.0\"",
			Timestamp: time.Now().UTC()},
	})

	// Now insert canonical
	InsertCommits([]Commit{
		{Hash: "aabb00112233", RepoURL: "https://github.com/user/repo", Branch: "main",
			Message: "Original\n\nGitMsg: ext=\"social\"; type=\"post\"; v=\"0.1.0\"", Timestamp: time.Now().UTC()},
	})

	created, err := ReconcileVersions()
	if err != nil {
		t.Fatalf("ReconcileVersions() error = %v", err)
	}
	if created != 1 {
		t.Errorf("created = %d, want 1", created)
	}

	// Verify retracted flag
	v, _ := GetCanonical("https://github.com/user/repo", "ccdd44556677", "main")
	if v == nil {
		t.Fatal("version should exist")
	}
	if !v.IsRetracted {
		t.Error("IsRetracted should be true")
	}
}

func TestGetLatestContent_fromEditRef(t *testing.T) {
	setupTestDB(t)
	insertCanonicalAndEdit(t)

	InsertVersion("https://github.com/user/repo", "edit12345678", "main",
		"https://github.com/user/repo", "canonical1234", "main", false)

	// Call with edit ref — should resolve to canonical first, then return latest edit content
	msg, hasEdits, err := GetLatestContent("https://github.com/user/repo", "edit12345678", "main")
	if err != nil {
		t.Fatalf("GetLatestContent() error = %v", err)
	}
	if !hasEdits {
		t.Error("hasEdits should be true")
	}
	if msg == "" {
		t.Error("msg should not be empty")
	}
}

func TestReconcileVersions_unparsableRef(t *testing.T) {
	setupTestDB(t)

	// Insert commit with edits field that doesn't parse as a valid ref (no hex hash)
	InsertCommits([]Commit{
		{Hash: "ccdd44556677", RepoURL: "https://github.com/user/repo", Branch: "main",
			Message:   "Edited\n\nGitMsg: ext=\"social\"; type=\"post\"; edits=\"not-a-valid-ref\"; v=\"0.1.0\"",
			Timestamp: time.Now().UTC()},
	})

	created, err := ReconcileVersions()
	if err != nil {
		t.Fatalf("ReconcileVersions() error = %v", err)
	}
	if created != 0 {
		t.Errorf("created = %d, want 0 (unparsable edits ref should be skipped)", created)
	}
}

func TestReconcileVersions_canonicalNotYetFetched(t *testing.T) {
	setupTestDB(t)

	editsRef := protocol.CreateRef(protocol.RefTypeCommit, "aabb00112233", "", "main")

	// Insert edit only — canonical does not exist
	InsertCommits([]Commit{
		{Hash: "ccdd44556677", RepoURL: "https://github.com/user/repo", Branch: "main",
			Message:   "Edited\n\nGitMsg: ext=\"social\"; type=\"post\"; edits=\"" + editsRef + "\"; v=\"0.1.0\"",
			Timestamp: time.Now().UTC()},
	})

	// Canonical not yet fetched — should skip without error
	created, err := ReconcileVersions()
	if err != nil {
		t.Fatalf("ReconcileVersions() error = %v", err)
	}
	if created != 0 {
		t.Errorf("created = %d, want 0 (canonical not yet fetched)", created)
	}
}

func TestGetVersionHistory_empty(t *testing.T) {
	setupTestDB(t)

	InsertCommits([]Commit{
		{Hash: "canonical1234", RepoURL: "https://github.com/user/repo", Branch: "main",
			Message: "Original", Timestamp: time.Now().UTC()},
	})

	versions, err := GetVersionHistory("https://github.com/user/repo", "canonical1234", "main")
	if err != nil {
		t.Fatalf("GetVersionHistory() error = %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("len(versions) = %d, want 0", len(versions))
	}
}

func TestReconcileVersions_noPending(t *testing.T) {
	setupTestDB(t)

	InsertCommits([]Commit{
		{Hash: "canonical1234", RepoURL: "https://github.com/user/repo", Branch: "main",
			Message: "Original", Timestamp: time.Now().UTC()},
	})

	created, err := ReconcileVersions()
	if err != nil {
		t.Fatalf("ReconcileVersions() error = %v", err)
	}
	if created != 0 {
		t.Errorf("ReconcileVersions() created = %d, want 0", created)
	}
}
