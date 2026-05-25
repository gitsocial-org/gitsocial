// memo_test.go - Tests for memo public API and cross-tier flows
package memo

import (
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

var (
	repoTemplate  string
	testCacheDir  string
	tempHomeDir   string
	origHome      string
	origSessionID string
)

func TestMain(m *testing.M) {
	// A reusable git repo template (cloned per test) so each test gets a fresh
	// workspace without paying the init cost.
	tmpl, _ := os.MkdirTemp("", "memo-test-template-*")
	git.Init(tmpl, "main")
	git.ExecGit(tmpl, []string{"config", "user.email", "memo-test@test.com"})
	git.ExecGit(tmpl, []string{"config", "user.name", "Memo Test"})
	git.CreateCommit(tmpl, git.CommitOptions{Message: "init", AllowEmpty: true})
	git.ExecGit(tmpl, []string{"remote", "add", "origin", "https://github.com/test/memo-repo.git"})
	repoTemplate = tmpl

	// Cache and isolated $HOME so memo tier paths don't escape the temp dir.
	cacheDir, _ := os.MkdirTemp("", "memo-test-cache-*")
	cache.Open(cacheDir)
	testCacheDir = cacheDir

	tempHomeDir, _ = os.MkdirTemp("", "memo-test-home-*")
	origHome = os.Getenv("HOME")
	os.Setenv("HOME", tempHomeDir)

	origSessionID = os.Getenv("MEMO_SESSION_ID")
	os.Setenv("MEMO_SESSION_ID", "memo-test-session")

	code := m.Run()

	cache.Reset()
	os.RemoveAll(cacheDir)
	os.RemoveAll(tmpl)
	os.RemoveAll(tempHomeDir)
	if origHome == "" {
		os.Unsetenv("HOME")
	} else {
		os.Setenv("HOME", origHome)
	}
	if origSessionID == "" {
		os.Unsetenv("MEMO_SESSION_ID")
	} else {
		os.Setenv("MEMO_SESSION_ID", origSessionID)
	}
	os.Exit(code)
}

func setupTestDB(t *testing.T) {
	t.Helper()
	cache.Reset()
	dir := t.TempDir()
	if err := cache.Open(dir); err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := copyDir(repoTemplate, dir); err != nil {
		t.Fatalf("copyDir: %v", err)
	}
	return dir
}

// freshHome resets the temporary $HOME between tests so per-tier bare repos
// don't leak state across cases.
func freshHome(t *testing.T) {
	t.Helper()
	newHome := t.TempDir()
	os.Setenv("HOME", newHome)
	t.Cleanup(func() { os.Setenv("HOME", tempHomeDir) })
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		sp := filepath.Join(src, e.Name())
		dp := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := os.MkdirAll(dp, 0o755); err != nil {
				return err
			}
			if err := copyDir(sp, dp); err != nil {
				return err
			}
			continue
		}
		sf, err := os.Open(sp)
		if err != nil {
			return err
		}
		df, err := os.Create(dp)
		if err != nil {
			sf.Close()
			return err
		}
		_, err = io.Copy(df, sf)
		sf.Close()
		df.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// --- Pure tests (no DB) ---------------------------------------------------

func TestTierPriority(t *testing.T) {
	if TierPriority(TierSession) != 0 {
		t.Errorf("session priority = %d, want 0", TierPriority(TierSession))
	}
	if TierPriority(TierPersonal) >= TierPriority(TierProject) {
		t.Errorf("personal must outrank project")
	}
	if TierPriority(TierProject) >= TierPriority(TierInherited) {
		t.Errorf("project must outrank inherited (retrieval order)")
	}
	if TierPriority(TierInherited) >= TierPriority(TierExternal) {
		t.Errorf("inherited must outrank external")
	}
}

func TestResolveSessionID_envVar(t *testing.T) {
	os.Setenv("MEMO_SESSION_ID", "explicit-id")
	t.Cleanup(func() { os.Setenv("MEMO_SESSION_ID", "memo-test-session") })
	if got := ResolveSessionID(); got != "explicit-id" {
		t.Errorf("ResolveSessionID = %q, want explicit-id", got)
	}
}

func TestResolveSessionID_generates(t *testing.T) {
	os.Unsetenv("MEMO_SESSION_ID")
	t.Cleanup(func() { os.Setenv("MEMO_SESSION_ID", "memo-test-session") })
	id := ResolveSessionID()
	if !strings.Contains(id, "-") {
		t.Errorf("auto-generated id should contain -; got %q", id)
	}
	parts := strings.SplitN(id, "-", 2)
	if len(parts[0]) != 8 {
		t.Errorf("date prefix = %q, want 8 chars", parts[0])
	}
}

func TestLocalRepoURL_roundTrip(t *testing.T) {
	url := LocalRepoURL("/tmp/x")
	if !strings.HasPrefix(url, "local:") {
		t.Errorf("LocalRepoURL missing prefix: %q", url)
	}
	if PathFromLocalURL(url) != "/tmp/x" {
		t.Errorf("PathFromLocalURL round-trip failed: %q", PathFromLocalURL(url))
	}
}

func TestJoinLabels_dedupAndSort(t *testing.T) {
	got := joinLabels([]string{"b", "a", "a", "  c  ", ""})
	if got != "a,b,c" {
		t.Errorf("joinLabels = %q, want a,b,c", got)
	}
}

func TestBuildMemoContent_includesSubjectAndLabels(t *testing.T) {
	content := buildMemoContent("Subject", "Body", CreateMemoOptions{
		Tier:   TierSession,
		Labels: []string{"kind/policy", "priority/high"},
	}, "")
	if !strings.Contains(content, "Subject") {
		t.Error("missing subject")
	}
	if !strings.Contains(content, "Body") {
		t.Error("missing body")
	}
	msg := protocol.ParseMessage(content)
	if msg == nil {
		t.Fatal("ParseMessage returned nil")
	}
	if msg.Header.Ext != "memo" {
		t.Errorf("ext = %q, want memo", msg.Header.Ext)
	}
	if got := msg.Header.Fields["labels"]; got != "kind/policy,priority/high" {
		t.Errorf("labels = %q", got)
	}
}

// --- Integration tests (DB + git) -----------------------------------------

func TestCreateMemo_session(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	res := CreateMemo(dir, "First memo", "body content", CreateMemoOptions{
		Tier:   TierSession,
		Labels: []string{"kind/policy"},
	})
	if !res.Success {
		t.Fatalf("CreateMemo failed: %s", res.Error.Message)
	}
	if res.Data.Subject != "First memo" {
		t.Errorf("subject = %q", res.Data.Subject)
	}
	if res.Data.Tier != TierSession {
		t.Errorf("tier = %q, want session", res.Data.Tier)
	}
	if len(res.Data.Labels) != 1 || res.Data.Labels[0] != "kind/policy" {
		t.Errorf("labels = %v", res.Data.Labels)
	}
}

func TestCreateMemo_emptySubject(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	res := CreateMemo(dir, "", "body", CreateMemoOptions{Tier: TierSession})
	if res.Success {
		t.Fatal("expected failure for empty subject")
	}
	if res.Error.Code != "INVALID_ARGS" {
		t.Errorf("code = %q, want INVALID_ARGS", res.Error.Code)
	}
}

// TestEditMemo_noopReturnsError locks the §3.1 fix: an edit with no actual
// changes (all-nil opts, or explicit opts that match the existing values)
// must return NO_CHANGES instead of writing a churn commit + flipping is_edited.
func TestEditMemo_noopReturnsError(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	created := CreateMemo(dir, "stable", "body content",
		CreateMemoOptions{Tier: TierSession, Labels: []string{"kind/policy"}})
	if !created.Success {
		t.Fatalf("CreateMemo: %s", created.Error.Message)
	}

	// All-nil opts → no changes.
	if r := EditMemo(dir, created.Data.ID, EditMemoOptions{}); r.Success || r.Error.Code != "NO_CHANGES" {
		t.Errorf("nil-opts edit: success=%v code=%q (want NO_CHANGES)", r.Success, r.Error.Code)
	}

	// Explicit opts matching existing values → still no changes.
	sameSubj := "stable"
	sameBody := "body content"
	sameLabels := []string{"kind/policy"}
	if r := EditMemo(dir, created.Data.ID, EditMemoOptions{
		Subject: &sameSubj, Body: &sameBody, Labels: &sameLabels,
	}); r.Success || r.Error.Code != "NO_CHANGES" {
		t.Errorf("explicit-same edit: success=%v code=%q (want NO_CHANGES)", r.Success, r.Error.Code)
	}

	// Same labels in different order — still no change (labelsEqual normalizes).
	reorderedLabels := []string{"kind/policy"}
	if r := EditMemo(dir, created.Data.ID, EditMemoOptions{
		Labels: &reorderedLabels,
	}); r.Success || r.Error.Code != "NO_CHANGES" {
		t.Errorf("same-labels edit: success=%v code=%q (want NO_CHANGES)", r.Success, r.Error.Code)
	}

	// Existing memo must NOT be marked edited.
	got := GetSingleMemo(created.Data.ID, "", nil)
	if !got.Success {
		t.Fatalf("GetSingleMemo: %s", got.Error.Message)
	}
	if got.Data.IsEdited {
		t.Error("memo got IsEdited=true after no-op edit (should remain false)")
	}

	// Sanity: a real change still succeeds.
	newBody := "real change"
	if r := EditMemo(dir, created.Data.ID, EditMemoOptions{Body: &newBody}); !r.Success {
		t.Errorf("real edit failed after no-op rejections: %s", r.Error.Message)
	}
}

func TestEditMemo_createsNewVersion(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	created := CreateMemo(dir, "Original", "", CreateMemoOptions{Tier: TierSession})
	if !created.Success {
		t.Fatalf("Create: %s", created.Error.Message)
	}

	newSubj := "Edited"
	edited := EditMemo(dir, created.Data.ID, EditMemoOptions{Subject: &newSubj})
	if !edited.Success {
		t.Fatalf("Edit: %s", edited.Error.Message)
	}
	// Latest version (the canonical) should now show edited content + IsEdited.
	got := GetSingleMemo(created.Data.ID, "", nil)
	if !got.Success {
		t.Fatalf("Get: %s", got.Error.Message)
	}
	if got.Data.Subject != "Edited" {
		t.Errorf("subject after edit = %q, want Edited", got.Data.Subject)
	}
	if !got.Data.IsEdited {
		t.Error("memo should be marked as edited")
	}
}

func TestRetractMemo_hidesFromList(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	created := CreateMemo(dir, "Doomed", "", CreateMemoOptions{Tier: TierSession})
	if !created.Success {
		t.Fatalf("Create: %s", created.Error.Message)
	}
	if r := RetractMemo(dir, created.Data.ID); !r.Success {
		t.Fatalf("Retract: %s", r.Error.Message)
	}

	listed := ListMemos(dir, ListOptions{IncludeSessions: "all"})
	if !listed.Success {
		t.Fatalf("List: %s", listed.Error.Message)
	}
	for _, m := range listed.Data {
		if m.Subject == "Doomed" {
			t.Error("retracted memo should not surface in default list")
		}
	}
}

func TestPromoteMemo_copiesToTargetTier(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	if r := InitPersonal(); !r.Success {
		t.Fatalf("InitPersonal: %s", r.Error.Message)
	}
	created := CreateMemo(dir, "Promote me", "body", CreateMemoOptions{
		Tier:   TierSession,
		Labels: []string{"kind/policy"},
	})
	if !created.Success {
		t.Fatalf("Create: %s", created.Error.Message)
	}

	promoted := PromoteMemo(dir, created.Data.ID, TierPersonal)
	if !promoted.Success {
		t.Fatalf("Promote: %s", promoted.Error.Message)
	}
	if promoted.Data.Tier != TierPersonal {
		t.Errorf("promoted tier = %q, want personal", promoted.Data.Tier)
	}
	if promoted.Data.Subject != "Promote me" {
		t.Errorf("promoted subject = %q", promoted.Data.Subject)
	}

	// Source should still exist on the session tier (no edit chain on promote).
	src := GetSingleMemo(created.Data.ID, "", nil)
	if !src.Success {
		t.Fatalf("source disappeared: %s", src.Error.Message)
	}
	if src.Data.IsEdited {
		t.Error("promote should not mark source as edited")
	}

	// Listing across tiers should see both copies.
	listed := ListMemos(dir, ListOptions{IncludeSessions: "all"})
	if !listed.Success {
		t.Fatalf("List: %s", listed.Error.Message)
	}
	subjects := 0
	for _, m := range listed.Data {
		if m.Subject == "Promote me" {
			subjects++
		}
	}
	if subjects != 2 {
		t.Errorf("expected 2 copies of promoted memo, got %d", subjects)
	}
}

func TestListMemos_filtersExpired(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	pastLabel := "expires/" + time.Now().Add(-48*time.Hour).Format("2006-01-02")
	futureLabel := "expires/" + time.Now().Add(48*time.Hour).Format("2006-01-02")

	if r := CreateMemo(dir, "Past memo", "", CreateMemoOptions{Tier: TierSession, Labels: []string{pastLabel}}); !r.Success {
		t.Fatalf("Create past: %s", r.Error.Message)
	}
	if r := CreateMemo(dir, "Future memo", "", CreateMemoOptions{Tier: TierSession, Labels: []string{futureLabel}}); !r.Success {
		t.Fatalf("Create future: %s", r.Error.Message)
	}

	def := ListMemos(dir, ListOptions{IncludeSessions: "all"})
	if !def.Success {
		t.Fatalf("List: %s", def.Error.Message)
	}
	hasPast, hasFuture := false, false
	for _, m := range def.Data {
		if m.Subject == "Past memo" {
			hasPast = true
		}
		if m.Subject == "Future memo" {
			hasFuture = true
		}
	}
	if hasPast {
		t.Error("default list should hide expired memo")
	}
	if !hasFuture {
		t.Error("default list should include not-yet-expired memo")
	}

	expired := ListMemos(dir, ListOptions{IncludeSessions: "all", OnlyExpired: true})
	if !expired.Success {
		t.Fatalf("List expired: %s", expired.Error.Message)
	}
	if len(expired.Data) != 1 || expired.Data[0].Subject != "Past memo" {
		t.Errorf("--expired result = %v", expired.Data)
	}

	includeExpired := ListMemos(dir, ListOptions{IncludeSessions: "all", IncludeExpired: true})
	if !includeExpired.Success {
		t.Fatalf("List include-expired: %s", includeExpired.Error.Message)
	}
	count := 0
	for _, m := range includeExpired.Data {
		if m.Subject == "Past memo" || m.Subject == "Future memo" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("--include-expired count = %d, want 2", count)
	}
}

func TestListMemos_tierOrdering(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	if r := InitProject(dir); !r.Success {
		t.Fatalf("InitProject: %s", r.Error.Message)
	}
	if r := InitPersonal(); !r.Success {
		t.Fatalf("InitPersonal: %s", r.Error.Message)
	}
	if r := CreateMemo(dir, "Project memo", "", CreateMemoOptions{Tier: TierProject}); !r.Success {
		t.Fatalf("Create project: %s", r.Error.Message)
	}
	if r := CreateMemo(dir, "Personal memo", "", CreateMemoOptions{Tier: TierPersonal}); !r.Success {
		t.Fatalf("Create personal: %s", r.Error.Message)
	}
	if r := CreateMemo(dir, "Session memo", "", CreateMemoOptions{Tier: TierSession}); !r.Success {
		t.Fatalf("Create session: %s", r.Error.Message)
	}

	listed := ListMemos(dir, ListOptions{IncludeSessions: "all"})
	if !listed.Success {
		t.Fatalf("List: %s", listed.Error.Message)
	}
	if len(listed.Data) < 3 {
		t.Fatalf("expected >= 3 memos, got %d", len(listed.Data))
	}
	// Order should be session, then personal, then project.
	wantOrder := []string{"Session memo", "Personal memo", "Project memo"}
	got := []string{}
	for _, m := range listed.Data {
		for _, want := range wantOrder {
			if m.Subject == want {
				got = append(got, m.Subject)
				break
			}
		}
	}
	for i, want := range wantOrder {
		if i >= len(got) || got[i] != want {
			t.Errorf("order[%d] = %q, want %q (full: %v)", i, safeIdx(got, i), want, got)
		}
	}
}

func TestSessionLifecycle(t *testing.T) {
	setupTestDB(t)
	freshHome(t)

	id := "lifecycle-test"
	res := InitSession(id, "")
	if !res.Success {
		t.Fatalf("InitSession: %s", res.Error.Message)
	}
	if res.Data != id {
		t.Errorf("init returned %q, want %q", res.Data, id)
	}

	listed := ListSessions("")
	if !listed.Success {
		t.Fatalf("ListSessions: %s", listed.Error.Message)
	}
	found := false
	for _, s := range listed.Data {
		if s.ID == id {
			found = true
		}
	}
	if !found {
		t.Errorf("session %q not in list", id)
	}

	gc := GCSession(id)
	if !gc.Success {
		t.Fatalf("GCSession: %s", gc.Error.Message)
	}
	listed2 := ListSessions("")
	for _, s := range listed2.Data {
		if s.ID == id {
			t.Errorf("session %q still present after gc", id)
		}
	}
}

func TestSessionWorkspaceFilter(t *testing.T) {
	setupTestDB(t)
	freshHome(t)

	urlA := "https://example.com/proj-a.git"
	urlB := "https://example.com/proj-b.git"

	if res := InitSession("sess-a", urlA); !res.Success {
		t.Fatalf("InitSession A: %s", res.Error.Message)
	}
	if res := InitSession("sess-b", urlB); !res.Success {
		t.Fatalf("InitSession B: %s", res.Error.Message)
	}
	if res := InitSession("sess-legacy", ""); !res.Success {
		t.Fatalf("InitSession legacy: %s", res.Error.Message)
	}

	listedA := ListSessions(urlA)
	if !listedA.Success {
		t.Fatalf("ListSessions(A): %s", listedA.Error.Message)
	}
	ids := map[string]bool{}
	for _, s := range listedA.Data {
		ids[s.ID] = true
	}
	if !ids["sess-a"] {
		t.Errorf("workspace A list missing sess-a; got %v", ids)
	}
	if ids["sess-b"] {
		t.Errorf("workspace A list leaked sess-b; got %v", ids)
	}
	if !ids["sess-legacy"] {
		t.Errorf("workspace A list missing untagged sess-legacy; got %v", ids)
	}

	listedAll := ListSessions("")
	if !listedAll.Success {
		t.Fatalf("ListSessions(\"\"): %s", listedAll.Error.Message)
	}
	if len(listedAll.Data) != 3 {
		t.Errorf("ListSessions(\"\") returned %d sessions, want 3", len(listedAll.Data))
	}
}

func safeIdx(s []string, i int) string {
	if i < 0 || i >= len(s) {
		return "<missing>"
	}
	return s[i]
}

// --- Regression tests for MEMO-REVIEW blockers ---------------------------

// TestGetSingleMemo_projectTierResolves locks in the §2.1 fix: when the
// workspace URL is passed correctly, a project-tier memo resolves to
// TierProject (and not TierExternal as it would with the previous empty-string
// argument).
func TestGetSingleMemo_projectTierResolves(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	if r := InitProject(dir); !r.Success {
		t.Fatalf("InitProject: %s", r.Error.Message)
	}
	created := CreateMemo(dir, "Project memo", "body", CreateMemoOptions{Tier: TierProject})
	if !created.Success {
		t.Fatalf("CreateMemo: %s", created.Error.Message)
	}

	workspaceURL := gitmsg.ResolveRepoURL(dir)
	res := GetSingleMemo(created.Data.ID, workspaceURL, ListInherits(dir))
	if !res.Success {
		t.Fatalf("GetSingleMemo: %s", res.Error.Message)
	}
	if res.Data.Tier != TierProject {
		t.Errorf("tier = %q, want project (§2.1 regression)", res.Data.Tier)
	}
}

// TestGCSession_clearsCacheRows locks in the §2.2 fix: GCSession must wipe
// every cache row keyed to the gc'd session's local: URL.
func TestGCSession_clearsCacheRows(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	sessionID := "gc-cleanup-session"
	os.Setenv("MEMO_SESSION_ID", sessionID)
	t.Cleanup(func() { os.Setenv("MEMO_SESSION_ID", "memo-test-session") })

	created := CreateMemo(dir, "doomed", "body", CreateMemoOptions{
		Tier:   TierSession,
		Labels: []string{"kind/policy"},
	})
	if !created.Success {
		t.Fatalf("CreateMemo: %s", created.Error.Message)
	}

	path, err := SessionRepoPath(sessionID)
	if err != nil {
		t.Fatalf("SessionRepoPath: %v", err)
	}
	sessionRepoURL := LocalRepoURL(path)
	preGC, err := GetMemoItems(MemoQuery{RepoURL: sessionRepoURL})
	if err != nil {
		t.Fatalf("pre-gc GetMemoItems: %v", err)
	}
	if len(preGC) == 0 {
		t.Fatal("session memo not in cache before GC")
	}

	if gc := GCSession(sessionID); !gc.Success {
		t.Fatalf("GCSession: %s", gc.Error.Message)
	}

	postGC, err := GetMemoItems(MemoQuery{RepoURL: sessionRepoURL})
	if err != nil {
		t.Fatalf("post-gc GetMemoItems: %v", err)
	}
	if len(postGC) != 0 {
		t.Errorf("memo_items rows for gc'd session still present: %d", len(postGC))
	}
}

// TestConfig_roundtrip locks in the §2.3 fix: the memo Config has no Branch
// field, and SaveConfig still writes branch=gitmsg/memo so the core protocol
// layer can locate the extension.
func TestConfig_roundtrip(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	cfg := DefaultConfig()
	if cfg.Version != "0.1.0" {
		t.Errorf("DefaultConfig version = %q, want 0.1.0", cfg.Version)
	}
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got := GetConfig(dir)
	if got.Version != "0.1.0" {
		t.Errorf("round-trip Version = %q", got.Version)
	}
	if !IsProjectInitialized(dir) {
		t.Error("project not marked initialized after SaveConfig (branch field missing from raw config)")
	}
}

// TestInherits_lifecycle covers add/list/remove + IsInherited idempotency.
// Locks in the §2.5 surfacing contract that --tier inherited reads from this
// ref set.
func TestInherits_lifecycle(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	url := protocol.NormalizeURL("https://example.com/org-policies.git")
	if r := AddInherit(dir, url); !r.Success {
		t.Fatalf("AddInherit: %s", r.Error.Message)
	}
	if !IsInherited(dir, url) {
		t.Fatal("IsInherited = false after AddInherit")
	}
	listed := ListInherits(dir)
	found := false
	for _, u := range listed {
		if u == url {
			found = true
		}
	}
	if !found {
		t.Errorf("ListInherits missing %q (got %v)", url, listed)
	}

	// Re-add is idempotent and reports already-present.
	if r := AddInherit(dir, url); !r.Success || r.Data {
		t.Errorf("idempotent re-add: success=%v data=%v (want success=true data=false)", r.Success, r.Data)
	}

	if r := RemoveInherit(dir, url); !r.Success {
		t.Fatalf("RemoveInherit: %s", r.Error.Message)
	}
	if IsInherited(dir, url) {
		t.Error("IsInherited = true after RemoveInherit")
	}
}

// TestTierForRepoURL_classification verifies tier classification for every
// branch of TierForRepoURL — the function the CLI bug in §2.1 was abusing.
func TestTierForRepoURL_classification(t *testing.T) {
	freshHome(t)
	workspaceURL := "https://example.com/my-repo.git"
	inherited := []string{"https://example.com/policies.git"}

	cases := []struct {
		name    string
		repoURL string
		want    Tier
	}{
		{"workspace", workspaceURL, TierProject},
		{"inherited", "https://example.com/policies.git", TierInherited},
		{"external", "https://example.com/other.git", TierExternal},
		{"empty falls to project", "", TierProject},
	}
	for _, c := range cases {
		got := TierForRepoURL(c.repoURL, workspaceURL, inherited)
		if got != c.want {
			t.Errorf("%s: TierForRepoURL(%q) = %q, want %q", c.name, c.repoURL, got, c.want)
		}
	}
}

// TestListMemos_inheritedSurfaces injects a memo on an inherited URL directly
// into cache (since the test can't fetch from a fake remote) and verifies the
// memo surfaces in default list AND in --tier inherited but not --tier external.
func TestListMemos_inheritedSurfaces(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	inheritedURL := protocol.NormalizeURL("https://example.com/org-policies.git")
	if r := AddInherit(dir, inheritedURL); !r.Success {
		t.Fatalf("AddInherit: %s", r.Error.Message)
	}

	// Build a real memo commit message so the protocol parser populates labels.
	commitMsg := buildMemoContent("Policy: encrypt at rest", "All PII must be encrypted at rest.",
		CreateMemoOptions{Labels: []string{"kind/policy", "priority/critical"}}, "")
	hash := "deadbeef1234567890abcdef1234567890abcdef"
	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        hash,
		RepoURL:     inheritedURL,
		Branch:      MemoBranch,
		AuthorName:  "Policy Bot",
		AuthorEmail: "bot@example.com",
		Message:     commitMsg,
		Timestamp:   time.Now(),
	}}); err != nil {
		t.Fatalf("InsertCommits: %v", err)
	}
	if msg := protocol.ParseMessage(commitMsg); msg != nil {
		cache.ProcessVersionFromHeader(msg, hash, inheritedURL, MemoBranch)
	}
	if err := InsertMemoItem(MemoItem{
		RepoURL: inheritedURL,
		Hash:    hash,
		Branch:  MemoBranch,
		Type:    "memo",
	}); err != nil {
		t.Fatalf("InsertMemoItem: %v", err)
	}

	defaultList := ListMemos(dir, ListOptions{IncludeSessions: "all"})
	if !defaultList.Success {
		t.Fatalf("ListMemos default: %s", defaultList.Error.Message)
	}
	if !containsMemoWithTier(defaultList.Data, "Policy: encrypt at rest", TierInherited) {
		t.Errorf("inherited memo not surfaced in default list (got %d memos)", len(defaultList.Data))
	}

	inheritedOnly := ListMemos(dir, ListOptions{Tier: TierInherited})
	if !inheritedOnly.Success {
		t.Fatalf("ListMemos --tier inherited: %s", inheritedOnly.Error.Message)
	}
	if !containsMemoWithTier(inheritedOnly.Data, "Policy: encrypt at rest", TierInherited) {
		t.Errorf("--tier inherited did not return the memo (got %d)", len(inheritedOnly.Data))
	}

	// External tier should not include inherited URLs.
	externalOnly := ListMemos(dir, ListOptions{Tier: TierExternal})
	if !externalOnly.Success {
		t.Fatalf("ListMemos --tier external: %s", externalOnly.Error.Message)
	}
	for _, m := range externalOnly.Data {
		if m.Repository == inheritedURL {
			t.Errorf("inherited memo leaked into --tier external")
		}
	}
}

func containsMemoWithTier(memos []Memo, subject string, want Tier) bool {
	for _, m := range memos {
		if m.Subject == subject && m.Tier == want {
			return true
		}
	}
	return false
}

// TestMemo_JSONCollapsesHomePath locks in the §4.1 fix: JSON output for a
// session-tier memo must not embed the absolute home path. The Go struct keeps
// the absolute form for cache lookups; only the marshaled bytes carry tilde.
func TestMemo_JSONCollapsesHomePath(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	created := CreateMemo(dir, "session memo", "body", CreateMemoOptions{Tier: TierSession})
	if !created.Success {
		t.Fatalf("CreateMemo: %s", created.Error.Message)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	if !strings.Contains(created.Data.Repository, home) {
		t.Fatalf("expected absolute home path in Repository, got %q", created.Data.Repository)
	}

	raw, err := json.Marshal(created.Data)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	got := string(raw)
	if strings.Contains(got, home) {
		t.Errorf("JSON output leaks absolute home %q: %s", home, got)
	}
	if !strings.Contains(got, "local:~/") {
		t.Errorf("JSON output missing tilde-form: %s", got)
	}
}

// TestGetMemoItemByRef_acceptsTildeForm locks in the round-trip: a memo's
// JSON-exported ID (tilde-form) must look up the original memo when passed
// back to the API.
func TestGetMemoItemByRef_acceptsTildeForm(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	created := CreateMemo(dir, "round-trip", "body", CreateMemoOptions{Tier: TierSession})
	if !created.Success {
		t.Fatalf("CreateMemo: %s", created.Error.Message)
	}

	// Serialize → take the tilde-form ID → deserialize → re-lookup.
	raw, err := json.Marshal(created.Data)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var view struct {
		ID string `json:"ID"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !strings.HasPrefix(view.ID, "local:~/") {
		t.Fatalf("expected tilde-form ID, got %q", view.ID)
	}
	got := GetSingleMemo(view.ID, "", nil)
	if !got.Success {
		t.Fatalf("GetSingleMemo with tilde-form ID failed: %s", got.Error.Message)
	}
	if got.Data.Subject != "round-trip" {
		t.Errorf("subject mismatch: got %q", got.Data.Subject)
	}
}

// TestSessionInfo_JSONCollapsesHomePath verifies SessionInfo.MarshalJSON
// tilde-collapses Path on serialization while leaving the struct field
// absolute for internal callers (gc, sync).
func TestSessionInfo_JSONCollapsesHomePath(t *testing.T) {
	setupTestDB(t)
	freshHome(t)

	id := "tilde-session"
	if r := InitSession(id, ""); !r.Success {
		t.Fatalf("InitSession: %s", r.Error.Message)
	}
	listed := ListSessions("")
	if !listed.Success {
		t.Fatalf("ListSessions: %s", listed.Error.Message)
	}
	if len(listed.Data) == 0 {
		t.Fatal("no sessions listed")
	}

	home, _ := os.UserHomeDir()
	var info SessionInfo
	for _, s := range listed.Data {
		if s.ID == id {
			info = s
		}
	}
	if !strings.Contains(info.Path, home) {
		t.Errorf("expected absolute Path in struct, got %q", info.Path)
	}

	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(raw), home) {
		t.Errorf("JSON leaks home: %s", raw)
	}
	if !strings.Contains(string(raw), "~/") {
		t.Errorf("JSON missing tilde-form Path: %s", raw)
	}
}

// TestAddInherit_autoFollows locks in the §3.3 fix: `inherit add` ensures the
// URL is in the managed `memo-inherits` social list with --all-branches, so
// the next `gitsocial fetch` pulls the source's gitmsg/memo commits without
// requiring the user to run a separate `social list add`.
func TestAddInherit_autoFollows(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	url := protocol.NormalizeURL("https://example.com/policies.git")
	if r := AddInherit(dir, url); !r.Success || !r.Data {
		t.Fatalf("AddInherit: success=%v data=%v err=%+v", r.Success, r.Data, r.Error)
	}

	listRes := social.GetList(dir, InheritsListID)
	if !listRes.Success {
		t.Fatalf("GetList(memo-inherits): %s", listRes.Error.Message)
	}
	if listRes.Data == nil {
		t.Fatal("memo-inherits list was not created by AddInherit")
	}
	wantRef := url + "#branch:*"
	foundAllBranches := false
	for _, r := range listRes.Data.Repositories {
		if r == wantRef {
			foundAllBranches = true
		}
	}
	if !foundAllBranches {
		t.Errorf("memo-inherits list missing %q; got %v", wantRef, listRes.Data.Repositories)
	}

	// Idempotent re-add — list should not grow and no error.
	if r := AddInherit(dir, url); !r.Success || r.Data {
		t.Errorf("idempotent re-add: success=%v data=%v (want success=true, data=false)", r.Success, r.Data)
	}
	listRes2 := social.GetList(dir, InheritsListID)
	count := 0
	for _, r := range listRes2.Data.Repositories {
		if r == wantRef {
			count++
		}
	}
	if count != 1 {
		t.Errorf("ref appears %d times in memo-inherits list after re-add (want 1)", count)
	}
}

// TestRemoveInherit_removesFromList verifies the cleanup side of the auto-follow:
// `inherit remove` strips the URL from the memo-inherits list too.
func TestRemoveInherit_removesFromList(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	url := protocol.NormalizeURL("https://example.com/policies.git")
	if r := AddInherit(dir, url); !r.Success {
		t.Fatalf("AddInherit: %s", r.Error.Message)
	}
	if r := RemoveInherit(dir, url); !r.Success || !r.Data {
		t.Fatalf("RemoveInherit: success=%v data=%v err=%+v", r.Success, r.Data, r.Error)
	}

	listRes := social.GetList(dir, InheritsListID)
	if !listRes.Success || listRes.Data == nil {
		// List may exist as empty; either way, the URL shouldn't be in it.
		return
	}
	for _, r := range listRes.Data.Repositories {
		if strings.HasPrefix(r, url+"#") {
			t.Errorf("URL %q still in memo-inherits list after RemoveInherit: %q", url, r)
		}
	}
}

// TestMemoNotificationProvider_inheritedPolicy locks half of the §3.7 fix:
// a priority/critical memo on an inherited source surfaces as an
// inherited-policy notification, and is filtered out once read.
func TestMemoNotificationProvider_inheritedPolicy(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)
	if _, err := git.ExecGit(dir, []string{"config", "user.email", "me@example.com"}); err != nil {
		t.Fatalf("set user.email: %v", err)
	}

	inheritedURL := protocol.NormalizeURL("https://example.com/org-policies.git")
	if r := AddInherit(dir, inheritedURL); !r.Success {
		t.Fatalf("AddInherit: %s", r.Error.Message)
	}

	// Seed an inherited critical memo (someone else authored it).
	commitMsg := buildMemoContent("Encrypt at rest", "PII must be encrypted at rest",
		CreateMemoOptions{Labels: []string{"kind/policy", "priority/critical"}}, "")
	hash := "deadbeef0000deadbeef0000deadbeef00000001"
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: inheritedURL, Branch: MemoBranch,
		AuthorName: "Policy Bot", AuthorEmail: "bot@example.com",
		Message: commitMsg, Timestamp: time.Now(),
	}}); err != nil {
		t.Fatalf("InsertCommits: %v", err)
	}
	if msg := protocol.ParseMessage(commitMsg); msg != nil {
		cache.ProcessVersionFromHeader(msg, hash, inheritedURL, MemoBranch)
	}
	if err := InsertMemoItem(MemoItem{RepoURL: inheritedURL, Hash: hash, Branch: MemoBranch, Type: "memo"}); err != nil {
		t.Fatalf("InsertMemoItem: %v", err)
	}

	provider := &memoNotificationProvider{}
	notes, err := provider.GetNotifications(dir, notifications.Filter{UnreadOnly: true})
	if err != nil {
		t.Fatalf("GetNotifications: %v", err)
	}
	if len(notes) == 0 {
		t.Fatal("expected at least one inherited-policy notification, got none")
	}
	found := false
	for _, n := range notes {
		if n.Type == "inherited-policy" && n.Hash == hash {
			found = true
		}
	}
	if !found {
		t.Errorf("inherited-policy not surfaced for %s; got %+v", hash, notes)
	}

	// Marking the commit as read should drop it from UnreadOnly.
	if err := notifications.MarkAsRead(inheritedURL, hash, MemoBranch); err != nil {
		t.Fatalf("MarkAsRead: %v", err)
	}
	notes2, _ := provider.GetNotifications(dir, notifications.Filter{UnreadOnly: true})
	for _, n := range notes2 {
		if n.Hash == hash {
			t.Errorf("inherited-policy %s still in UnreadOnly after MarkNotificationsRead", hash)
		}
	}
}

// TestMemoNotificationProvider_commentOnMyMemo locks the other half: comments
// on memos the user authored surface as memo-comment notifications, even when
// the user hasn't engaged in the thread (the social provider misses this).
func TestMemoNotificationProvider_commentOnMyMemo(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)
	if _, err := git.ExecGit(dir, []string{"config", "user.email", "me@example.com"}); err != nil {
		t.Fatalf("set user.email: %v", err)
	}

	// Author a memo as "me".
	created := CreateMemo(dir, "my memo", "body", CreateMemoOptions{Tier: TierSession})
	if !created.Success {
		t.Fatalf("CreateMemo: %s", created.Error.Message)
	}
	parsed := protocol.ParseRef(created.Data.ID)
	memoHash := parsed.Value
	memoRepoURL := parsed.Repository
	memoBranch := parsed.Branch
	if memoBranch == "" {
		memoBranch = MemoBranch
	}

	// Patch the memo's author email so the JOIN finds it as "mine."
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE core_commits SET author_email = ?
			WHERE repo_url = ? AND hash = ? AND branch = ?`,
			"me@example.com", memoRepoURL, memoHash, memoBranch)
		return err
	}); err != nil {
		t.Fatalf("update memo author: %v", err)
	}

	// Synthesize a social comment on the memo from someone else.
	commentHash := "c0ffee0000c0ffee0000c0ffee0000c0ffee0001"
	commentMsg := "Looks good\n\n--- GitMsg ---\nExt: social\nV: 1.0.0\ntype: comment\nORIGINAL_REPO_URL: " + memoRepoURL + "\nORIGINAL_HASH: " + memoHash + "\nORIGINAL_BRANCH: " + memoBranch + "\n"
	workspaceURL := gitmsg.ResolveRepoURL(dir)
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: commentHash, RepoURL: workspaceURL, Branch: "gitmsg/social",
		AuthorName: "Other Person", AuthorEmail: "other@example.com",
		Message: commentMsg, Timestamp: time.Now(),
	}}); err != nil {
		t.Fatalf("InsertCommits comment: %v", err)
	}
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`INSERT INTO social_items
			(repo_url, hash, branch, type, original_repo_url, original_hash, original_branch)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			workspaceURL, commentHash, "gitmsg/social", "comment",
			memoRepoURL, memoHash, memoBranch)
		return err
	}); err != nil {
		t.Fatalf("insert social_items: %v", err)
	}

	provider := &memoNotificationProvider{}
	notes, err := provider.GetNotifications(dir, notifications.Filter{})
	if err != nil {
		t.Fatalf("GetNotifications: %v", err)
	}
	found := false
	for _, n := range notes {
		if n.Type == "memo-comment" && n.Hash == commentHash {
			found = true
		}
	}
	if !found {
		t.Errorf("memo-comment not surfaced for %s; got %d notes: %+v", commentHash, len(notes), notes)
	}
}

// TestSessionLastUsed_tracksCommitTime locks the §3.6 fix: a session's
// LastUsed in ListSessions reflects the latest gitmsg/memo commit timestamp,
// not the bare-repo directory mtime. Bumping the dir mtime (e.g., by touching
// it the way a backup would) must not appear as "recent activity."
func TestSessionLastUsed_tracksCommitTime(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	sessionID := "lastused-test"
	os.Setenv("MEMO_SESSION_ID", sessionID)
	t.Cleanup(func() { os.Setenv("MEMO_SESSION_ID", "memo-test-session") })

	beforeCreate := time.Now().Add(-1 * time.Minute)
	if r := CreateMemo(dir, "first", "body", CreateMemoOptions{Tier: TierSession}); !r.Success {
		t.Fatalf("CreateMemo: %s", r.Error.Message)
	}
	afterCreate := time.Now().Add(1 * time.Minute)

	listed := ListSessions("")
	if !listed.Success || len(listed.Data) == 0 {
		t.Fatalf("ListSessions: %+v", listed)
	}
	var info SessionInfo
	for _, s := range listed.Data {
		if s.ID == sessionID {
			info = s
		}
	}
	if info.ID == "" {
		t.Fatalf("session %q not in listed: %+v", sessionID, listed.Data)
	}
	if info.LastUsed.Before(beforeCreate) || info.LastUsed.After(afterCreate) {
		t.Errorf("LastUsed = %v, expected within [%v, %v]", info.LastUsed, beforeCreate, afterCreate)
	}

	// Simulate a backup or `cp -p` bumping the dir mtime far into the future.
	// LastUsed must remain anchored to the commit time, not the file mtime.
	path, err := SessionRepoPath(sessionID)
	if err != nil {
		t.Fatalf("SessionRepoPath: %v", err)
	}
	future := time.Now().Add(48 * time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	listed2 := ListSessions("")
	if !listed2.Success {
		t.Fatalf("ListSessions: %s", listed2.Error.Message)
	}
	for _, s := range listed2.Data {
		if s.ID == sessionID {
			if s.LastUsed.After(afterCreate) {
				t.Errorf("LastUsed jumped to %v after mtime touch — should still reflect commit time", s.LastUsed)
			}
		}
	}
}

// TestInheritsStatus_reportsFreshness locks the §3.5 fix: InheritsStatus
// joins inherited URLs with core_repositories to surface follow-state and
// last-fetch time so `memo status` can warn about stale or unfollowed sources.
func TestInheritsStatus_reportsFreshness(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	// Empty case.
	if got := InheritsStatus(dir); len(got) != 0 {
		t.Errorf("InheritsStatus with no inherits returned %d items", len(got))
	}

	urlFollowed := protocol.NormalizeURL("https://example.com/policies.git")
	urlNotFollowed := protocol.NormalizeURL("https://example.com/legacy.git")

	if r := AddInherit(dir, urlFollowed); !r.Success {
		t.Fatalf("AddInherit(followed): %s", r.Error.Message)
	}
	// Add a legacy inherit by writing the ref directly without going through
	// AddInherit's auto-follow (simulates an inherit added before §3.3 fix).
	ref := inheritRefPath(urlNotFollowed)
	hash, err := git.CreateCommitTree(dir, urlNotFollowed+"\n", "")
	if err != nil {
		t.Fatalf("legacy commit-tree: %v", err)
	}
	if err := git.WriteRef(dir, ref, hash); err != nil {
		t.Fatalf("legacy ref write: %v", err)
	}

	// Record a fetch time for the followed URL via cache.InsertRepository.
	fetchTime := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`INSERT OR REPLACE INTO core_repositories
			(url, branch, storage_path, last_fetch) VALUES (?, ?, ?, ?)`,
			urlFollowed, "main", "/tmp/test", fetchTime)
		return err
	}); err != nil {
		t.Fatalf("seed core_repositories: %v", err)
	}

	statuses := InheritsStatus(dir)
	if len(statuses) != 2 {
		t.Fatalf("InheritsStatus = %d items, want 2", len(statuses))
	}
	byURL := map[string]InheritStatus{}
	for _, s := range statuses {
		byURL[s.URL] = s
	}
	if s := byURL[urlFollowed]; !s.Followed {
		t.Errorf("%s should report Followed=true", urlFollowed)
	} else if s.LastFetch.IsZero() {
		t.Errorf("%s should have LastFetch populated", urlFollowed)
	} else if time.Since(s.LastFetch) < time.Hour {
		t.Errorf("LastFetch %v unexpectedly recent", s.LastFetch)
	}
	if s := byURL[urlNotFollowed]; s.Followed {
		t.Errorf("%s should report Followed=false (legacy inherit, no list entry)", urlNotFollowed)
	}
}

// TestProcessors_populatesMemoItems locks in the followed-repo fetch wire-up:
// the CommitProcessor returned by Processors() must turn a `gitmsg/memo`
// commit on an arbitrary repo URL into a memo_items row, so inherited and
// external memos surface through `memo list --tier inherited`.
//
// Pre-fix: the memo extension only registered a WorkspaceSyncFunc (covers the
// workspace itself), not a CommitProcessor — so commits on followed remote
// repos landed in core_commits but never produced a memo_items row, and the
// memo_items_resolved INNER JOIN dropped them.
func TestProcessors_populatesMemoItems(t *testing.T) {
	setupTestDB(t)
	freshHome(t)

	procs := Processors()
	if len(procs) == 0 {
		t.Fatal("Processors() returned empty list")
	}

	remoteURL := "https://example.com/inherited-policies"
	hash := "1234567890ab1234567890ab1234567890ab1234"
	commitMsg := buildMemoContent("Inherited policy", "Body",
		CreateMemoOptions{Labels: []string{"kind/policy", "priority/critical"}}, "")

	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        hash,
		RepoURL:     remoteURL,
		Branch:      MemoBranch,
		AuthorName:  "Org Bot",
		AuthorEmail: "bot@example.com",
		Message:     commitMsg,
		Timestamp:   time.Now(),
	}}); err != nil {
		t.Fatalf("InsertCommits: %v", err)
	}

	gc := git.Commit{
		Hash:    hash,
		Author:  "Org Bot",
		Email:   "bot@example.com",
		Message: commitMsg,
	}
	msg := protocol.ParseMessage(commitMsg)
	if msg == nil {
		t.Fatal("ParseMessage returned nil for synthetic memo commit")
	}
	for _, proc := range procs {
		proc(gc, msg, remoteURL, MemoBranch)
	}

	item, err := GetMemoItem(remoteURL, hash, MemoBranch)
	if err != nil {
		t.Fatalf("GetMemoItem after processor run: %v", err)
	}
	if item == nil {
		t.Fatal("memo_items row not created by Processors()")
	}
	if item.Type != "memo" {
		t.Errorf("memo_items.type = %q, want memo", item.Type)
	}
}

// TestProcessors_skipsNonMemoCommits ensures the memo CommitProcessor doesn't
// touch commits from other extensions (so it can sit in a shared processor
// list without corrupting other extensions' rows).
func TestProcessors_skipsNonMemoCommits(t *testing.T) {
	setupTestDB(t)
	freshHome(t)

	procs := Processors()
	remoteURL := "https://example.com/other"
	hash := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	// A commit with a non-memo header — e.g., a social post.
	socialMsg := "Hello\n\n--- GitMsg ---\nExt: social\nV: 1.0.0\n"

	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        hash,
		RepoURL:     remoteURL,
		Branch:      "gitmsg/social",
		AuthorName:  "Alice",
		AuthorEmail: "alice@example.com",
		Message:     socialMsg,
		Timestamp:   time.Now(),
	}}); err != nil {
		t.Fatalf("InsertCommits: %v", err)
	}
	gc := git.Commit{Hash: hash, Author: "Alice", Email: "alice@example.com", Message: socialMsg}
	msg := protocol.ParseMessage(socialMsg)
	for _, proc := range procs {
		proc(gc, msg, remoteURL, "gitmsg/social")
	}

	// memo_items must remain untouched.
	item, _ := GetMemoItem(remoteURL, hash, "gitmsg/social")
	if item != nil {
		t.Errorf("memo CommitProcessor created memo_items row for non-memo commit: %+v", item)
	}
}

// TestCollapseExpand_roundTrip covers the path helpers directly.
func TestCollapseExpand_roundTrip(t *testing.T) {
	freshHome(t)
	home, _ := os.UserHomeDir()
	cases := []string{
		"local:" + home + "/.cache/gitsocial/memo/session/foo",
		"local:" + home,
		"local:" + home + "/x",
		"https://example.com/repo.git",
		"",
	}
	for _, in := range cases {
		collapsed := CollapseLocalURL(in)
		round := ExpandLocalURL(collapsed)
		if round != in {
			t.Errorf("round-trip %q → %q → %q", in, collapsed, round)
		}
	}
	// A non-home local path is returned unchanged.
	if got := CollapseLocalURL("local:/etc/gitsocial/something"); got != "local:/etc/gitsocial/something" {
		t.Errorf("non-home path collapsed: %q", got)
	}
}
