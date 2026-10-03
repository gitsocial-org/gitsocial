// versions_test.go - Tests for message versioning and edit tracking
package cache

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
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

// --- Gating: only same-repo edits are authoritative ---

// readResolved returns a canonical's denormalized resolved state.
func readResolved(t *testing.T, repoURL, hash, branch string) (hasEdits, isRetracted int, resolved sql.NullString) {
	t.Helper()
	type row struct {
		he, ir int
		msg    sql.NullString
	}
	out, err := QueryLocked(func(db *sql.DB) (row, error) {
		var x row
		e := db.QueryRow(`SELECT has_edits, is_retracted, resolved_message FROM core_commits
			WHERE repo_url = ? AND hash = ? AND branch = ?`, repoURL, hash, branch).Scan(&x.he, &x.ir, &x.msg)
		return x, e
	})
	if err != nil {
		t.Fatalf("readResolved: %v", err)
	}
	return out.he, out.ir, out.msg
}

func TestGetLatestVersion_crossRepoEditExcluded(t *testing.T) {
	setupTestDB(t)
	repoA := "https://github.com/alice/repo"
	repoB := "https://github.com/bob/repo"
	InsertCommits([]Commit{
		{Hash: "canon00000001", RepoURL: repoA, Branch: "main", Message: "Original", Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC)},
		{Hash: "editb0000001", RepoURL: repoB, Branch: "main", Message: "Fork edit", Timestamp: time.Date(2025, 10, 21, 13, 0, 0, 0, time.UTC)},
	})
	// Cross-repo proposal: authored on repoB, targets repoA's canonical.
	if err := InsertVersion(repoB, "editb0000001", "main", repoA, "canon00000001", "main", false); err != nil {
		t.Fatalf("InsertVersion() error = %v", err)
	}
	result, err := GetLatestVersion(repoA, "canon00000001", "main")
	if err != nil {
		t.Fatalf("GetLatestVersion() error = %v", err)
	}
	if result.HasEdits {
		t.Error("cross-repo edit must not be selected as the latest version (gating)")
	}
	if result.Hash != "canon00000001" {
		t.Errorf("Hash = %q, want canonical (cross-repo edit excluded)", result.Hash)
	}
}

func TestGetLatestVersion_sameRepoWinsOverNewerCrossRepo(t *testing.T) {
	setupTestDB(t)
	repoA := "https://github.com/alice/repo"
	repoB := "https://github.com/bob/repo"
	InsertCommits([]Commit{
		{Hash: "canon00000001", RepoURL: repoA, Branch: "main", Message: "Original", Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC)},
		{Hash: "edita0000001", RepoURL: repoA, Branch: "main", Message: "Owner edit", Timestamp: time.Date(2025, 10, 21, 13, 0, 0, 0, time.UTC)},
		{Hash: "editb0000001", RepoURL: repoB, Branch: "main", Message: "Fork edit", Timestamp: time.Date(2025, 10, 21, 14, 0, 0, 0, time.UTC)},
	})
	InsertVersion(repoA, "edita0000001", "main", repoA, "canon00000001", "main", false)
	// Newer cross-repo proposal must not win over the owner's same-repo edit.
	InsertVersion(repoB, "editb0000001", "main", repoA, "canon00000001", "main", false)

	result, err := GetLatestVersion(repoA, "canon00000001", "main")
	if err != nil {
		t.Fatalf("GetLatestVersion() error = %v", err)
	}
	if !result.HasEdits {
		t.Fatal("same-repo edit should be selected")
	}
	if result.Hash != "edita0000001" {
		t.Errorf("Hash = %q, want edita0000001 (same-repo wins over newer cross-repo)", result.Hash)
	}
}

func TestGetLatestContent_crossRepoEditExcluded(t *testing.T) {
	setupTestDB(t)
	repoA := "https://github.com/alice/repo"
	repoB := "https://github.com/bob/repo"
	InsertCommits([]Commit{
		{Hash: "canon00000001", RepoURL: repoA, Branch: "main", Message: "Original content", Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC)},
		{Hash: "editb0000001", RepoURL: repoB, Branch: "main", Message: "Fork edited content", Timestamp: time.Date(2025, 10, 21, 13, 0, 0, 0, time.UTC)},
	})
	InsertVersion(repoB, "editb0000001", "main", repoA, "canon00000001", "main", false)

	msg, hasEdits, err := GetLatestContent(repoA, "canon00000001", "main")
	if err != nil {
		t.Fatalf("GetLatestContent() error = %v", err)
	}
	if hasEdits {
		t.Error("cross-repo edit must not count as an edit (gating)")
	}
	if msg != "Original content" {
		t.Errorf("msg = %q, want %q (canonical content, cross-repo edit excluded)", msg, "Original content")
	}
}

func TestApplyEditToCanonical_gatingResolvedState(t *testing.T) {
	setupTestDB(t)
	repoA := "https://github.com/alice/repo"
	repoB := "https://github.com/bob/repo"
	InsertCommits([]Commit{
		{Hash: "canon00000001", RepoURL: repoA, Branch: "main", Message: "Original", Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC)},
		{Hash: "editb0000001", RepoURL: repoB, Branch: "main", Message: "Fork retraction", Timestamp: time.Date(2025, 10, 21, 13, 0, 0, 0, time.UTC)},
	})
	// Cross-repo retraction proposal must not touch the canonical's resolved state.
	InsertVersion(repoB, "editb0000001", "main", repoA, "canon00000001", "main", true)
	hasEdits, retracted, resolved := readResolved(t, repoA, "canon00000001", "main")
	if hasEdits != 0 || retracted != 0 || resolved.Valid {
		t.Errorf("cross-repo edit changed resolved state: has_edits=%d is_retracted=%d resolved=%v", hasEdits, retracted, resolved)
	}

	// A same-repo edit on the same canonical does apply.
	InsertCommits([]Commit{
		{Hash: "edita0000001", RepoURL: repoA, Branch: "main", Message: "Owner edit", Timestamp: time.Date(2025, 10, 21, 14, 0, 0, 0, time.UTC)},
	})
	InsertVersion(repoA, "edita0000001", "main", repoA, "canon00000001", "main", false)
	hasEdits, _, resolved = readResolved(t, repoA, "canon00000001", "main")
	if hasEdits != 1 || !resolved.Valid || resolved.String != "Owner edit" {
		t.Errorf("same-repo edit did not apply: has_edits=%d resolved=%v", hasEdits, resolved)
	}
}

// --- Resolution by repository and hash ---

const movedRepo = "https://github.com/user/repo"

// movedEdit returns a social edit message whose edits reference names the canonical on a branch.
func movedEdit(text, canonicalHash, branch string) string {
	return text + "\n\nGitMsg: ext=\"social\"; type=\"post\"; edits=\"#commit:" + canonicalHash + "@" + branch + "\"; v=\"0.1.0\""
}

// insertAt inserts one commit of the test repository on a branch at a fixed hour.
func insertAt(t *testing.T, hash, branch, message string, hour int) {
	t.Helper()
	if err := InsertCommits([]Commit{{Hash: hash, RepoURL: movedRepo, Branch: branch, Message: message,
		Timestamp: time.Date(2025, 10, 21, hour, 0, 0, 0, time.UTC)}}); err != nil {
		t.Fatalf("InsertCommits(%s@%s) error = %v", hash, branch, err)
	}
}

// markStale marks the row of a hash on feature/x stale.
func markStale(t *testing.T, hash string) {
	t.Helper()
	if err := ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE core_commits SET stale_since = '2025-10-22T00:00:00Z' WHERE repo_url = ? AND hash = ? AND branch = 'feature/x'`, movedRepo, hash)
		return err
	}); err != nil {
		t.Fatalf("markStale: %v", err)
	}
}

// ftsBranches returns the branches of the rows of a hash whose search row matches a term.
func ftsBranches(t *testing.T, hash, term string) []string {
	t.Helper()
	branches, err := QueryLocked(func(db *sql.DB) ([]string, error) {
		rows, err := db.Query(`SELECT c.branch FROM core_commits c WHERE c.repo_url = ? AND c.hash = ?
			AND c.rowid IN (SELECT rowid FROM core_fts WHERE core_fts MATCH ?) ORDER BY c.branch`, movedRepo, hash, term)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var b string
			if err := rows.Scan(&b); err != nil {
				return nil, err
			}
			out = append(out, b)
		}
		return out, rows.Err()
	})
	if err != nil {
		t.Fatalf("ftsBranches: %v", err)
	}
	return branches
}

// assertEdited checks that the row of the test canonical on a branch carries the edited text.
func assertEdited(t *testing.T, branch string) {
	t.Helper()
	hasEdits, _, resolved := readResolved(t, movedRepo, "aabb00112233", branch)
	if hasEdits != 1 || !resolved.Valid || !strings.HasPrefix(resolved.String, "Edited text") {
		t.Errorf("row on %s: has_edits=%d resolved=%q, want the edited text", branch, hasEdits, resolved.String)
	}
}

func TestApplyEdit_everyRowOfTheHash(t *testing.T) {
	setupTestDB(t)
	insertAt(t, "aabb00112233", "feature/x", "Original", 12)
	insertAt(t, "aabb00112233", "main", "Original", 12)
	markStale(t, "aabb00112233")
	insertAt(t, "ccdd44556677", "main", movedEdit("Edited text", "aabb00112233", "feature/x"), 13)

	assertEdited(t, "feature/x")
	assertEdited(t, "main")
	if got := ftsBranches(t, "aabb00112233", "Edited"); strings.Join(got, ",") != "feature/x,main" {
		t.Errorf("rows whose search row has the edited text = %v, want both", got)
	}
	if got := ftsBranches(t, "aabb00112233", "Original"); len(got) != 0 {
		t.Errorf("rows whose search row has the old text = %v, want none", got)
	}
}

func TestInsertCommits_editAppliesAcrossBranch(t *testing.T) {
	setupTestDB(t)
	insertAt(t, "aabb00112233", "main", "Original", 12)
	insertAt(t, "ccdd44556677", "main", movedEdit("Edited text", "aabb00112233", "feature/x"), 13)

	assertEdited(t, "main")
	branch, err := QueryLocked(func(db *sql.DB) (string, error) {
		var b string
		err := db.QueryRow(`SELECT canonical_branch FROM core_commits_version WHERE edit_hash = ?`, "ccdd44556677").Scan(&b)
		return b, err
	})
	if err != nil || branch != "feature/x" {
		t.Errorf("version row canonical_branch = %q (%v), want the branch of the reference", branch, err)
	}
}

func TestInsertCommits_newRowOfKnownCanonical(t *testing.T) {
	setupTestDB(t)
	insertAt(t, "aabb00112233", "feature/x", "Original", 12)
	insertAt(t, "ccdd44556677", "feature/x", movedEdit("Edited text", "aabb00112233", "feature/x"), 13)
	insertAt(t, "aabb00112233", "main", "Original", 12)

	assertEdited(t, "main")
	if got := ftsBranches(t, "aabb00112233", "Edited"); strings.Join(got, ",") != "feature/x,main" {
		t.Errorf("rows whose search row has the edited text = %v, want both", got)
	}
}

func TestApplyEdit_extensionColumnsEveryRow(t *testing.T) {
	setupTestDBWithAllSchemas(t)
	insertAt(t, "aabb00112233", "feature/x", "Issue", 12)
	insertAt(t, "aabb00112233", "main", "Issue", 12)
	insertAt(t, "ccdd44556677", "main", movedEdit("Issue closed", "aabb00112233", "feature/x"), 13)
	insertAt(t, "eeff00112233", "main", "Other issue", 12)
	insertAt(t, "ffee00112233", "main", movedEdit("Other issue edited", "eeff00112233", "main"), 13)
	if err := ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`INSERT INTO pm_items (repo_url, hash, branch, type, state, assignees) VALUES
			(?, 'aabb00112233', 'feature/x', 'issue', 'open', NULL),
			(?, 'aabb00112233', 'main', 'issue', 'open', NULL),
			(?, 'ccdd44556677', 'main', 'issue', 'closed', 'a@x.com,b@x.com'),
			(?, 'eeff00112233', 'main', 'issue', 'open', 'a@x.com')`, movedRepo, movedRepo, movedRepo, movedRepo)
		return err
	}); err != nil {
		t.Fatalf("insert pm rows: %v", err)
	}
	SyncEditExtensionFields([]EditKey{{RepoURL: movedRepo, Hash: "ccdd44556677", Branch: "main"}, {RepoURL: movedRepo, Hash: "ffee00112233", Branch: "main"}})

	type pmRow struct {
		state, assignees string
		links            int
	}
	read := func(hash, branch string) pmRow {
		t.Helper()
		row, err := QueryLocked(func(db *sql.DB) (pmRow, error) {
			var r pmRow
			err := db.QueryRow(`SELECT state, COALESCE(assignees, ''),
				(SELECT COUNT(*) FROM pm_assignees a WHERE a.repo_url = p.repo_url AND a.hash = p.hash AND a.branch = p.branch)
				FROM pm_items p WHERE repo_url = ? AND hash = ? AND branch = ?`, movedRepo, hash, branch).Scan(&r.state, &r.assignees, &r.links)
			return r, err
		})
		if err != nil {
			t.Fatalf("read pm row %s@%s: %v", hash, branch, err)
		}
		return row
	}
	for _, branch := range []string{"feature/x", "main"} {
		if got := read("aabb00112233", branch); got.state != "closed" || got.assignees != "a@x.com,b@x.com" || got.links != 2 {
			t.Errorf("canonical row on %s = %+v, want closed with two linked assignees", branch, got)
		}
	}
	if got := read("eeff00112233", "main"); got.state != "open" || got.assignees != "a@x.com" {
		t.Errorf("canonical whose edit has no pm row = %+v, want its own columns kept", got)
	}
}

// withLabels returns a social message with a labels field and, when canonicalHash is set, an edits reference to it on feature/x.
func withLabels(text, labels, canonicalHash string) string {
	fields := `type="post"; `
	if labels != "" {
		fields += `labels="` + labels + `"; `
	}
	if canonicalHash != "" {
		fields += `edits="#commit:` + canonicalHash + `@feature/x"; `
	}
	return text + "\n\nGitMsg: ext=\"social\"; " + fields + "v=\"0.1.0\""
}

// labelsOf returns the labels column and the linked labels of the row of a hash on a branch.
func labelsOf(t *testing.T, repoURL, hash, branch string) (string, string) {
	t.Helper()
	type labels struct{ column, linked string }
	got, err := QueryLocked(func(db *sql.DB) (labels, error) {
		var l labels
		err := db.QueryRow(`SELECT COALESCE(labels, ''),
			COALESCE((SELECT GROUP_CONCAT(label) FROM core_labels cl WHERE cl.repo_url = c.repo_url AND cl.hash = c.hash AND cl.branch = c.branch), '')
			FROM core_commits c WHERE repo_url = ? AND hash = ? AND branch = ?`, repoURL, hash, branch).Scan(&l.column, &l.linked)
		return l, err
	})
	if err != nil {
		t.Fatalf("labelsOf(%s@%s): %v", hash, branch, err)
	}
	return got.column, got.linked
}

func TestApplyEdit_labelsFromLatestEditThatCarriesThem(t *testing.T) {
	setupTestDB(t)
	insertAt(t, "aabb00112233", "feature/x", withLabels("Original", "a", ""), 12)
	insertAt(t, "ccdd44556677", "feature/x", withLabels("First edit", "b", "aabb00112233"), 13)
	insertAt(t, "eeff00112233", "feature/x", withLabels("Second edit", "", "aabb00112233"), 14)
	insertAt(t, "aabb00112233", "main", withLabels("Original", "a", ""), 12)
	markStale(t, "aabb00112233")
	for _, branch := range []string{"feature/x", "main"} {
		if column, linked := labelsOf(t, movedRepo, "aabb00112233", branch); column != "b" || linked != "b" {
			t.Errorf("long-lived row on %s: labels %q, linked %q, want b from the first edit", branch, column, linked)
		}
		if _, _, resolved := readResolved(t, movedRepo, "aabb00112233", branch); !strings.HasPrefix(resolved.String, "Second edit") {
			t.Errorf("long-lived row on %s: resolved %q, want the text of the second edit", branch, resolved.String)
		}
	}

	const rebuilt = "https://github.com/user/rebuilt"
	if err := InsertCommits([]Commit{
		{Hash: "aabb00112233", RepoURL: rebuilt, Branch: "main", Message: withLabels("Original", "a", ""), Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC)},
		{Hash: "ccdd44556677", RepoURL: rebuilt, Branch: "main", Message: withLabels("First edit", "b", "aabb00112233"), Timestamp: time.Date(2025, 10, 21, 13, 0, 0, 0, time.UTC)},
		{Hash: "eeff00112233", RepoURL: rebuilt, Branch: "main", Message: withLabels("Second edit", "", "aabb00112233"), Timestamp: time.Date(2025, 10, 21, 14, 0, 0, 0, time.UTC)},
	}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	if column, linked := labelsOf(t, rebuilt, "aabb00112233", "main"); column != "b" || linked != "b" {
		t.Errorf("rebuilt row: labels %q, linked %q, want b from the first edit", column, linked)
	}
}

func TestApplyEdit_extensionColumnsFromLatestEditWithRow(t *testing.T) {
	setupTestDBWithAllSchemas(t)
	insertAt(t, "aabb00112233", "feature/x", "Issue", 12)
	insertAt(t, "ccdd44556677", "feature/x", movedEdit("Issue closed", "aabb00112233", "feature/x"), 13)
	insertAt(t, "eeff00112233", "feature/x", movedEdit("Issue retitled", "aabb00112233", "feature/x"), 14)
	pmRow := func(hash, branch, state string) {
		t.Helper()
		if err := ExecLocked(func(db *sql.DB) error {
			_, err := db.Exec(`INSERT INTO pm_items (repo_url, hash, branch, type, state) VALUES (?, ?, ?, 'issue', ?)`, movedRepo, hash, branch, state)
			return err
		}); err != nil {
			t.Fatalf("insert pm row: %v", err)
		}
	}
	pmRow("aabb00112233", "feature/x", "open")
	pmRow("ccdd44556677", "feature/x", "closed")
	SyncEditExtensionFields([]EditKey{{RepoURL: movedRepo, Hash: "ccdd44556677", Branch: "feature/x"}})
	insertAt(t, "aabb00112233", "main", "Issue", 12)
	pmRow("aabb00112233", "main", "open")
	markStale(t, "aabb00112233")
	SyncEditExtensionFields([]EditKey{{RepoURL: movedRepo, Hash: "eeff00112233", Branch: "feature/x"}})

	for _, branch := range []string{"feature/x", "main"} {
		state, err := QueryLocked(func(db *sql.DB) (string, error) {
			var s string
			err := db.QueryRow(`SELECT state FROM pm_items WHERE repo_url = ? AND hash = ? AND branch = ?`, movedRepo, "aabb00112233", branch).Scan(&s)
			return s, err
		})
		if err != nil || state != "closed" {
			t.Errorf("canonical pm row on %s: state %q (%v), want closed from the latest edit with a pm row", branch, state, err)
		}
	}

	const rebuilt = "https://github.com/user/rebuilt"
	if err := InsertCommits([]Commit{
		{Hash: "aabb00112233", RepoURL: rebuilt, Branch: "main", Message: "Issue", Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC)},
		{Hash: "ccdd44556677", RepoURL: rebuilt, Branch: "main", Message: movedEdit("Issue closed", "aabb00112233", "main"), Timestamp: time.Date(2025, 10, 21, 13, 0, 0, 0, time.UTC)},
		{Hash: "eeff00112233", RepoURL: rebuilt, Branch: "main", Message: movedEdit("Issue retitled", "aabb00112233", "main"), Timestamp: time.Date(2025, 10, 21, 14, 0, 0, 0, time.UTC)},
	}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	if err := ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`INSERT INTO pm_items (repo_url, hash, branch, type, state) VALUES (?, 'aabb00112233', 'main', 'issue', 'open'), (?, 'ccdd44556677', 'main', 'issue', 'closed')`, rebuilt, rebuilt)
		return err
	}); err != nil {
		t.Fatalf("insert pm rows: %v", err)
	}
	SyncEditExtensionFields([]EditKey{{RepoURL: rebuilt, Hash: "ccdd44556677", Branch: "main"}, {RepoURL: rebuilt, Hash: "eeff00112233", Branch: "main"}})
	state, err := QueryLocked(func(db *sql.DB) (string, error) {
		var s string
		err := db.QueryRow(`SELECT state FROM pm_items WHERE repo_url = ? AND hash = 'aabb00112233'`, rebuilt).Scan(&s)
		return s, err
	})
	if err != nil || state != "closed" {
		t.Errorf("rebuilt canonical pm row: state %q (%v), want closed", state, err)
	}
}

func TestReconcileVersions_acrossBranch(t *testing.T) {
	setupTestDB(t)
	insertAt(t, "ccdd44556677", "main", movedEdit("Edited text", "aabb00112233", "feature/x"), 13)
	insertAt(t, "aabb00112233", "main", "Original", 12)
	if created, err := ReconcileVersions(); err != nil || created != 1 {
		t.Fatalf("ReconcileVersions() = %d (%v), want one version row", created, err)
	}
	assertEdited(t, "main")
}

func TestApplyEdit_marksEveryRowOfTheEdit(t *testing.T) {
	setupTestDB(t)
	insertAt(t, "aabb00112233", "main", "Original", 12)
	insertAt(t, "ccdd44556677", "feature/x", "Edited text", 13)
	insertAt(t, "ccdd44556677", "main", "Edited text", 13)
	if err := InsertVersion(movedRepo, "ccdd44556677", "main", movedRepo, "aabb00112233", "main", false); err != nil {
		t.Fatalf("InsertVersion() error = %v", err)
	}
	marked, err := QueryLocked(func(db *sql.DB) (int, error) {
		var n int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_commits WHERE repo_url = ? AND hash = ? AND is_edit_commit = 1`, movedRepo, "ccdd44556677").Scan(&n)
		return n, err
	})
	if err != nil || marked != 2 {
		t.Errorf("edit rows marked = %d (%v), want both", marked, err)
	}
}
