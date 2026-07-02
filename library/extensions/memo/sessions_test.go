// sessions_test.go - Session/tier lifecycle tests: gc-by-age, push/fetch, tier sync
package memo

import (
	"os"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
)

func TestGCSessionsOlderThan_deletesOnlyStale(t *testing.T) {
	setupTestDB(t)
	freshHome(t)

	if res := InitSession("gc-old", ""); !res.Success {
		t.Fatalf("InitSession old: %s", res.Error.Message)
	}
	if res := InitSession("gc-fresh", ""); !res.Success {
		t.Fatalf("InitSession fresh: %s", res.Error.Message)
	}
	// No commits on either session, so last-used falls back to dir mtime.
	oldPath, err := SessionRepoPath("gc-old")
	if err != nil {
		t.Fatalf("SessionRepoPath: %v", err)
	}
	stale := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(oldPath, stale, stale); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	res := GCSessionsOlderThan(7 * 24 * time.Hour)
	if !res.Success {
		t.Fatalf("GCSessionsOlderThan: %s", res.Error.Message)
	}
	if len(res.Data) != 1 || res.Data[0] != "gc-old" {
		t.Errorf("deleted = %v, want [gc-old]", res.Data)
	}
	if git.BareRepoExists(oldPath) {
		t.Error("stale session repo still exists after gc")
	}
	freshPath, _ := SessionRepoPath("gc-fresh")
	if !git.BareRepoExists(freshPath) {
		t.Error("fresh session repo was deleted by gc")
	}
}

func TestPushSession_errors(t *testing.T) {
	setupTestDB(t)
	freshHome(t)

	if res := PushSession(""); res.Success || res.Error.Code != "INVALID_ARGS" {
		t.Errorf("PushSession(\"\") = %+v, want INVALID_ARGS", res.Error)
	}
	if res := PushSession("no-such-session"); res.Success || res.Error.Code != "NOT_FOUND" {
		t.Errorf("PushSession(missing) = %+v, want NOT_FOUND", res.Error)
	}
	if res := InitSession("push-no-remote", ""); !res.Success {
		t.Fatalf("InitSession: %s", res.Error.Message)
	}
	if res := PushSession("push-no-remote"); res.Success || res.Error.Code != "NO_REMOTE" {
		t.Errorf("PushSession(no remote) = %+v, want NO_REMOTE", res.Error)
	}
}

func TestFetchSession_errors(t *testing.T) {
	setupTestDB(t)
	freshHome(t)

	if res := FetchSession(""); res.Success || res.Error.Code != "INVALID_ARGS" {
		t.Errorf("FetchSession(\"\") = %+v, want INVALID_ARGS", res.Error)
	}
	if res := FetchSession("no-such-session"); res.Success || res.Error.Code != "NOT_FOUND" {
		t.Errorf("FetchSession(missing) = %+v, want NOT_FOUND", res.Error)
	}
	if res := InitSession("fetch-no-remote", ""); !res.Success {
		t.Fatalf("InitSession: %s", res.Error.Message)
	}
	if res := FetchSession("fetch-no-remote"); res.Success || res.Error.Code != "NO_REMOTE" {
		t.Errorf("FetchSession(no remote) = %+v, want NO_REMOTE", res.Error)
	}
}

// TestFetchSession_remoteGone exercises the fetch-failure path: a configured
// remote that no longer exists must surface FETCH_FAILED, not silence.
func TestFetchSession_remoteGone(t *testing.T) {
	setupTestDB(t)
	freshHome(t)

	if res := InitSession("fetch-bad-remote", ""); !res.Success {
		t.Fatalf("InitSession: %s", res.Error.Message)
	}
	path, err := SessionRepoPath("fetch-bad-remote")
	if err != nil {
		t.Fatalf("SessionRepoPath: %v", err)
	}
	gone := t.TempDir() + "/no-such-remote.git"
	if _, err := git.ExecGit(path, []string{"remote", "add", "origin", gone}); err != nil {
		t.Fatalf("remote add: %v", err)
	}
	if res := FetchSession("fetch-bad-remote"); res.Success || res.Error.Code != "FETCH_FAILED" {
		t.Errorf("FetchSession(remote gone) = %+v, want FETCH_FAILED", res.Error)
	}
}

// TestSessionPushFetch_roundTrip pushes a session memo to a local bare remote
// and fetches it back, covering the push/fetch happy paths without network.
func TestSessionPushFetch_roundTrip(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	sessionID := "push-fetch-roundtrip"
	os.Setenv("MEMO_SESSION_ID", sessionID)
	t.Cleanup(func() { os.Setenv("MEMO_SESSION_ID", "memo-test-session") })

	created := CreateMemo(dir, "roundtrip memo", "body", CreateMemoOptions{Tier: TierSession})
	if !created.Success {
		t.Fatalf("CreateMemo: %s", created.Error.Message)
	}

	remote := t.TempDir()
	if _, err := git.ExecGit(remote, []string{"init", "--bare"}); err != nil {
		t.Fatalf("init bare remote: %v", err)
	}
	path, err := SessionRepoPath(sessionID)
	if err != nil {
		t.Fatalf("SessionRepoPath: %v", err)
	}
	if _, err := git.ExecGit(path, []string{"remote", "add", "origin", remote}); err != nil {
		t.Fatalf("remote add: %v", err)
	}

	if res := PushSession(sessionID); !res.Success {
		t.Fatalf("PushSession: %s", res.Error.Message)
	}
	if tip, err := git.ReadRef(remote, MemoBranch); err != nil || tip == "" {
		t.Errorf("remote %s tip missing after push (err %v)", MemoBranch, err)
	}
	if res := FetchSession(sessionID); !res.Success {
		t.Fatalf("FetchSession: %s", res.Error.Message)
	}
}

func TestPersonalPushFetch_notInitialized(t *testing.T) {
	setupTestDB(t)
	freshHome(t)

	if res := PushPersonal(); res.Success || res.Error.Code != "NOT_INITIALIZED" {
		t.Errorf("PushPersonal() = %+v, want NOT_INITIALIZED", res.Error)
	}
	if res := FetchPersonal(); res.Success || res.Error.Code != "NOT_INITIALIZED" {
		t.Errorf("FetchPersonal() = %+v, want NOT_INITIALIZED", res.Error)
	}
}

// TestSyncAllTierReposToCache_syncsWorkspaceAndSession verifies the tier sweep
// indexes the workspace and session repos and silently skips the missing
// personal repo.
func TestSyncAllTierReposToCache_syncsWorkspaceAndSession(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	dir := initTestRepo(t)

	sessionID := "tier-sweep-session"
	os.Setenv("MEMO_SESSION_ID", sessionID)
	t.Cleanup(func() { os.Setenv("MEMO_SESSION_ID", "memo-test-session") })

	if r := InitProject(dir); !r.Success {
		t.Fatalf("InitProject: %s", r.Error.Message)
	}
	if r := CreateMemo(dir, "workspace memo", "", CreateMemoOptions{Tier: TierProject}); !r.Success {
		t.Fatalf("CreateMemo project: %s", r.Error.Message)
	}
	if r := CreateMemo(dir, "session memo", "", CreateMemoOptions{Tier: TierSession}); !r.Success {
		t.Fatalf("CreateMemo session: %s", r.Error.Message)
	}

	if err := SyncAllTierReposToCache(dir); err != nil {
		t.Fatalf("SyncAllTierReposToCache: %v", err)
	}

	workspaceURL := gitmsg.ResolveRepoURL(dir)
	wsItems, err := GetMemoItems(MemoQuery{RepoURL: workspaceURL, Branch: MemoBranch})
	if err != nil || len(wsItems) == 0 {
		t.Errorf("workspace memos not in cache after sweep (err %v, n=%d)", err, len(wsItems))
	}
	path, _ := SessionRepoPath(sessionID)
	sessItems, err := GetMemoItems(MemoQuery{RepoURL: LocalRepoURL(path), Branch: MemoBranch})
	if err != nil || len(sessItems) == 0 {
		t.Errorf("session memos not in cache after sweep (err %v, n=%d)", err, len(sessItems))
	}
}

func TestFormatAge(t *testing.T) {
	if got := FormatAge(time.Time{}); got != "?" {
		t.Errorf("FormatAge(zero) = %q, want ?", got)
	}
	cases := []struct {
		age  time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{49 * time.Hour, "2d"},
	}
	for _, c := range cases {
		if got := FormatAge(time.Now().Add(-c.age)); got != c.want {
			t.Errorf("FormatAge(-%v) = %q, want %q", c.age, got, c.want)
		}
	}
}
