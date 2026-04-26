// fetch_test.go - Tests for fetch orchestration and commit processing
package fetch

import (
	"database/sql"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

var (
	baseRepoDir    string
	sharedCacheDir string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fetch-test-base-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	if err := git.Init(dir, "main"); err != nil {
		panic(err)
	}
	git.ExecGit(dir, []string{"config", "user.email", "test@test.com"})
	git.ExecGit(dir, []string{"config", "user.name", "Test User"})
	for i := 0; i < 3; i++ {
		if _, err := git.CreateCommit(dir, git.CommitOptions{
			Message:    "commit " + string(rune('A'+i)),
			AllowEmpty: true,
		}); err != nil {
			panic(err)
		}
	}
	baseRepoDir = dir

	cacheDir, err := os.MkdirTemp("", "fetch-test-cache-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(cacheDir)
	if err := cache.Open(cacheDir); err != nil {
		panic(err)
	}
	sharedCacheDir = cacheDir

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

// setupTestCache initializes a fresh cache database in a temp directory.
func setupTestCache(t *testing.T) {
	t.Helper()
	cache.Reset()
	if err := cache.Open(t.TempDir()); err != nil {
		t.Fatalf("cache.Open() error = %v", err)
	}
	t.Cleanup(func() { cache.Reset() })
}

// initTestRepo creates a git repo with N commits, returns dir and commit list (newest first).
func initTestRepo(t *testing.T, commitCount int) (string, []git.Commit) {
	t.Helper()
	var dir string
	if commitCount == 3 {
		dir = cloneFixture(t)
	} else {
		dir = t.TempDir()
		if err := git.Init(dir, "main"); err != nil {
			t.Fatalf("git.Init() error = %v", err)
		}
		git.ExecGit(dir, []string{"config", "user.email", "test@test.com"})
		git.ExecGit(dir, []string{"config", "user.name", "Test User"})
		for i := 0; i < commitCount; i++ {
			if _, err := git.CreateCommit(dir, git.CommitOptions{
				Message:    "commit " + string(rune('A'+i)),
				AllowEmpty: true,
			}); err != nil {
				t.Fatalf("CreateCommit(%d) error = %v", i, err)
			}
		}
	}
	commits, err := git.GetCommits(dir, &git.GetCommitsOptions{All: true})
	if err != nil {
		t.Fatalf("GetCommits() error = %v", err)
	}
	return dir, commits
}

// --- DedupeRepos tests (existing) ---

func TestDedupeRepos(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		repos []RepoInfo
		want  int
	}{
		{
			name:  "no duplicates",
			repos: []RepoInfo{{URL: "a", Branch: "main"}, {URL: "b", Branch: "main"}},
			want:  2,
		},
		{
			name:  "duplicate url+branch",
			repos: []RepoInfo{{URL: "a", Branch: "main"}, {URL: "a", Branch: "main"}},
			want:  1,
		},
		{
			name:  "same url different branch",
			repos: []RepoInfo{{URL: "a", Branch: "main"}, {URL: "a", Branch: "develop"}},
			want:  2,
		},
		{
			name:  "empty input",
			repos: nil,
			want:  0,
		},
		{
			name: "multiple duplicates",
			repos: []RepoInfo{
				{URL: "a", Branch: "main"},
				{URL: "b", Branch: "main"},
				{URL: "a", Branch: "main"},
				{URL: "b", Branch: "main"},
				{URL: "c", Branch: "main"},
			},
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DedupeRepos(tt.repos)
			if len(got) != tt.want {
				t.Errorf("DedupeRepos() returned %d items, want %d", len(got), tt.want)
			}
		})
	}
}

func TestDedupeRepos_preservesOrder(t *testing.T) {
	t.Parallel()
	repos := []RepoInfo{
		{URL: "c", Branch: "main"},
		{URL: "a", Branch: "main"},
		{URL: "b", Branch: "main"},
		{URL: "a", Branch: "main"},
	}

	got := DedupeRepos(repos)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].URL != "c" || got[1].URL != "a" || got[2].URL != "b" {
		t.Errorf("Order not preserved: %v", got)
	}
}

// --- processCommits tests ---

func TestProcessCommits_insertsNewCommits(t *testing.T) {
	t.Parallel()
	repoURL := "https://example.com/test/pc-insert"
	branch := "main"
	now := time.Now()
	commits := []git.Commit{
		{Hash: "aaa111", Message: "first", Author: "Alice", Email: "alice@test.com", Timestamp: now},
		{Hash: "bbb222", Message: "second", Author: "Bob", Email: "bob@test.com", Timestamp: now},
	}

	count, err := ProcessCommits("", commits, repoURL, branch, nil)
	if err != nil {
		t.Fatalf("ProcessCommits() error = %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	// Verify commits are in cache
	dbCount, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow("SELECT COUNT(*) FROM core_commits WHERE repo_url = ?", repoURL).Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("QueryLocked() error = %v", err)
	}
	if dbCount != 2 {
		t.Errorf("db count = %d, want 2", dbCount)
	}
}

func TestProcessCommits_filtersAlreadyFetched(t *testing.T) {
	t.Parallel()
	repoURL := "https://example.com/test/pc-filter"
	branch := "main"
	now := time.Now()

	// Pre-insert commits
	if err := cache.InsertCommits([]cache.Commit{
		{Hash: "aaa111", RepoURL: repoURL, Branch: branch, AuthorName: "Alice", AuthorEmail: "alice@test.com", Message: "first", Timestamp: now},
		{Hash: "bbb222", RepoURL: repoURL, Branch: branch, AuthorName: "Bob", AuthorEmail: "bob@test.com", Message: "second", Timestamp: now},
	}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}

	gitCommits := []git.Commit{
		{Hash: "aaa111", Message: "first", Author: "Alice", Email: "alice@test.com", Timestamp: now},
		{Hash: "bbb222", Message: "second", Author: "Bob", Email: "bob@test.com", Timestamp: now},
	}

	count, err := ProcessCommits("", gitCommits, repoURL, branch, nil)
	if err != nil {
		t.Fatalf("ProcessCommits() error = %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 (all already fetched)", count)
	}
}

func TestProcessCommits_callsProcessors(t *testing.T) {
	t.Parallel()
	repoURL := "https://example.com/test/pc-procs"
	branch := "main"
	now := time.Now()
	commits := []git.Commit{
		{Hash: "aaa111", Message: "first", Author: "Alice", Email: "alice@test.com", Timestamp: now},
		{Hash: "bbb222", Message: "second", Author: "Bob", Email: "bob@test.com", Timestamp: now},
	}

	var mu sync.Mutex
	var calls []string
	proc := func(commit git.Commit, msg *protocol.Message, rURL, b string) {
		mu.Lock()
		calls = append(calls, commit.Hash)
		mu.Unlock()
	}

	count, err := ProcessCommits("", commits, repoURL, branch, []CommitProcessor{proc})
	if err != nil {
		t.Fatalf("ProcessCommits() error = %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if len(calls) != 2 {
		t.Errorf("processor called %d times, want 2", len(calls))
	}
}

func TestProcessCommits_parsesGitMsgHeaders(t *testing.T) {
	t.Parallel()
	repoURL := "https://example.com/test/pc-headers"
	branch := "main"
	now := time.Now()

	gitmsgMessage := protocol.FormatMessage("Hello world", protocol.Header{
		Ext:    "social",
		V:      "1",
		Fields: map[string]string{"type": "post"},
	}, nil)

	commits := []git.Commit{
		{Hash: "aaa111", Message: gitmsgMessage, Author: "Alice", Email: "alice@test.com", Timestamp: now},
	}

	var receivedMsg *protocol.Message
	proc := func(commit git.Commit, msg *protocol.Message, rURL, b string) {
		receivedMsg = msg
	}

	_, err := ProcessCommits("", commits, repoURL, branch, []CommitProcessor{proc})
	if err != nil {
		t.Fatalf("ProcessCommits() error = %v", err)
	}
	if receivedMsg == nil {
		t.Fatal("processor received nil msg for GitMsg commit")
	}
	if receivedMsg.Header.Ext != "social" {
		t.Errorf("header ext = %q, want social", receivedMsg.Header.Ext)
	}
	if receivedMsg.Header.Fields["type"] != "post" {
		t.Errorf("header type = %q, want post", receivedMsg.Header.Fields["type"])
	}
}

func TestProcessCommits_nilMsgForPlainCommits(t *testing.T) {
	t.Parallel()
	repoURL := "https://example.com/test/pc-nilmsg"
	branch := "main"
	now := time.Now()

	commits := []git.Commit{
		{Hash: "aaa111", Message: "just a regular commit", Author: "Alice", Email: "alice@test.com", Timestamp: now},
	}

	var receivedMsg *protocol.Message
	proc := func(commit git.Commit, msg *protocol.Message, rURL, b string) {
		receivedMsg = msg
	}

	_, err := ProcessCommits("", commits, repoURL, branch, []CommitProcessor{proc})
	if err != nil {
		t.Fatalf("ProcessCommits() error = %v", err)
	}
	if receivedMsg != nil {
		t.Errorf("processor received non-nil msg for plain commit: %+v", receivedMsg)
	}
}

func TestProcessCommits_emptySlice(t *testing.T) {
	t.Parallel()

	count, err := ProcessCommits("", nil, "https://example.com/test/pc-empty", "main", nil)
	if err != nil {
		t.Fatalf("ProcessCommits() error = %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestProcessCommits_multipleProcessors(t *testing.T) {
	t.Parallel()
	repoURL := "https://example.com/test/pc-multiproc"
	branch := "main"
	now := time.Now()
	commits := []git.Commit{
		{Hash: "aaa111", Message: "commit", Author: "Alice", Email: "alice@test.com", Timestamp: now},
	}

	var calls1, calls2 int
	proc1 := func(commit git.Commit, msg *protocol.Message, rURL, b string) { calls1++ }
	proc2 := func(commit git.Commit, msg *protocol.Message, rURL, b string) { calls2++ }

	_, err := ProcessCommits("", commits, repoURL, branch, []CommitProcessor{proc1, proc2})
	if err != nil {
		t.Fatalf("ProcessCommits() error = %v", err)
	}
	if calls1 != 1 || calls2 != 1 {
		t.Errorf("proc1 calls = %d, proc2 calls = %d, want 1 each", calls1, calls2)
	}
}

func TestProcessCommits_passesCorrectArgs(t *testing.T) {
	t.Parallel()
	repoURL := "https://example.com/test/pc-args"
	branch := "main"
	now := time.Now()
	commits := []git.Commit{
		{Hash: "aaa111", Message: "test", Author: "Alice", Email: "alice@test.com", Timestamp: now},
	}

	var gotRepoURL, gotBranch, gotHash string
	proc := func(commit git.Commit, msg *protocol.Message, rURL, b string) {
		gotRepoURL = rURL
		gotBranch = b
		gotHash = commit.Hash
	}

	_, err := ProcessCommits("", commits, repoURL, branch, []CommitProcessor{proc})
	if err != nil {
		t.Fatalf("ProcessCommits() error = %v", err)
	}
	if gotRepoURL != repoURL {
		t.Errorf("repoURL = %q, want %q", gotRepoURL, repoURL)
	}
	if gotBranch != branch {
		t.Errorf("branch = %q, want %q", gotBranch, branch)
	}
	if gotHash != "aaa111" {
		t.Errorf("hash = %q, want aaa111", gotHash)
	}
}

// --- runHooks tests ---

func TestRunHooks(t *testing.T) {
	t.Parallel()
	var calls []string
	hook1 := func(storageDir, repoURL, branch, workspaceURL string) {
		calls = append(calls, "hook1:"+repoURL)
	}
	hook2 := func(storageDir, repoURL, branch, workspaceURL string) {
		calls = append(calls, "hook2:"+repoURL)
	}

	runHooks([]PostFetchHook{hook1, hook2}, "/storage", "https://example.com/repo", "main", "https://workspace.com")

	if len(calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2", len(calls))
	}
	if calls[0] != "hook1:https://example.com/repo" {
		t.Errorf("calls[0] = %q", calls[0])
	}
	if calls[1] != "hook2:https://example.com/repo" {
		t.Errorf("calls[1] = %q", calls[1])
	}
}

func TestRunHooks_passesAllArgs(t *testing.T) {
	t.Parallel()
	var gotStorage, gotRepo, gotBranch, gotWorkspace string
	hook := func(storageDir, repoURL, branch, workspaceURL string) {
		gotStorage = storageDir
		gotRepo = repoURL
		gotBranch = branch
		gotWorkspace = workspaceURL
	}

	runHooks([]PostFetchHook{hook}, "/store", "https://repo.com", "dev", "https://ws.com")

	if gotStorage != "/store" {
		t.Errorf("storageDir = %q", gotStorage)
	}
	if gotRepo != "https://repo.com" {
		t.Errorf("repoURL = %q", gotRepo)
	}
	if gotBranch != "dev" {
		t.Errorf("branch = %q", gotBranch)
	}
	if gotWorkspace != "https://ws.com" {
		t.Errorf("workspaceURL = %q", gotWorkspace)
	}
}

func TestRunHooks_empty(t *testing.T) {
	t.Parallel()
	// nil hooks should not panic
	runHooks(nil, "/storage", "https://example.com/repo", "main", "")
	// empty slice should not panic
	runHooks([]PostFetchHook{}, "/storage", "https://example.com/repo", "main", "")
}

// --- DB-corrupting error path tests (sequential, each with fresh cache) ---

func TestCacheErrorPaths(t *testing.T) {
	t.Run("ProcessCommits_filterError", func(t *testing.T) {
		setupTestCache(t)
		// Rename core_commits to make FilterUnfetchedCommits fail
		cache.ExecLocked(func(db *sql.DB) error {
			_, err := db.Exec("ALTER TABLE core_commits RENAME TO core_commits_disabled")
			return err
		})
		commits := []git.Commit{
			{Hash: "aaa111", Message: "test", Author: "Alice", Email: "alice@test.com", Timestamp: time.Now()},
		}
		_, err := ProcessCommits("", commits, "https://example.com/repo", "main", nil)
		if err == nil {
			t.Error("expected error after renaming core_commits")
		}
	})
	t.Run("ProcessCommits_insertError", func(t *testing.T) {
		setupTestCache(t)
		// Block INSERT on core_commits to make InsertCommits fail
		cache.ExecLocked(func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TRIGGER block_commit_insert BEFORE INSERT ON core_commits BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
			return err
		})
		commits := []git.Commit{
			{Hash: "aaa111", Message: "test", Author: "Alice", Email: "alice@test.com", Timestamp: time.Now()},
		}
		_, err := ProcessCommits("", commits, "https://example.com/repo", "main", nil)
		if err == nil {
			t.Error("expected error with insert trigger")
		}
	})
	t.Run("FetchFullHistory_processError", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping integration test")
		}
		setupTestCache(t)
		dir, _ := initTestRepo(t, 2)
		cache.ExecLocked(func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TRIGGER block_commit_insert BEFORE INSERT ON core_commits BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
			return err
		})
		_, err := fetchFullHistory(dir, "https://example.com/proc-err", "main", nil)
		if err == nil {
			t.Error("expected error from processCommits")
		}
	})
	t.Run("FetchIncremental_processError", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping integration test")
		}
		setupTestCache(t)
		dir, _ := initTestRepo(t, 2)
		cache.ExecLocked(func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TRIGGER block_commit_insert BEFORE INSERT ON core_commits BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
			return err
		})
		sinceTime := time.Now().AddDate(0, 0, -1)
		_, err := fetchIncremental(dir, "https://example.com/incr-err", "main", sinceTime, nil)
		if err == nil {
			t.Error("expected error from processCommits")
		}
	})
	t.Run("Fetch30DayWindow_processError", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping integration test")
		}
		setupTestCache(t)
		dir, _ := initTestRepo(t, 2)
		cache.ExecLocked(func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TRIGGER block_commit_insert BEFORE INSERT ON core_commits BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
			return err
		})
		since := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
		_, err := fetch30DayWindow(dir, "https://example.com/win-err", "main", since, "", nil)
		if err == nil {
			t.Error("expected error from processCommits")
		}
	})
	t.Run("FetchRepository_getMetaError", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping integration test")
		}
		setupTestCache(t)
		// Rename core_commits to make GetRepositoryFetchMeta fail
		cache.ExecLocked(func(db *sql.DB) error {
			_, err := db.Exec("ALTER TABLE core_commits RENAME TO core_commits_disabled")
			return err
		})
		repoDir, _ := initTestRepo(t, 1)
		bareDir := t.TempDir()
		git.ExecGit(bareDir, []string{"init", "--bare"})
		git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
		git.ExecGit(repoDir, []string{"push", "origin", "main"})
		cacheDir := t.TempDir()
		_, err := fetchRepository(cacheDir, bareDir, "main", false, "", "", "", nil, nil)
		if err == nil {
			t.Error("expected error from GetRepositoryFetchMeta")
		}
	})
	t.Run("FetchRepository_errorPropagation", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping integration test")
		}
		setupTestCache(t)
		repoDir, _ := initTestRepo(t, 2)
		bareDir := t.TempDir()
		git.ExecGit(bareDir, []string{"init", "--bare"})
		git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
		git.ExecGit(repoDir, []string{"push", "origin", "main"})
		// Block INSERT on core_commits: processCommits fails → error propagates through fetchRepository
		cache.ExecLocked(func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TRIGGER block_commit_insert BEFORE INSERT ON core_commits BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
			return err
		})
		cacheDir := t.TempDir()
		// isFollowed=true, no prior commits → full history → processCommits → InsertCommits fails
		_, err := fetchRepository(cacheDir, bareDir, "main", true, "", "", "", nil, nil)
		if err == nil {
			t.Error("expected error propagated from processCommits")
		}
	})
	t.Run("FetchRepository_processError", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping integration test")
		}
		setupTestCache(t)
		repoDir, _ := initTestRepo(t, 1)
		bareDir := t.TempDir()
		git.ExecGit(bareDir, []string{"init", "--bare"})
		git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
		git.ExecGit(repoDir, []string{"push", "origin", "main"})
		// Block INSERT on core_commits → fetchFullHistory → processCommits fails → PROCESS_ERROR
		cache.ExecLocked(func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TRIGGER block_commit_insert BEFORE INSERT ON core_commits BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
			return err
		})
		cacheDir := t.TempDir()
		res := FetchRepository(cacheDir, bareDir, "main", "", nil, nil)
		if res.Success {
			t.Error("expected failure")
		}
		if res.Error.Code != "PROCESS_ERROR" {
			t.Errorf("code = %q, want PROCESS_ERROR", res.Error.Code)
		}
	})
	t.Run("FetchRepository_cacheLogErrors", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping integration test")
		}
		setupTestCache(t)
		repoDir, _ := initTestRepo(t, 1)
		bareDir := t.TempDir()
		git.ExecGit(bareDir, []string{"init", "--bare"})
		git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
		git.ExecGit(repoDir, []string{"push", "origin", "main"})
		// Block INSERT/UPDATE on core_repositories to make InsertRepository and UpdateLastFetch fail (logged only)
		cache.ExecLocked(func(db *sql.DB) error {
			db.Exec(`CREATE TRIGGER block_repo_insert BEFORE INSERT ON core_repositories BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
			_, err := db.Exec(`CREATE TRIGGER block_repo_update BEFORE UPDATE ON core_repositories BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
			return err
		})
		cacheDir := t.TempDir()
		res := FetchRepository(cacheDir, bareDir, "main", "", nil, nil)
		if !res.Success {
			t.Errorf("FetchRepository() should succeed despite cache log errors: %v", res.Error)
		}
	})
	t.Run("FetchRepository_internal_cacheLogErrors", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping integration test")
		}
		setupTestCache(t)
		repoDir, _ := initTestRepo(t, 1)
		bareDir := t.TempDir()
		git.ExecGit(bareDir, []string{"init", "--bare"})
		git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
		git.ExecGit(repoDir, []string{"push", "origin", "main"})
		// Block INSERT/UPDATE on core_repositories
		cache.ExecLocked(func(db *sql.DB) error {
			db.Exec(`CREATE TRIGGER block_repo_insert BEFORE INSERT ON core_repositories BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
			_, err := db.Exec(`CREATE TRIGGER block_repo_update BEFORE UPDATE ON core_repositories BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
			return err
		})
		cacheDir := t.TempDir()
		// isFollowed=true → full history path (uses nil fetch options, avoids --shallow-since issues)
		_, err := fetchRepository(cacheDir, bareDir, "main", true, "", "", "", nil, nil)
		if err != nil {
			t.Errorf("fetchRepository should succeed despite cache log errors: %v", err)
		}
	})
	t.Run("FetchAll_workspaceCacheErrors", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping integration test")
		}
		setupTestCache(t)
		repoDir, _ := initTestRepo(t, 2)
		bareDir := t.TempDir()
		git.ExecGit(bareDir, []string{"init", "--bare"})
		git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
		git.ExecGit(repoDir, []string{"push", "origin", "main"})
		// Block INSERT on core_repositories to trigger InsertRepository error logging
		cache.ExecLocked(func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TRIGGER block_repo_insert BEFORE INSERT ON core_repositories BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
			return err
		})
		res := FetchAll(repoDir, t.TempDir(), &Options{}, nil, nil, nil)
		if !res.Success {
			t.Errorf("FetchAll() should succeed: %v", res.Error)
		}
		if res.Data.Repositories != 1 {
			t.Errorf("repositories = %d, want 1 (workspace counted despite cache errors)", res.Data.Repositories)
		}
	})
	t.Run("FetchAll_workspaceUpdateLastFetchError", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping integration test")
		}
		setupTestCache(t)
		repoDir, _ := initTestRepo(t, 2)
		bareDir := t.TempDir()
		git.ExecGit(bareDir, []string{"init", "--bare"})
		git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
		git.ExecGit(repoDir, []string{"push", "origin", "main"})
		// Only block UPDATE: InsertRepository succeeds (row exists), UpdateLastFetch fails
		cache.ExecLocked(func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TRIGGER block_repo_update BEFORE UPDATE ON core_repositories BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
			return err
		})
		res := FetchAll(repoDir, t.TempDir(), &Options{}, nil, nil, nil)
		if !res.Success {
			t.Errorf("FetchAll() should succeed: %v", res.Error)
		}
	})
	t.Run("FetchRepository_updateLastFetchError", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping integration test")
		}
		setupTestCache(t)
		repoDir, _ := initTestRepo(t, 1)
		bareDir := t.TempDir()
		git.ExecGit(bareDir, []string{"init", "--bare"})
		git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
		git.ExecGit(repoDir, []string{"push", "origin", "main"})
		// Only block UPDATE: InsertRepository succeeds, UpdateLastFetch fails
		cache.ExecLocked(func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TRIGGER block_repo_update BEFORE UPDATE ON core_repositories BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
			return err
		})
		cacheDir := t.TempDir()
		res := FetchRepository(cacheDir, bareDir, "main", "", nil, nil)
		if !res.Success {
			t.Errorf("FetchRepository() should succeed: %v", res.Error)
		}
	})
	t.Run("FetchRepository_internal_updateLastFetchError", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping integration test")
		}
		setupTestCache(t)
		repoDir, _ := initTestRepo(t, 1)
		bareDir := t.TempDir()
		git.ExecGit(bareDir, []string{"init", "--bare"})
		git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
		git.ExecGit(repoDir, []string{"push", "origin", "main"})
		// Only block UPDATE
		cache.ExecLocked(func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TRIGGER block_repo_update BEFORE UPDATE ON core_repositories BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
			return err
		})
		cacheDir := t.TempDir()
		// isFollowed=true → full history path (nil fetch options, no --shallow-since issues)
		_, err := fetchRepository(cacheDir, bareDir, "main", true, "", "", "", nil, nil)
		if err != nil {
			t.Errorf("fetchRepository should succeed: %v", err)
		}
	})

	// Restore shared cache for parallel tests
	cache.Reset()
	if err := cache.Open(sharedCacheDir); err != nil {
		t.Fatalf("restore shared cache: %v", err)
	}
}

// --- fetchFullHistory tests (real git repo) ---

func TestFetchFullHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	dir, commits := initTestRepo(t, 3)
	repoURL := "https://example.com/fullhistory"

	count, err := fetchFullHistory(dir, repoURL, "main", nil)
	if err != nil {
		t.Fatalf("fetchFullHistory() error = %v", err)
	}
	if count != len(commits) {
		t.Errorf("count = %d, want %d", count, len(commits))
	}

	// Verify commits in cache
	dbCount, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow("SELECT COUNT(*) FROM core_commits WHERE repo_url = ?", repoURL).Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("QueryLocked() error = %v", err)
	}
	if dbCount != len(commits) {
		t.Errorf("db count = %d, want %d", dbCount, len(commits))
	}

	// Verify fetch range was inserted
	rangeCount, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow("SELECT COUNT(*) FROM core_fetch_ranges WHERE repo_url = ?", repoURL).Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("QueryLocked() error = %v", err)
	}
	if rangeCount == 0 {
		t.Error("no fetch range inserted")
	}
}

func TestFetchFullHistory_withProcessor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	dir, commits := initTestRepo(t, 2)
	repoURL := "https://example.com/fullhistory-proc"

	var processed int
	proc := func(commit git.Commit, msg *protocol.Message, rURL, b string) { processed++ }

	count, err := fetchFullHistory(dir, repoURL, "main", []CommitProcessor{proc})
	if err != nil {
		t.Fatalf("fetchFullHistory() error = %v", err)
	}
	if count != len(commits) {
		t.Errorf("count = %d, want %d", count, len(commits))
	}
	if processed != len(commits) {
		t.Errorf("processed = %d, want %d", processed, len(commits))
	}
}

func TestFetchFullHistory_idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	dir, _ := initTestRepo(t, 3)
	repoURL := "https://example.com/fullhistory-idem"

	count1, err := fetchFullHistory(dir, repoURL, "main", nil)
	if err != nil {
		t.Fatalf("first fetchFullHistory() error = %v", err)
	}

	count2, err := fetchFullHistory(dir, repoURL, "main", nil)
	if err != nil {
		t.Fatalf("second fetchFullHistory() error = %v", err)
	}
	if count2 != 0 {
		t.Errorf("second fetch count = %d, want 0 (all already cached)", count2)
	}
	if count1 == 0 {
		t.Error("first fetch should have returned > 0")
	}
}

// --- fetchIncremental tests ---

func TestFetchIncremental(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	dir, commits := initTestRepo(t, 3)
	repoURL := "https://example.com/incremental"

	// Use yesterday to ensure all today's commits are included (git --since uses date granularity)
	sinceTime := time.Now().AddDate(0, 0, -1)
	count, err := fetchIncremental(dir, repoURL, "main", sinceTime, nil)
	if err != nil {
		t.Fatalf("fetchIncremental() error = %v", err)
	}
	if count != len(commits) {
		t.Errorf("count = %d, want %d", count, len(commits))
	}
}

func TestFetchIncremental_onlyNewCommits(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	dir, _ := initTestRepo(t, 2)
	repoURL := "https://example.com/incr-new"

	// Fetch all first (use yesterday to avoid date boundary issues)
	sinceTime := time.Now().AddDate(0, 0, -1)
	_, err := fetchIncremental(dir, repoURL, "main", sinceTime, nil)
	if err != nil {
		t.Fatalf("first fetchIncremental() error = %v", err)
	}

	// Add a new commit
	if _, err := git.CreateCommit(dir, git.CommitOptions{Message: "new commit", AllowEmpty: true}); err != nil {
		t.Fatalf("CreateCommit() error = %v", err)
	}

	// Fetch again — only the new commit should be counted
	count, err := fetchIncremental(dir, repoURL, "main", sinceTime, nil)
	if err != nil {
		t.Fatalf("second fetchIncremental() error = %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (only the new commit)", count)
	}
}

// --- fetch30DayWindow tests ---

func TestFetch30DayWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	dir, commits := initTestRepo(t, 3)
	repoURL := "https://example.com/window"

	since := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	count, err := fetch30DayWindow(dir, repoURL, "main", since, "", nil)
	if err != nil {
		t.Fatalf("fetch30DayWindow() error = %v", err)
	}
	if count != len(commits) {
		t.Errorf("count = %d, want %d", count, len(commits))
	}
}

func TestFetch30DayWindow_withBefore(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	dir, _ := initTestRepo(t, 3)
	repoURL := "https://example.com/window-before"

	// Use a "before" date in the past so no commits match
	since := "2020-01-01"
	before := "2020-01-02"
	count, err := fetch30DayWindow(dir, repoURL, "main", since, before, nil)
	if err != nil {
		t.Fatalf("fetch30DayWindow() error = %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 (before date is in the past)", count)
	}
}

func TestFetch30DayWindow_futureRange(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	dir, _ := initTestRepo(t, 2)
	repoURL := "https://example.com/window-future"

	since := "2099-01-01"
	count, err := fetch30DayWindow(dir, repoURL, "main", since, "", nil)
	if err != nil {
		t.Fatalf("fetch30DayWindow() error = %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 (since is in the future)", count)
	}
}

// --- FetchAll edge cases ---

func TestFetchAll_nilOpts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	git.Init(dir, "main")

	// Should not panic with nil opts
	res := FetchAll(dir, t.TempDir(), nil, nil, nil, nil)
	if !res.Success {
		t.Errorf("FetchAll() failed: %v", res.Error)
	}
}

func TestFetchAll_emptyRepos(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	git.Init(dir, "main")

	res := FetchAll(dir, t.TempDir(), &Options{}, []RepoInfo{}, nil, nil)
	if !res.Success {
		t.Errorf("FetchAll() failed: %v", res.Error)
	}
	if res.Data.Items != 0 {
		t.Errorf("items = %d, want 0", res.Data.Items)
	}
}

func TestFetchAll_progressCallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	dir := t.TempDir()
	git.Init(dir, "main")

	var mu sync.Mutex
	var progressCalls []int
	opts := &Options{
		OnProgress: func(repoURL string, processed, total int) {
			mu.Lock()
			progressCalls = append(progressCalls, processed)
			mu.Unlock()
		},
	}

	// Create a bare repo to fetch from
	repoDir, _ := initTestRepo(t, 2)
	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})
	git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(repoDir, []string{"push", "origin", "main"})

	repos := []RepoInfo{{URL: bareDir, Branch: "main"}}
	cacheDir := t.TempDir()
	FetchAll(dir, cacheDir, opts, repos, nil, nil)

	mu.Lock()
	defer mu.Unlock()
	if len(progressCalls) == 0 {
		t.Error("OnProgress was never called")
	}
}

func TestFetchAll_reconcileVersions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	git.Init(dir, "main")

	// FetchAll with empty repos should still run ReconcileVersions without error
	res := FetchAll(dir, t.TempDir(), &Options{}, nil, nil, nil)
	if !res.Success {
		t.Errorf("FetchAll() failed: %v", res.Error)
	}
}

// --- FetchRepositoryRange tests ---

func TestFetchRepositoryRange_defaultBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()

	// Create a repo with a gitmsg branch (required by storage.FetchRepository)
	repoDir, _ := initTestRepo(t, 2)
	git.ExecGit(repoDir, []string{"checkout", "-b", "gitmsg/social/posts"})
	git.CreateCommit(repoDir, git.CommitOptions{Message: "gitmsg post", AllowEmpty: true})
	git.ExecGit(repoDir, []string{"checkout", "main"})

	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})
	git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(repoDir, []string{"push", "origin", "--all"})

	cacheDir := t.TempDir()
	since := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	// Empty branch should default to "main"
	res := FetchRepositoryRange(cacheDir, bareDir, "", since, "", "", nil, nil)
	if !res.Success {
		t.Errorf("FetchRepositoryRange() failed: %v", res.Error)
	}
	if res.Data.Repositories != 1 {
		t.Errorf("repositories = %d, want 1", res.Data.Repositories)
	}
}

// --- FetchRepository tests ---

func TestFetchRepository_defaultBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()

	repoDir, _ := initTestRepo(t, 2)
	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})
	git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(repoDir, []string{"push", "origin", "main"})

	cacheDir := t.TempDir()
	res := FetchRepository(cacheDir, bareDir, "", "", nil, nil)
	if !res.Success {
		t.Errorf("FetchRepository() failed: %v", res.Error)
	}
	if res.Data.Repositories != 1 {
		t.Errorf("repositories = %d, want 1", res.Data.Repositories)
	}
	if res.Data.Items < 2 {
		t.Errorf("items = %d, want >= 2", res.Data.Items)
	}
}

func TestFetchRepository_runsHooks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()

	repoDir, _ := initTestRepo(t, 1)
	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})
	git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(repoDir, []string{"push", "origin", "main"})

	hookCalled := false
	hook := func(storageDir, repoURL, branch, workspaceURL string) {
		hookCalled = true
	}

	cacheDir := t.TempDir()
	res := FetchRepository(cacheDir, bareDir, "main", "", nil, []PostFetchHook{hook})
	if !res.Success {
		t.Errorf("FetchRepository() failed: %v", res.Error)
	}
	if !hookCalled {
		t.Error("post-fetch hook was not called")
	}
}

func TestFetchRepository_runsProcessors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()

	repoDir, _ := initTestRepo(t, 2)
	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})
	git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(repoDir, []string{"push", "origin", "main"})

	var processed int
	proc := func(commit git.Commit, msg *protocol.Message, rURL, b string) { processed++ }

	cacheDir := t.TempDir()
	res := FetchRepository(cacheDir, bareDir, "main", "", []CommitProcessor{proc}, nil)
	if !res.Success {
		t.Errorf("FetchRepository() failed: %v", res.Error)
	}
	if processed < 2 {
		t.Errorf("processed = %d, want >= 2", processed)
	}
}

func TestFetchRepository_storageError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	res := FetchRepository("/dev/null/impossible", "https://invalid.example.com/repo", "main", "", nil, nil)
	if res.Success {
		t.Error("expected failure for invalid cache dir")
	}
	if res.Error.Code != "STORAGE_ERROR" {
		t.Errorf("code = %q, want STORAGE_ERROR", res.Error.Code)
	}
}

func TestFetchRepository_fetchError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	// Valid cache dir but unreachable remote: EnsureRepository succeeds (just inits bare + configures upstream),
	// storage.FetchRepository fails (git fetch upstream → remote not found)
	res := FetchRepository(t.TempDir(), "/nonexistent/repo.git", "main", "", nil, nil)
	if res.Success {
		t.Error("expected failure for nonexistent remote")
	}
	if res.Error.Code != "FETCH_ERROR" {
		t.Errorf("code = %q, want FETCH_ERROR", res.Error.Code)
	}
}

// --- fetchRepository internal tests (followed paths) ---

func TestFetchRepository_followedNoCommits(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	repoDir, _ := initTestRepo(t, 3)
	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})
	git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(repoDir, []string{"push", "origin", "main"})

	cacheDir := t.TempDir()
	// isFollowed=true, no commits in cache → takes full history path
	count, err := fetchRepository(cacheDir, bareDir, "main", true, "", "", "", nil, nil)
	if err != nil {
		t.Fatalf("fetchRepository() error = %v", err)
	}
	if count < 3 {
		t.Errorf("count = %d, want >= 3", count)
	}
}

func TestFetchRepository_followedIncremental(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	repoDir, _ := initTestRepo(t, 2)
	// Add gitmsg branch so storage.FetchRepository succeeds with --shallow-since/--depth
	git.ExecGit(repoDir, []string{"checkout", "-b", "gitmsg/ext/data"})
	git.CreateCommit(repoDir, git.CommitOptions{Message: "gitmsg data", AllowEmpty: true})
	git.ExecGit(repoDir, []string{"checkout", "main"})

	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})
	git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(repoDir, []string{"push", "origin", "--all"})

	cacheDir := t.TempDir()
	// First fetch: full history (populates cache, meta.HasCommits becomes true)
	_, err := fetchRepository(cacheDir, bareDir, "main", true, "", "", "", nil, nil)
	if err != nil {
		t.Fatalf("first fetch error = %v", err)
	}

	// Add a new commit and push
	git.CreateCommit(repoDir, git.CommitOptions{Message: "incremental", AllowEmpty: true})
	git.ExecGit(repoDir, []string{"push", "origin", "main"})

	// Second fetch: exercises incremental path (meta.HasCommits is true).
	// Count may be 0 if storage.FetchRepository silently fails and no new commits are seen.
	_, err = fetchRepository(cacheDir, bareDir, "main", true, "", "", "", nil, nil)
	if err != nil {
		t.Fatalf("second fetch error = %v", err)
	}
}

func TestFetchRepository_ensureError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	_, err := fetchRepository("/dev/null/impossible", "https://invalid.example.com/repo", "main", false, "", "", "", nil, nil)
	if err == nil {
		t.Error("expected error for invalid cache dir")
	}
}

func TestFetchRepository_followedFullFetchError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	cacheDir := t.TempDir()
	// EnsureRepository succeeds (creates bare repo), but storage.FetchRepository fails (bad upstream)
	_, err := fetchRepository(cacheDir, "/nonexistent/repo.git", "main", true, "", "", "", nil, nil)
	if err == nil {
		t.Error("expected error for nonexistent remote")
	}
}

func TestFetchRepository_notFollowedFetchError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	cacheDir := t.TempDir()
	since := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	_, err := fetchRepository(cacheDir, "/nonexistent/repo.git", "main", false, since, "", "", nil, nil)
	if err == nil {
		t.Error("expected error for nonexistent remote")
	}
}

func TestFetchRepository_withHooksAndProcessors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	repoDir, _ := initTestRepo(t, 2)
	// Add gitmsg branch so storage.FetchRepository succeeds with --shallow-since/--depth
	git.ExecGit(repoDir, []string{"checkout", "-b", "gitmsg/ext/data"})
	git.CreateCommit(repoDir, git.CommitOptions{Message: "gitmsg data", AllowEmpty: true})
	git.ExecGit(repoDir, []string{"checkout", "main"})

	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})
	git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(repoDir, []string{"push", "origin", "--all"})

	hookCalled := false
	hook := func(storageDir, repoURL, branch, workspaceURL string) { hookCalled = true }
	var processed int
	proc := func(commit git.Commit, msg *protocol.Message, rURL, b string) { processed++ }

	cacheDir := t.TempDir()
	since := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	_, err := fetchRepository(cacheDir, bareDir, "main", false, since, "", "https://workspace.com", []CommitProcessor{proc}, []PostFetchHook{hook})
	if err != nil {
		t.Fatalf("fetchRepository() error = %v", err)
	}
	if !hookCalled {
		t.Error("hook not called")
	}
	if processed < 2 {
		t.Errorf("processed = %d, want >= 2", processed)
	}
}

// --- Additional FetchAll tests ---

func TestFetchAll_workspaceSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	repoDir, _ := initTestRepo(t, 2)
	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})
	git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(repoDir, []string{"push", "origin", "main"})

	// Workspace with valid origin exercises the origin sync block
	res := FetchAll(repoDir, t.TempDir(), &Options{}, nil, nil, nil)
	if !res.Success {
		t.Errorf("FetchAll() failed: %v", res.Error)
	}
	if res.Data.Repositories < 1 {
		t.Errorf("repositories = %d, want >= 1 (workspace sync)", res.Data.Repositories)
	}
}

func TestFetchAll_workspaceSyncCustomBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	repoDir, _ := initTestRepo(t, 1)
	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})
	git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(repoDir, []string{"push", "origin", "main"})

	// Explicit WorkspaceBranch exercises the branch != "" path
	res := FetchAll(repoDir, t.TempDir(), &Options{WorkspaceBranch: "develop"}, nil, nil, nil)
	if !res.Success {
		t.Errorf("FetchAll() failed: %v", res.Error)
	}
	if res.Data.Repositories < 1 {
		t.Errorf("repositories = %d, want >= 1", res.Data.Repositories)
	}
}

func TestFetchAll_repoFetchError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	dir := t.TempDir()
	git.Init(dir, "main")

	// Invalid repo triggers error accumulation in the goroutine
	repos := []RepoInfo{{URL: "/nonexistent/repo.git", Branch: "main"}}
	res := FetchAll(dir, t.TempDir(), &Options{}, repos, nil, nil)
	if !res.Success {
		t.Error("FetchAll should succeed even with individual repo errors")
	}
	if len(res.Data.Errors) == 0 {
		t.Error("expected errors for invalid repo")
	}
}

func TestFetchAll_withRealRepos(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	wsDir, _ := initTestRepo(t, 1)
	repoDir, _ := initTestRepo(t, 3)
	// Add gitmsg branch so storage.FetchRepository succeeds
	git.ExecGit(repoDir, []string{"checkout", "-b", "gitmsg/ext/data"})
	git.CreateCommit(repoDir, git.CommitOptions{Message: "gitmsg data", AllowEmpty: true})
	git.ExecGit(repoDir, []string{"checkout", "main"})

	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})
	git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(repoDir, []string{"push", "origin", "--all"})

	repos := []RepoInfo{{URL: bareDir, Branch: "main"}}
	cacheDir := t.TempDir()
	res := FetchAll(wsDir, cacheDir, &Options{}, repos, nil, nil)
	if !res.Success {
		t.Errorf("FetchAll() failed: %v", res.Error)
	}
	if res.Data.Items < 3 {
		t.Errorf("items = %d, want >= 3", res.Data.Items)
	}
}

func TestFetchAll_followedViaListID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	wsDir, _ := initTestRepo(t, 1)
	repoDir, _ := initTestRepo(t, 2)
	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})
	git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(repoDir, []string{"push", "origin", "main"})

	// ListID makes isFollowed=true, exercising the followed fetch strategy in goroutine
	repos := []RepoInfo{{URL: bareDir, Branch: "main", ListID: "test-list"}}
	cacheDir := t.TempDir()
	res := FetchAll(wsDir, cacheDir, &Options{}, repos, nil, nil)
	if !res.Success {
		t.Errorf("FetchAll() failed: %v", res.Error)
	}
}

// --- Additional FetchRepositoryRange tests ---

func TestFetchRepositoryRange_fetchError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	res := FetchRepositoryRange(t.TempDir(), "/nonexistent/repo.git", "main", "", "", "", nil, nil)
	if res.Success {
		t.Error("expected failure for nonexistent remote")
	}
}

func TestFetchAll_reconcilesVersions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	dir, _ := initTestRepo(t, 1)
	repoURL := "https://example.com/versions"
	now := time.Now()

	// Insert EDIT commit first — canonical doesn't exist yet, so no version record is created
	// Hash in ref must be hex-only (ParseRef regex: [a-f0-9]+), truncated to 12 chars
	editMsg := protocol.FormatMessage("edited content", protocol.Header{
		Ext:    "social",
		V:      "1",
		Fields: map[string]string{"type": "post", "edits": repoURL + "#commit:aaa111bbb222"},
	}, nil)
	cache.InsertCommits([]cache.Commit{
		{Hash: "eee555fff666", RepoURL: repoURL, Branch: "main", AuthorName: "Alice", AuthorEmail: "alice@test.com", Message: editMsg, Timestamp: now},
	})

	// Insert CANONICAL commit — InsertCommits won't retroactively link existing edits
	cache.InsertCommits([]cache.Commit{
		{Hash: "aaa111bbb222", RepoURL: repoURL, Branch: "main", AuthorName: "Alice", AuthorEmail: "alice@test.com", Message: "original", Timestamp: now},
	})

	// FetchAll returns early if repos is empty (before ReconcileVersions).
	// Pass a dummy repo so the goroutine loop runs, then ReconcileVersions executes.
	repos := []RepoInfo{{URL: "/nonexistent/repo.git", Branch: "main"}}
	res := FetchAll(dir, t.TempDir(), &Options{}, repos, nil, nil)
	if !res.Success {
		t.Errorf("FetchAll() failed: %v", res.Error)
	}
}

func TestFetchRepository_followedIncrementalFetchFail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	repoDir, _ := initTestRepo(t, 2)
	git.ExecGit(repoDir, []string{"checkout", "-b", "gitmsg/ext/data"})
	git.CreateCommit(repoDir, git.CommitOptions{Message: "gitmsg data", AllowEmpty: true})
	git.ExecGit(repoDir, []string{"checkout", "main"})
	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})
	git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(repoDir, []string{"push", "origin", "--all"})
	cacheDir := t.TempDir()
	// First fetch: full history
	_, err := fetchRepository(cacheDir, bareDir, "main", true, "", "", "", nil, nil)
	if err != nil {
		t.Fatalf("first fetch error = %v", err)
	}
	// Delete remote to make storage.FetchRepository fail on second call
	os.RemoveAll(bareDir)
	// Second fetch: incremental path, storage.FetchRepository fails → logged, continues with cached data
	_, err = fetchRepository(cacheDir, bareDir, "main", true, "", "", "", nil, nil)
	if err != nil {
		t.Fatalf("second fetch should succeed with cached data: %v", err)
	}
}
