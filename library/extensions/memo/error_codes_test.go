// error_codes_test.go - Error codes the memo write paths return at the Result boundary
package memo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// blockedTierPath returns a regular file path, which no tier repo can be created under.
func blockedTierPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("occupied\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// skipWithoutFilesRefBackend skips the test unless the repository stores refs in files, the backend a ref lock blocks.
func skipWithoutFilesRefBackend(t *testing.T, dir string) {
	t.Helper()
	if out, err := git.ExecGit(dir, []string{"rev-parse", "--show-ref-format"}); err == nil {
		if format := strings.TrimSpace(out.Stdout); format != "files" {
			t.Skipf("ref format is %q, and a ref lock blocks writes on the files backend", format)
		}
		return
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "reftable")); err == nil {
		t.Skip("ref format is reftable, and a ref lock blocks writes on the files backend")
	}
}

// lockInheritRef leaves a ref lock on a memo source URL's inherits ref, the way a crashed writer would.
func lockInheritRef(t *testing.T, dir, normalizedURL string) {
	t.Helper()
	lock := filepath.Join(dir, ".git", inheritRefPath(normalizedURL)+".lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(lock), err)
	}
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatalf("write %s: %v", lock, err)
	}
}

// TestCreateMemo_invalidLabels asserts INVALID_LABELS on an expires label that is not a date.
func TestCreateMemo_invalidLabels(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	res := CreateMemo(dir, "Bad label", "", CreateMemoOptions{
		Tier:   TierSession,
		Labels: []string{"expires/tomorrow"},
	})
	if res.Success || res.Error.Code != "INVALID_LABELS" {
		t.Errorf("CreateMemo() with expires/tomorrow = %+v, want INVALID_LABELS", res)
	}

	created := CreateMemo(dir, "Good label", "", CreateMemoOptions{Tier: TierSession})
	if !created.Success {
		t.Fatalf("CreateMemo() failed: %s", created.Error.Message)
	}
	badLabels := []string{"expires/next-week"}
	edited := EditMemo(dir, created.Data.ID, EditMemoOptions{Labels: &badLabels})
	if edited.Success || edited.Error.Code != "INVALID_LABELS" {
		t.Errorf("EditMemo() with expires/next-week = %+v, want INVALID_LABELS", edited)
	}
}

// TestEditMemo_readonlyTier asserts READONLY_TIER on a memo owned by another repository.
func TestEditMemo_readonlyTier(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)
	other := initTestRepo(t)

	if _, err := git.ExecGit(other, []string{"remote", "set-url", "origin", "https://example.com/other-repo.git"}); err != nil {
		t.Fatalf("set-url: %v", err)
	}
	created := CreateMemo(other, "Their memo", "body", CreateMemoOptions{Tier: TierProject})
	if !created.Success {
		t.Fatalf("CreateMemo() failed: %s", created.Error.Message)
	}

	newSubject := "Mine now"
	edited := EditMemo(dir, created.Data.ID, EditMemoOptions{Subject: &newSubject})
	if edited.Success || edited.Error.Code != "READONLY_TIER" {
		t.Errorf("EditMemo() on an external memo = %+v, want READONLY_TIER", edited)
	}
	retracted := RetractMemo(dir, created.Data.ID)
	if retracted.Success || retracted.Error.Code != "READONLY_TIER" {
		t.Errorf("RetractMemo() on an external memo = %+v, want READONLY_TIER", retracted)
	}
}

// TestCreateMemo_tierInitFailed asserts TIER_INIT_FAILED when the tier repo cannot be created.
func TestCreateMemo_tierInitFailed(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)
	t.Setenv("MEMO_SESSION_DIR", blockedTierPath(t))

	res := CreateMemo(dir, "Nowhere to land", "", CreateMemoOptions{Tier: TierSession})
	if res.Success || res.Error.Code != "TIER_INIT_FAILED" {
		t.Errorf("CreateMemo() onto an uncreatable session tier = %+v, want TIER_INIT_FAILED", res)
	}
}

// TestInitProject_outsideRepository asserts PROJECT_INIT_FAILED when the workdir is not a git repository.
func TestInitProject_outsideRepository(t *testing.T) {
	setupTestDB(t)
	freshHome(t)

	res := InitProject(t.TempDir())
	if res.Success || res.Error.Code != "PROJECT_INIT_FAILED" {
		t.Errorf("InitProject() outside a repository = %+v, want PROJECT_INIT_FAILED", res)
	}
}

// TestInitPersonal_blockedPath asserts PERSONAL_INIT_FAILED when the personal repo path is taken.
func TestInitPersonal_blockedPath(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	t.Setenv("GITSOCIAL_PERSONAL_REPO", blockedTierPath(t))

	res := InitPersonal()
	if res.Success || res.Error.Code != "PERSONAL_INIT_FAILED" {
		t.Errorf("InitPersonal() onto a taken path = %+v, want PERSONAL_INIT_FAILED", res)
	}
}

// TestInitSession_blockedPath asserts SESSION_INIT_FAILED when the session directory is taken.
func TestInitSession_blockedPath(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	t.Setenv("MEMO_SESSION_DIR", blockedTierPath(t))

	res := InitSession("blocked-session", "")
	if res.Success || res.Error.Code != "SESSION_INIT_FAILED" {
		t.Errorf("InitSession() onto a taken path = %+v, want SESSION_INIT_FAILED", res)
	}
}

// TestAddInherit_lockedRef asserts REF_WRITE_FAILED when the inherits ref cannot be written.
func TestAddInherit_lockedRef(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)
	skipWithoutFilesRefBackend(t, dir)

	url := protocol.NormalizeURL("https://example.com/locked-policies.git")
	lockInheritRef(t, dir, url)

	res := AddInherit(dir, url)
	if res.Success || res.Error.Code != "REF_WRITE_FAILED" {
		t.Errorf("AddInherit() over a locked ref = %+v, want REF_WRITE_FAILED", res)
	}
	if IsInherited(dir, url) {
		t.Error("the URL is registered as inherited after the failed ref write")
	}
}

// TestRemoveInherit_lockedRef asserts REF_DELETE_FAILED when the inherits ref cannot be deleted.
func TestRemoveInherit_lockedRef(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)
	skipWithoutFilesRefBackend(t, dir)

	url := protocol.NormalizeURL("https://example.com/stuck-policies.git")
	if added := AddInherit(dir, url); !added.Success {
		t.Fatalf("AddInherit() failed: %s", added.Error.Message)
	}
	lockInheritRef(t, dir, url)

	res := RemoveInherit(dir, url)
	if res.Success || res.Error.Code != "REF_DELETE_FAILED" {
		t.Errorf("RemoveInherit() over a locked ref = %+v, want REF_DELETE_FAILED", res)
	}
	if !IsInherited(dir, url) {
		t.Error("the URL stopped being inherited after the failed ref delete")
	}
}
