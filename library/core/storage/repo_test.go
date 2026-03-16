// repo_test.go - Tests for bare repository storage management
package storage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/core/git"
)

var baseSourceRepoDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "storage-test-base-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	git.Init(dir, "main")
	git.ExecGit(dir, []string{"config", "user.email", "test@test.com"})
	git.ExecGit(dir, []string{"config", "user.name", "Test User"})
	git.CreateCommit(dir, git.CommitOptions{Message: "initial", AllowEmpty: true})
	git.ExecGit(dir, []string{"checkout", "-b", "gitmsg/social/posts"})
	git.CreateCommit(dir, git.CommitOptions{Message: "post", AllowEmpty: true})
	git.ExecGit(dir, []string{"checkout", "main"})
	baseSourceRepoDir = dir
	os.Exit(m.Run())
}

func cloneFixture(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	cmd := exec.Command("cp", "-a", baseSourceRepoDir+"/.", dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cloneFixture: %v: %s", err, out)
	}
	return dst
}

// failOnArg returns an ExecFunc that fails with exit code 128 when any arg matches failArg.
func failOnArg(failArg string) git.ExecFunc {
	return func(ctx context.Context, workdir string, args []string) (*git.ExecResult, error) {
		for _, arg := range args {
			if arg == failArg {
				return nil, &git.GitError{
					Op:     "git " + strings.Join(args, " "),
					Args:   args,
					Err:    git.ErrGitExec,
					Stderr: "mock failure on " + failArg,
					Code:   128,
				}
			}
		}
		return git.DefaultExec(ctx, workdir, args)
	}
}

// initSourceRepo creates a git repo with commits and a gitmsg branch, returns dir.
func initSourceRepo(t *testing.T) string {
	t.Helper()
	return cloneFixture(t)
}

// pushToBare creates a bare repo and pushes all branches from source, returns bare dir.
func pushToBare(t *testing.T, sourceDir string) string {
	t.Helper()
	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})
	git.ExecGit(sourceDir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(sourceDir, []string{"push", "origin", "--all"})
	return bareDir
}

// --- urlToDirectoryName tests ---

func TestUrlToDirectoryName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"https github", "https://github.com/user/repo", "github.com-user-repo"},
		{"https with .git", "https://github.com/user/repo.git", "github.com-user-repo"},
		{"http prefix", "http://github.com/user/repo", "github.com-user-repo"},
		{"colon replaced", "git:github.com/user/repo", "git-github.com-user-repo"},
		{"slash replaced", "https://gitlab.com/org/sub/repo", "gitlab.com-org-sub-repo"},
		{"no prefix", "github.com/user/repo", "github.com-user-repo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := urlToDirectoryName(tt.url)
			if got != tt.want {
				t.Errorf("urlToDirectoryName(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestUrlToDirectoryName_truncation(t *testing.T) {
	t.Parallel()
	longURL := "https://github.com/organization-with-very-long-name/repository-with-extremely-long-name-that-exceeds-limit"
	got := urlToDirectoryName(longURL)
	if len(got) > 50 {
		t.Errorf("urlToDirectoryName() length = %d, want <= 50", len(got))
	}
}

// --- GetStorageDir tests ---

func TestGetStorageDir(t *testing.T) {
	t.Parallel()
	baseDir := "/tmp/cache"
	repoURL := "https://github.com/user/repo"

	got := GetStorageDir(baseDir, repoURL)

	if !strings.HasPrefix(got, filepath.Join(baseDir, "repositories")) {
		t.Errorf("GetStorageDir() = %q, should start with %q", got, filepath.Join(baseDir, "repositories"))
	}
	if !strings.Contains(got, "github.com-user-repo") {
		t.Errorf("GetStorageDir() = %q, should contain directory name", got)
	}
}

func TestGetStorageDir_deterministic(t *testing.T) {
	t.Parallel()
	baseDir := "/tmp/cache"
	repoURL := "https://github.com/user/repo"

	first := GetStorageDir(baseDir, repoURL)
	second := GetStorageDir(baseDir, repoURL)
	if first != second {
		t.Errorf("GetStorageDir() not deterministic: %q != %q", first, second)
	}
}

func TestGetStorageDir_uniqueForDifferentURLs(t *testing.T) {
	t.Parallel()
	baseDir := "/tmp/cache"
	dir1 := GetStorageDir(baseDir, "https://github.com/user/repo1")
	dir2 := GetStorageDir(baseDir, "https://github.com/user/repo2")
	if dir1 == dir2 {
		t.Errorf("GetStorageDir() should return unique dirs for different URLs: both = %q", dir1)
	}
}

// --- EnsureRepository tests ---

func TestEnsureRepository_createsBareRepo(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()
	repoURL := "https://github.com/test/repo"

	storageDir, err := EnsureRepository(baseDir, repoURL, "main", nil)
	if err != nil {
		t.Fatalf("EnsureRepository() error = %v", err)
	}
	if storageDir == "" {
		t.Fatal("storageDir is empty")
	}

	// Verify bare repo was initialized
	if !git.IsRepository(storageDir) {
		t.Error("storage dir is not a git repository")
	}

	// Verify upstream remote points to repoURL
	result, err := git.ExecGit(storageDir, []string{"remote", "get-url", "upstream"})
	if err != nil {
		t.Fatalf("get remote url error = %v", err)
	}
	if strings.TrimSpace(result.Stdout) != repoURL {
		t.Errorf("upstream url = %q, want %q", strings.TrimSpace(result.Stdout), repoURL)
	}
}

func TestEnsureRepository_setsConfigs(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()
	repoURL := "https://github.com/test/config-repo"

	storageDir, err := EnsureRepository(baseDir, repoURL, "develop", &EnsureOptions{IsPersistent: true})
	if err != nil {
		t.Fatalf("EnsureRepository() error = %v", err)
	}

	// Verify partial clone filter
	result, _ := git.ExecGit(storageDir, []string{"config", "remote.upstream.partialclonefilter"})
	if strings.TrimSpace(result.Stdout) != "blob:none" {
		t.Errorf("partialclonefilter = %q, want blob:none", strings.TrimSpace(result.Stdout))
	}

	// Verify push disabled
	result, _ = git.ExecGit(storageDir, []string{"config", "remote.upstream.pushurl"})
	if strings.TrimSpace(result.Stdout) != "" {
		t.Errorf("pushurl = %q, want empty", strings.TrimSpace(result.Stdout))
	}

	// Verify branch config
	result, _ = git.ExecGit(storageDir, []string{"config", "gitmsg.branch"})
	if strings.TrimSpace(result.Stdout) != "develop" {
		t.Errorf("gitmsg.branch = %q, want develop", strings.TrimSpace(result.Stdout))
	}

	// Verify persistent config
	result, _ = git.ExecGit(storageDir, []string{"config", "gitmsg.persistent"})
	if strings.TrimSpace(result.Stdout) != "1" {
		t.Errorf("gitmsg.persistent = %q, want 1", strings.TrimSpace(result.Stdout))
	}
}

func TestEnsureRepository_notPersistent(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()

	storageDir, err := EnsureRepository(baseDir, "https://github.com/test/np", "main", &EnsureOptions{IsPersistent: false})
	if err != nil {
		t.Fatalf("EnsureRepository() error = %v", err)
	}

	result, _ := git.ExecGit(storageDir, []string{"config", "gitmsg.persistent"})
	if strings.TrimSpace(result.Stdout) != "0" {
		t.Errorf("gitmsg.persistent = %q, want 0", strings.TrimSpace(result.Stdout))
	}
}

func TestEnsureRepository_nilOpts(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()

	storageDir, err := EnsureRepository(baseDir, "https://github.com/test/nilopt", "main", nil)
	if err != nil {
		t.Fatalf("EnsureRepository() error = %v", err)
	}

	// nil opts should default to non-persistent
	result, _ := git.ExecGit(storageDir, []string{"config", "gitmsg.persistent"})
	if strings.TrimSpace(result.Stdout) != "0" {
		t.Errorf("gitmsg.persistent = %q, want 0", strings.TrimSpace(result.Stdout))
	}
}

func TestEnsureRepository_existingDirSkipsInit(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()
	repoURL := "https://github.com/test/existing"

	first, err := EnsureRepository(baseDir, repoURL, "main", nil)
	if err != nil {
		t.Fatalf("first EnsureRepository() error = %v", err)
	}

	// Second call should return the same dir without re-initializing
	second, err := EnsureRepository(baseDir, repoURL, "main", nil)
	if err != nil {
		t.Fatalf("second EnsureRepository() error = %v", err)
	}
	if first != second {
		t.Errorf("second call returned %q, want %q", second, first)
	}
}

func TestEnsureRepository_forceReInitializes(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()
	repoURL := "https://github.com/test/force"

	// Force on a non-existing dir should create it normally
	storageDir, err := EnsureRepository(baseDir, repoURL, "main", &EnsureOptions{Force: true})
	if err != nil {
		t.Fatalf("EnsureRepository(Force) error = %v", err)
	}
	if !git.IsRepository(storageDir) {
		t.Error("forced repo is not a valid git repository")
	}

	// Verify Force=true doesn't short-circuit on existing dir
	// (it attempts re-init, which fails on remote add — this is expected behavior)
	_, err = EnsureRepository(baseDir, repoURL, "main", &EnsureOptions{Force: true})
	if err == nil {
		t.Error("expected error when forcing re-init on existing repo (remote already exists)")
	}
}

func TestEnsureRepository_storageDirMatchesGetStorageDir(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()
	repoURL := "https://github.com/test/path"

	storageDir, err := EnsureRepository(baseDir, repoURL, "main", nil)
	if err != nil {
		t.Fatalf("EnsureRepository() error = %v", err)
	}

	expected := GetStorageDir(baseDir, repoURL)
	if storageDir != expected {
		t.Errorf("storageDir = %q, want %q", storageDir, expected)
	}
}

func TestEnsureRepository_gitInitFails(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()
	repoURL := "https://github.com/test/initfail"
	storagePath := GetStorageDir(baseDir, repoURL)
	// Create dir normally, then make read-only so git init --bare can't write HEAD
	os.MkdirAll(storagePath, 0755)
	os.Chmod(storagePath, 0444)
	t.Cleanup(func() { os.Chmod(storagePath, 0755) })

	_, err := EnsureRepository(baseDir, repoURL, "main", &EnsureOptions{Force: true})
	if err == nil {
		t.Error("expected error when git init fails on read-only dir")
	}
	if !strings.Contains(err.Error(), "failed to init bare repo") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnsureRepository_mkdirFails(t *testing.T) {
	t.Parallel()
	// Place a file where the storage dir would be created, so MkdirAll fails
	baseDir := t.TempDir()
	repoURL := "https://github.com/test/mkdirfail"
	storagePath := GetStorageDir(baseDir, repoURL)
	os.MkdirAll(filepath.Dir(storagePath), 0755)
	os.WriteFile(storagePath, []byte("blocker"), 0644)

	_, err := EnsureRepository(baseDir, repoURL, "main", &EnsureOptions{Force: true})
	if err == nil {
		t.Error("expected error when MkdirAll fails")
	}
	if !strings.Contains(err.Error(), "failed to create storage dir") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- FetchRepository tests ---

func TestFetchRepository_fetchesGitmsgBranches(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	source := initSourceRepo(t)
	bare := pushToBare(t, source)
	baseDir := t.TempDir()

	storageDir, err := EnsureRepository(baseDir, bare, "main", nil)
	if err != nil {
		t.Fatalf("EnsureRepository() error = %v", err)
	}

	err = FetchRepository(storageDir, "main", nil)
	if err != nil {
		t.Fatalf("FetchRepository() error = %v", err)
	}

	// Verify gitmsg branch was fetched
	result, err := git.ExecGit(storageDir, []string{"branch", "-a"})
	if err != nil {
		t.Fatalf("git branch error = %v", err)
	}
	if !strings.Contains(result.Stdout, "gitmsg/social/posts") {
		t.Errorf("gitmsg/social/posts branch not found in: %s", result.Stdout)
	}
}

func TestFetchRepository_fetchesDefaultBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	source := initSourceRepo(t)
	bare := pushToBare(t, source)
	baseDir := t.TempDir()

	storageDir, err := EnsureRepository(baseDir, bare, "main", nil)
	if err != nil {
		t.Fatalf("EnsureRepository() error = %v", err)
	}

	err = FetchRepository(storageDir, "main", nil)
	if err != nil {
		t.Fatalf("FetchRepository() error = %v", err)
	}

	// Verify main branch was fetched
	result, err := git.ExecGit(storageDir, []string{"branch", "-a"})
	if err != nil {
		t.Fatalf("git branch error = %v", err)
	}
	if !strings.Contains(result.Stdout, "main") {
		t.Errorf("main branch not found in: %s", result.Stdout)
	}
}

func TestFetchRepository_skipsDefaultBranchFetchForGitmsgPrefix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	source := initSourceRepo(t)
	bare := pushToBare(t, source)
	baseDir := t.TempDir()

	storageDir, err := EnsureRepository(baseDir, bare, "gitmsg/social/posts", nil)
	if err != nil {
		t.Fatalf("EnsureRepository() error = %v", err)
	}

	// Should not error when branch starts with gitmsg/
	err = FetchRepository(storageDir, "gitmsg/social/posts", nil)
	if err != nil {
		t.Fatalf("FetchRepository() error = %v", err)
	}
}

func TestFetchRepository_nilOpts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	source := initSourceRepo(t)
	bare := pushToBare(t, source)
	baseDir := t.TempDir()

	storageDir, err := EnsureRepository(baseDir, bare, "main", nil)
	if err != nil {
		t.Fatalf("EnsureRepository() error = %v", err)
	}

	// nil opts should work without panic
	err = FetchRepository(storageDir, "main", nil)
	if err != nil {
		t.Fatalf("FetchRepository(nil opts) error = %v", err)
	}
}

func TestFetchRepository_withDepth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	source := initSourceRepo(t)
	bare := pushToBare(t, source)
	baseDir := t.TempDir()

	storageDir, err := EnsureRepository(baseDir, bare, "main", nil)
	if err != nil {
		t.Fatalf("EnsureRepository() error = %v", err)
	}

	err = FetchRepository(storageDir, "main", &FetchOptions{Depth: 1})
	if err != nil {
		t.Fatalf("FetchRepository(Depth=1) error = %v", err)
	}
}

func TestFetchRepository_withSince(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	source := initSourceRepo(t)
	bare := pushToBare(t, source)
	baseDir := t.TempDir()

	storageDir, err := EnsureRepository(baseDir, bare, "main", nil)
	if err != nil {
		t.Fatalf("EnsureRepository() error = %v", err)
	}

	// Fetch with since option — may fall back to depth=100 if shallow-since fails
	err = FetchRepository(storageDir, "main", &FetchOptions{Since: "2020-01-01"})
	if err != nil {
		t.Fatalf("FetchRepository(Since) error = %v", err)
	}
}

func TestFetchRepository_emptyBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	source := initSourceRepo(t)
	bare := pushToBare(t, source)
	baseDir := t.TempDir()

	storageDir, err := EnsureRepository(baseDir, bare, "main", nil)
	if err != nil {
		t.Fatalf("EnsureRepository() error = %v", err)
	}

	// Empty branch should skip the default branch fetch (branch != "")
	err = FetchRepository(storageDir, "", nil)
	if err != nil {
		t.Fatalf("FetchRepository(empty branch) error = %v", err)
	}
}

func TestFetchRepository_withSinceAndDefaultBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	source := initSourceRepo(t)
	bare := pushToBare(t, source)
	baseDir := t.TempDir()

	storageDir, err := EnsureRepository(baseDir, bare, "main", nil)
	if err != nil {
		t.Fatalf("EnsureRepository() error = %v", err)
	}

	// Since + non-gitmsg branch covers the --shallow-since append for default branch fetch
	err = FetchRepository(storageDir, "main", &FetchOptions{Since: "2020-01-01"})
	if err != nil {
		t.Fatalf("FetchRepository(Since + main branch) error = %v", err)
	}
}

func TestFetchRepository_withSinceAndCustomBranches(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	// Create repo with a gitmsg config ref pointing to a custom branch
	source := t.TempDir()
	git.Init(source, "main")
	git.ExecGit(source, []string{"config", "user.email", "test@test.com"})
	git.ExecGit(source, []string{"config", "user.name", "Test User"})
	git.CreateCommit(source, git.CommitOptions{Message: "init", AllowEmpty: true})

	// Create custom branch
	git.ExecGit(source, []string{"checkout", "-b", "releases"})
	git.CreateCommit(source, git.CommitOptions{Message: "release", AllowEmpty: true})
	git.ExecGit(source, []string{"checkout", "main"})

	// Create gitmsg config ref pointing to custom branch
	hash, _ := git.CreateCommit(source, git.CommitOptions{
		Message:    `{"branch": "releases", "ext": "release"}`,
		AllowEmpty: true,
	})
	result, _ := git.ExecGit(source, []string{"rev-parse", hash})
	git.ExecGit(source, []string{"update-ref", "refs/gitmsg/release/config", strings.TrimSpace(result.Stdout)})

	// Create gitmsg posts branch
	git.ExecGit(source, []string{"checkout", "-b", "gitmsg/social/posts"})
	git.CreateCommit(source, git.CommitOptions{Message: "post", AllowEmpty: true})
	git.ExecGit(source, []string{"checkout", "main"})

	bare := pushToBare(t, source)
	git.ExecGit(source, []string{"push", "origin", "refs/gitmsg/release/config:refs/gitmsg/release/config"})

	baseDir := t.TempDir()
	storageDir, err := EnsureRepository(baseDir, bare, "main", nil)
	if err != nil {
		t.Fatalf("EnsureRepository() error = %v", err)
	}

	// First fetch without Since to populate refs
	FetchRepository(storageDir, "main", nil)

	// Fetch with Since to cover custom branch + --shallow-since path
	err = FetchRepository(storageDir, "main", &FetchOptions{Since: "2020-01-01"})
	if err != nil {
		t.Fatalf("FetchRepository(Since + custom branches) error = %v", err)
	}
}

// --- discoverCustomBranches tests ---

func TestDiscoverCustomBranches_noRefs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	git.ExecGit(dir, []string{"init", "--bare"})

	branches := discoverCustomBranches(dir)
	if len(branches) != 0 {
		t.Errorf("discoverCustomBranches() = %v, want empty", branches)
	}
}

func TestDiscoverCustomBranches_withCustomBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	// Create a source repo with a gitmsg config ref under refs/gitmsg/
	source := t.TempDir()
	git.Init(source, "main")
	git.ExecGit(source, []string{"config", "user.email", "test@test.com"})
	git.ExecGit(source, []string{"config", "user.name", "Test User"})
	git.CreateCommit(source, git.CommitOptions{Message: "init", AllowEmpty: true})

	// Create a config commit and store it under refs/gitmsg/release/config
	hash, err := git.CreateCommit(source, git.CommitOptions{
		Message:    `{"branch": "releases", "ext": "release"}`,
		AllowEmpty: true,
	})
	if err != nil {
		t.Fatalf("CreateCommit() error = %v", err)
	}
	// Resolve short hash to full hash
	result, _ := git.ExecGit(source, []string{"rev-parse", hash})
	fullHash := strings.TrimSpace(result.Stdout)
	git.ExecGit(source, []string{"update-ref", "refs/gitmsg/release/config", fullHash})

	// Push to bare repo including gitmsg refs
	bare := pushToBare(t, source)
	git.ExecGit(source, []string{"push", "origin", "refs/gitmsg/release/config:refs/gitmsg/release/config"})

	baseDir := t.TempDir()
	storageDir, err := EnsureRepository(baseDir, bare, "main", nil)
	if err != nil {
		t.Fatalf("EnsureRepository() error = %v", err)
	}
	FetchRepository(storageDir, "main", nil)

	branches := discoverCustomBranches(storageDir)

	found := false
	for _, b := range branches {
		if b == "releases" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("discoverCustomBranches() = %v, want to contain 'releases'", branches)
	}
}

func TestDiscoverCustomBranches_ignoresGitmsgPrefixBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	source := t.TempDir()
	git.Init(source, "main")
	git.ExecGit(source, []string{"config", "user.email", "test@test.com"})
	git.ExecGit(source, []string{"config", "user.name", "Test User"})
	git.CreateCommit(source, git.CommitOptions{Message: "init", AllowEmpty: true})

	hash, _ := git.CreateCommit(source, git.CommitOptions{
		Message:    `{"branch": "gitmsg/social/posts", "ext": "social"}`,
		AllowEmpty: true,
	})
	result, _ := git.ExecGit(source, []string{"rev-parse", hash})
	git.ExecGit(source, []string{"update-ref", "refs/gitmsg/social/config", strings.TrimSpace(result.Stdout)})

	bare := pushToBare(t, source)
	git.ExecGit(source, []string{"push", "origin", "refs/gitmsg/social/config:refs/gitmsg/social/config"})

	baseDir := t.TempDir()
	storageDir, _ := EnsureRepository(baseDir, bare, "main", nil)
	FetchRepository(storageDir, "main", nil)

	branches := discoverCustomBranches(storageDir)
	for _, b := range branches {
		if strings.HasPrefix(b, "gitmsg/") {
			t.Errorf("discoverCustomBranches() should not return gitmsg-prefixed branch: %q", b)
		}
	}
}

func TestDiscoverCustomBranches_invalidJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	source := t.TempDir()
	git.Init(source, "main")
	git.ExecGit(source, []string{"config", "user.email", "test@test.com"})
	git.ExecGit(source, []string{"config", "user.name", "Test User"})
	git.CreateCommit(source, git.CommitOptions{Message: "init", AllowEmpty: true})

	hash, _ := git.CreateCommit(source, git.CommitOptions{Message: "not valid json", AllowEmpty: true})
	result, _ := git.ExecGit(source, []string{"rev-parse", hash})
	git.ExecGit(source, []string{"update-ref", "refs/gitmsg/test/config", strings.TrimSpace(result.Stdout)})

	bare := pushToBare(t, source)
	git.ExecGit(source, []string{"push", "origin", "refs/gitmsg/test/config:refs/gitmsg/test/config"})

	baseDir := t.TempDir()
	storageDir, _ := EnsureRepository(baseDir, bare, "main", nil)
	FetchRepository(storageDir, "main", nil)

	branches := discoverCustomBranches(storageDir)
	if len(branches) != 0 {
		t.Errorf("discoverCustomBranches() = %v, want empty for invalid JSON", branches)
	}
}

func TestDiscoverCustomBranches_noBranchField(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	source := t.TempDir()
	git.Init(source, "main")
	git.ExecGit(source, []string{"config", "user.email", "test@test.com"})
	git.ExecGit(source, []string{"config", "user.name", "Test User"})
	git.CreateCommit(source, git.CommitOptions{Message: "init", AllowEmpty: true})

	hash, _ := git.CreateCommit(source, git.CommitOptions{Message: `{"ext": "social"}`, AllowEmpty: true})
	result, _ := git.ExecGit(source, []string{"rev-parse", hash})
	git.ExecGit(source, []string{"update-ref", "refs/gitmsg/test/config", strings.TrimSpace(result.Stdout)})

	bare := pushToBare(t, source)
	git.ExecGit(source, []string{"push", "origin", "refs/gitmsg/test/config:refs/gitmsg/test/config"})

	baseDir := t.TempDir()
	storageDir, _ := EnsureRepository(baseDir, bare, "main", nil)
	FetchRepository(storageDir, "main", nil)

	branches := discoverCustomBranches(storageDir)
	if len(branches) != 0 {
		t.Errorf("discoverCustomBranches() = %v, want empty when no branch field", branches)
	}
}

func TestDiscoverCustomBranches_emptyBranchField(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	source := t.TempDir()
	git.Init(source, "main")
	git.ExecGit(source, []string{"config", "user.email", "test@test.com"})
	git.ExecGit(source, []string{"config", "user.name", "Test User"})
	git.CreateCommit(source, git.CommitOptions{Message: "init", AllowEmpty: true})

	hash, _ := git.CreateCommit(source, git.CommitOptions{Message: `{"branch": "", "ext": "social"}`, AllowEmpty: true})
	result, _ := git.ExecGit(source, []string{"rev-parse", hash})
	git.ExecGit(source, []string{"update-ref", "refs/gitmsg/test/config", strings.TrimSpace(result.Stdout)})

	bare := pushToBare(t, source)
	git.ExecGit(source, []string{"push", "origin", "refs/gitmsg/test/config:refs/gitmsg/test/config"})

	baseDir := t.TempDir()
	storageDir, _ := EnsureRepository(baseDir, bare, "main", nil)
	FetchRepository(storageDir, "main", nil)

	branches := discoverCustomBranches(storageDir)
	if len(branches) != 0 {
		t.Errorf("discoverCustomBranches() = %v, want empty for empty branch field", branches)
	}
}

// --- EnsureForkRepository tests ---

func TestEnsureForkRepository_createsRepo(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	repoURL := "https://github.com/test/fork-repo"

	dir, err := EnsureForkRepository(cacheDir, repoURL)
	if err != nil {
		t.Fatalf("EnsureForkRepository() error = %v", err)
	}
	if dir == "" {
		t.Fatal("dir is empty")
	}

	// Verify it's under forks/
	if !strings.Contains(dir, "forks") {
		t.Errorf("dir = %q, should contain 'forks'", dir)
	}

	// Verify bare repo was initialized
	if !git.IsRepository(dir) {
		t.Error("fork dir is not a git repository")
	}

	// No upstream remote — remotes are added on-demand by fetchFromUpstream
	_, err = git.ExecGit(dir, []string{"remote"})
	if err != nil {
		t.Fatalf("git remote error = %v", err)
	}
}

func TestEnsureForkRepository_existingDirSkipsInit(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	repoURL := "https://github.com/test/fork-existing"

	first, err := EnsureForkRepository(cacheDir, repoURL)
	if err != nil {
		t.Fatalf("first EnsureForkRepository() error = %v", err)
	}

	// Write marker to detect re-init
	marker := filepath.Join(first, "marker")
	os.WriteFile(marker, []byte("x"), 0644)

	second, err := EnsureForkRepository(cacheDir, repoURL)
	if err != nil {
		t.Fatalf("second EnsureForkRepository() error = %v", err)
	}
	if first != second {
		t.Errorf("second call returned different dir: %q vs %q", first, second)
	}

	// Marker should still exist (dir was not re-created)
	if _, err := os.Stat(marker); err != nil {
		t.Error("marker file was removed — dir was re-initialized unexpectedly")
	}
}

func TestEnsureForkRepository_usesUrlName(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	dir, err := EnsureForkRepository(cacheDir, "https://github.com/org/myrepo")
	if err != nil {
		t.Fatalf("EnsureForkRepository() error = %v", err)
	}

	expected := filepath.Join(cacheDir, "forks", "github.com-org-myrepo")
	if dir != expected {
		t.Errorf("dir = %q, want %q", dir, expected)
	}
}

func TestEnsureForkRepository_mkdirFails(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	// Create forks/ dir as read-only so MkdirAll fails for subdirectory
	forksDir := filepath.Join(cacheDir, "forks")
	os.MkdirAll(forksDir, 0755)
	os.Chmod(forksDir, 0444)
	t.Cleanup(func() { os.Chmod(forksDir, 0755) })

	_, err := EnsureForkRepository(cacheDir, "https://github.com/test/fork-mkdir-fail")
	if err == nil {
		t.Error("expected error when MkdirAll fails")
	}
	if !strings.Contains(err.Error(), "create fork dir") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- discoverCustomBranches with non-config refs ---

func TestDiscoverCustomBranches_skipsNonConfigRefs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	source := t.TempDir()
	git.Init(source, "main")
	git.ExecGit(source, []string{"config", "user.email", "test@test.com"})
	git.ExecGit(source, []string{"config", "user.name", "Test User"})
	git.CreateCommit(source, git.CommitOptions{Message: "init", AllowEmpty: true})

	// Create a non-config ref under refs/gitmsg/ (e.g., a list ref)
	hash, _ := git.CreateCommit(source, git.CommitOptions{Message: "list data", AllowEmpty: true})
	result, _ := git.ExecGit(source, []string{"rev-parse", hash})
	git.ExecGit(source, []string{"update-ref", "refs/gitmsg/social/lists", strings.TrimSpace(result.Stdout)})

	// Also create a valid config ref to ensure iteration hits both
	hash2, _ := git.CreateCommit(source, git.CommitOptions{
		Message:    `{"branch": "releases", "ext": "release"}`,
		AllowEmpty: true,
	})
	result2, _ := git.ExecGit(source, []string{"rev-parse", hash2})
	git.ExecGit(source, []string{"update-ref", "refs/gitmsg/release/config", strings.TrimSpace(result2.Stdout)})

	bare := pushToBare(t, source)
	git.ExecGit(source, []string{"push", "origin", "refs/gitmsg/social/lists:refs/gitmsg/social/lists"})
	git.ExecGit(source, []string{"push", "origin", "refs/gitmsg/release/config:refs/gitmsg/release/config"})

	baseDir := t.TempDir()
	storageDir, _ := EnsureRepository(baseDir, bare, "main", nil)
	FetchRepository(storageDir, "main", nil)

	branches := discoverCustomBranches(storageDir)
	for _, b := range branches {
		if b == "list data" {
			t.Error("discoverCustomBranches() should not read non-config refs")
		}
	}
}

func TestDiscoverCustomBranches_gitShowFails(t *testing.T) {
	t.Parallel()
	// Create a bare repo with a config ref pointing to a non-existent object
	dir := t.TempDir()
	git.ExecGit(dir, []string{"init", "--bare"})

	// Manually write a ref file pointing to a bogus hash
	refDir := filepath.Join(dir, "refs", "gitmsg", "broken")
	os.MkdirAll(refDir, 0755)
	os.WriteFile(filepath.Join(refDir, "config"), []byte("0000000000000000000000000000000001234567\n"), 0644)

	branches := discoverCustomBranches(dir)
	if len(branches) != 0 {
		t.Errorf("discoverCustomBranches() = %v, want empty when git show fails", branches)
	}
}

// --- FetchRepository error path tests ---

func TestFetchRepository_sinceFallbackOnFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	git.ExecGit(dir, []string{"init", "--bare"})
	git.ExecGit(dir, []string{"remote", "add", "upstream", "file:///nonexistent-repo"})

	err := FetchRepository(dir, "main", &FetchOptions{Since: "2020-01-01"})
	if err == nil {
		t.Fatal("expected error with non-existent upstream")
	}
}

// --- EnsureRepository config error path tests ---

func TestEnsureRepository_partialCloneFilterFails(t *testing.T) {
	restore := git.SetExecutor(failOnArg("remote.upstream.partialclonefilter"))
	defer restore()

	baseDir := t.TempDir()
	_, err := EnsureRepository(baseDir, "https://github.com/test/pcf-fail", "main", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to set partial clone filter") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnsureRepository_pushUrlFails(t *testing.T) {
	restore := git.SetExecutor(failOnArg("remote.upstream.pushurl"))
	defer restore()

	baseDir := t.TempDir()
	_, err := EnsureRepository(baseDir, "https://github.com/test/push-fail", "main", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to disable push") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnsureRepository_branchConfigFails(t *testing.T) {
	restore := git.SetExecutor(failOnArg("gitmsg.branch"))
	defer restore()

	baseDir := t.TempDir()
	_, err := EnsureRepository(baseDir, "https://github.com/test/branch-fail", "main", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to set branch config") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnsureRepository_persistentConfigFails(t *testing.T) {
	restore := git.SetExecutor(failOnArg("gitmsg.persistent"))
	defer restore()

	baseDir := t.TempDir()
	_, err := EnsureRepository(baseDir, "https://github.com/test/persist-fail", "main", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to set persistent config") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- EnsureForkRepository error path tests ---

func TestEnsureForkRepository_gitInitFails(t *testing.T) {
	restore := git.SetExecutor(failOnArg("--bare"))
	defer restore()

	cacheDir := t.TempDir()
	_, err := EnsureForkRepository(cacheDir, "https://github.com/test/fork-init-fail")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "init fork repo") {
		t.Errorf("unexpected error: %v", err)
	}
}
