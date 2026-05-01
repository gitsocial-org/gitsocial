// coverage_test.go - Tests for error paths and edge cases to improve coverage
package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mockExecutor returns an ExecFunc that delegates to real git except for
// commands matching the interceptor. The interceptor receives the args and
// returns (result, error, handled). When handled is false the real executor runs.
func mockExecutor(intercept func(args []string) (*ExecResult, error, bool)) ExecFunc {
	return func(ctx context.Context, workdir string, args []string) (*ExecResult, error) {
		if res, err, ok := intercept(args); ok {
			return res, err
		}
		return DefaultExec(ctx, workdir, args)
	}
}

var errMock = &GitError{Op: "mock", Err: ErrGitExec, Code: 1}

// --- exec.go ---

func TestSetExecutor(t *testing.T) {
	dir := initTestRepo(t)

	called := false
	restore := SetExecutor(func(_ context.Context, _ string, _ []string) (*ExecResult, error) {
		called = true
		return &ExecResult{Stdout: "mocked"}, nil
	})

	result, err := ExecGit(dir, []string{"status"})
	if err != nil {
		t.Fatalf("ExecGit() error = %v", err)
	}
	if !called {
		t.Error("mock executor should have been called")
	}
	if result.Stdout != "mocked" {
		t.Errorf("Stdout = %q, want mocked", result.Stdout)
	}

	restore()

	result, err = ExecGit(dir, []string{"status"})
	if err != nil {
		t.Fatalf("ExecGit() after restore error = %v", err)
	}
	if result.Stdout == "mocked" {
		t.Error("should use real executor after restore")
	}
}

// --- operations.go ---

func TestGetUserEmail_error(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) >= 2 && args[0] == "config" && args[1] == "user.email" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	email := GetUserEmail(dir)
	if email != "" {
		t.Errorf("GetUserEmail() = %q, want empty on error", email)
	}
}

func TestInit_execError(t *testing.T) {
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "init" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	err := Init(t.TempDir(), "main")
	if err == nil {
		t.Fatal("Init() should error")
	}
	if !errors.Is(err, ErrGitInit) {
		t.Errorf("error = %v, want ErrGitInit", err)
	}
}

func TestCreateCommit_statusError(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "status" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := CreateCommit(dir, CommitOptions{Message: "test"})
	if err == nil {
		t.Fatal("CreateCommit() should error when status fails")
	}
}

func TestCreateCommit_addError(t *testing.T) {
	dir := initTestRepo(t)
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content"), 0644)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "add" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := CreateCommit(dir, CommitOptions{Message: "test"})
	if err == nil {
		t.Fatal("CreateCommit() should error when add fails")
	}
}

func TestCreateCommit_commitError(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "commit" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := CreateCommit(dir, CommitOptions{Message: "test", AllowEmpty: true})
	if err == nil {
		t.Fatal("CreateCommit() should error when commit fails")
	}
}

func TestCreateCommit_revParseError(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--short=12" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := CreateCommit(dir, CommitOptions{Message: "test", AllowEmpty: true})
	if err == nil {
		t.Fatal("CreateCommit() should error when rev-parse fails")
	}
}

func TestCreateCommitOnBranch_commitTreeError(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "commit-tree" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := CreateCommitOnBranch(dir, "newbranch", "test")
	if err == nil {
		t.Fatal("CreateCommitOnBranch() should error when commit-tree fails")
	}
	if !errors.Is(err, ErrGitCommit) {
		t.Errorf("error = %v, want ErrGitCommit", err)
	}
}

func TestCreateCommitOnBranch_updateRefError(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "update-ref" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := CreateCommitOnBranch(dir, "newbranch", "test")
	if err == nil {
		t.Fatal("CreateCommitOnBranch() should error when update-ref fails")
	}
	if !errors.Is(err, ErrGitCommit) {
		t.Errorf("error = %v, want ErrGitCommit", err)
	}
}

func TestCreateCommitOnBranch_existingBranch_commitTreeError(t *testing.T) {
	dir := initTestRepo(t)
	CreateCommitOnBranch(dir, "feat", "first")
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "commit-tree" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := CreateCommitOnBranch(dir, "feat", "second")
	if err == nil {
		t.Fatal("should error when commit-tree fails on existing branch")
	}
	if !errors.Is(err, ErrGitCommit) {
		t.Errorf("error = %v, want ErrGitCommit", err)
	}
}

func TestCreateCommitOnBranch_existingBranch_updateRefError(t *testing.T) {
	dir := initTestRepo(t)
	CreateCommitOnBranch(dir, "feat", "first")
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "update-ref" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := CreateCommitOnBranch(dir, "feat", "second")
	if err == nil {
		t.Fatal("should error when update-ref fails on existing branch")
	}
	if !errors.Is(err, ErrGitCommit) {
		t.Errorf("error = %v, want ErrGitCommit", err)
	}
}

func TestCreateCommitOnBranch_shortHashFallback(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--short=12" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	hash, err := CreateCommitOnBranch(dir, "newbranch", "test")
	if err != nil {
		t.Fatalf("should fallback, got error = %v", err)
	}
	if len(hash) != 12 {
		t.Errorf("hash = %q (len %d), want 12-char fallback", hash, len(hash))
	}
}

func TestCreateCommitOnBranch_existingBranch_shortHashFallback(t *testing.T) {
	dir := initTestRepo(t)
	CreateCommitOnBranch(dir, "feat", "first")
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--short=12" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	hash, err := CreateCommitOnBranch(dir, "feat", "second")
	if err != nil {
		t.Fatalf("should fallback, got error = %v", err)
	}
	if len(hash) != 12 {
		t.Errorf("hash = %q (len %d), want 12-char fallback", hash, len(hash))
	}
}

func TestGetMergeBase_error(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	_, err := GetMergeBase(dir, "nonexistent1", "nonexistent2")
	if err == nil {
		t.Fatal("GetMergeBase() should error for invalid refs")
	}
}

func TestMergeBranches_fastForward_resolveHeadError(t *testing.T) {
	dir := initTestRepo(t)
	ExecGit(dir, []string{"checkout", "-b", "feature"})
	CreateCommit(dir, CommitOptions{Message: "feat", AllowEmpty: true})
	ExecGit(dir, []string{"checkout", "main"})
	ExecGit(dir, []string{"checkout", "-b", "other"})

	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "refs/heads/feature" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := MergeBranches(dir, "main", "feature")
	if err == nil {
		t.Fatal("should error when rev-parse fails")
	}
	if !strings.Contains(err.Error(), "resolve head branch") {
		t.Errorf("error = %v, want 'resolve head branch'", err)
	}
}

func TestMergeBranches_fastForward_updateRefError(t *testing.T) {
	dir := initTestRepo(t)
	ExecGit(dir, []string{"checkout", "-b", "feature"})
	CreateCommit(dir, CommitOptions{Message: "feat", AllowEmpty: true})
	ExecGit(dir, []string{"checkout", "main"})
	ExecGit(dir, []string{"checkout", "-b", "other"})

	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "update-ref" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := MergeBranches(dir, "main", "feature")
	if err == nil {
		t.Fatal("should error when update-ref fails")
	}
	if !strings.Contains(err.Error(), "fast-forward") {
		t.Errorf("error = %v, want 'fast-forward'", err)
	}
}

func TestMergeBranches_conflict_plumbing(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	os.WriteFile(filepath.Join(dir, "conflict.txt"), []byte("base"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "base"})

	ExecGit(dir, []string{"checkout", "-b", "feature"})
	os.WriteFile(filepath.Join(dir, "conflict.txt"), []byte("feature version"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "feature change"})

	ExecGit(dir, []string{"checkout", "main"})
	os.WriteFile(filepath.Join(dir, "conflict.txt"), []byte("main version"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "main change"})

	ExecGit(dir, []string{"checkout", "-b", "other"})

	_, err := MergeBranches(dir, "main", "feature")
	if err == nil {
		t.Error("should error on conflict in plumbing merge path")
	}
	if !strings.Contains(err.Error(), "merge conflicts") {
		t.Errorf("error = %v, want 'merge conflicts'", err)
	}
}

func TestMergeBranches_nonFF_resolveBaseError(t *testing.T) {
	dir := initTestRepo(t)

	ExecGit(dir, []string{"checkout", "-b", "feature"})
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("feat"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "feat"})

	ExecGit(dir, []string{"checkout", "main"})
	os.WriteFile(filepath.Join(dir, "main.txt"), []byte("main"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "main"})

	ExecGit(dir, []string{"checkout", "-b", "other"})

	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "refs/heads/main" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := MergeBranches(dir, "main", "feature")
	if err == nil {
		t.Fatal("should error when resolving base branch fails")
	}
	if !strings.Contains(err.Error(), "resolve base branch") {
		t.Errorf("error = %v, want 'resolve base branch'", err)
	}
}

func TestMergeBranches_nonFF_resolveHeadError(t *testing.T) {
	dir := initTestRepo(t)

	ExecGit(dir, []string{"checkout", "-b", "feature"})
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("feat"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "feat"})

	ExecGit(dir, []string{"checkout", "main"})
	os.WriteFile(filepath.Join(dir, "main.txt"), []byte("main"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "main"})

	ExecGit(dir, []string{"checkout", "-b", "other"})

	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "refs/heads/feature" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := MergeBranches(dir, "main", "feature")
	if err == nil {
		t.Fatal("should error when resolving head branch fails")
	}
	if !strings.Contains(err.Error(), "resolve head branch") {
		t.Errorf("error = %v, want 'resolve head branch'", err)
	}
}

func TestMergeBranches_nonFF_commitTreeError(t *testing.T) {
	dir := initTestRepo(t)

	ExecGit(dir, []string{"checkout", "-b", "feature"})
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("feat"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "feat"})

	ExecGit(dir, []string{"checkout", "main"})
	os.WriteFile(filepath.Join(dir, "main.txt"), []byte("main"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "main"})

	ExecGit(dir, []string{"checkout", "-b", "other"})

	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "commit-tree" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := MergeBranches(dir, "main", "feature")
	if err == nil {
		t.Fatal("should error when commit-tree fails")
	}
	if !strings.Contains(err.Error(), "create merge commit") {
		t.Errorf("error = %v, want 'create merge commit'", err)
	}
}

func TestMergeBranches_nonFF_updateRefError(t *testing.T) {
	dir := initTestRepo(t)

	ExecGit(dir, []string{"checkout", "-b", "feature"})
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("feat"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "feat"})

	ExecGit(dir, []string{"checkout", "main"})
	os.WriteFile(filepath.Join(dir, "main.txt"), []byte("main"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "main"})

	ExecGit(dir, []string{"checkout", "-b", "other"})

	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "update-ref" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := MergeBranches(dir, "main", "feature")
	if err == nil {
		t.Fatal("should error when update-ref fails")
	}
	if !strings.Contains(err.Error(), "update main ref") {
		t.Errorf("error = %v, want 'update main ref'", err)
	}
}

func TestListLocalBranches_error(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "for-each-ref" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := ListLocalBranches(dir)
	if err == nil {
		t.Fatal("ListLocalBranches() should error when for-each-ref fails")
	}
}

func TestValidatePushPreconditions_remoteListError(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) == 1 && args[0] == "remote" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	err := ValidatePushPreconditions(dir, "origin", "main")
	if !errors.Is(err, ErrGitRemote) {
		t.Errorf("error = %v, want ErrGitRemote", err)
	}
}

func TestCreateCommitTree_error(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "commit-tree" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := CreateCommitTree(dir, "msg", "")
	if err == nil {
		t.Fatal("CreateCommitTree() should error")
	}
}

func TestCreateOrphanBranch_createTreeError(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "commit-tree" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	err := CreateOrphanBranch(dir, "orphan")
	if err == nil {
		t.Fatal("CreateOrphanBranch() should error when commit-tree fails")
	}
}

func TestCreateOrphanBranch_writeRefError(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "update-ref" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	err := CreateOrphanBranch(dir, "orphan")
	if err == nil {
		t.Fatal("CreateOrphanBranch() should error when write-ref fails")
	}
}

func TestCreateCommit_parentCommitTreeError(t *testing.T) {
	dir := initTestRepo(t)
	head, _ := ReadRef(dir, "HEAD")
	result, _ := ExecGit(dir, []string{"rev-parse", head})

	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "commit-tree" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := CreateCommit(dir, CommitOptions{Message: "child", Parent: result.Stdout})
	if err == nil {
		t.Fatal("CreateCommit() with parent should error when commit-tree fails")
	}
	if !errors.Is(err, ErrGitCommit) {
		t.Errorf("error = %v, want ErrGitCommit", err)
	}
}

func TestGetCommit_errorPath(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "log" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	// GetCommits swallows errors (returns empty), so GetCommit returns nil
	commit, err := GetCommit(dir, "HEAD")
	if err != nil {
		t.Fatalf("GetCommit() error = %v", err)
	}
	if commit != nil {
		t.Error("GetCommit() should return nil when GetCommits returns empty")
	}
}

func TestGetUnpushedCommits_revListError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	dir := initTestRepo(t)
	bareDir := t.TempDir()
	ExecGit(bareDir, []string{"init", "--bare"})
	ExecGit(dir, []string{"remote", "add", "origin", bareDir})
	ExecGit(dir, []string{"push", "origin", "main"})
	ExecGit(dir, []string{"fetch", "origin"})

	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "rev-list" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	hashes, err := GetUnpushedCommits(dir, "main")
	if err != nil {
		t.Fatalf("GetUnpushedCommits() error = %v", err)
	}
	if len(hashes) != 0 {
		t.Errorf("len(hashes) = %d, want 0 on rev-list error", len(hashes))
	}
}

func TestListLocalBranches_empty(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "for-each-ref" {
			return &ExecResult{Stdout: ""}, nil, true
		}
		return nil, nil, false
	}))
	defer restore()

	branches, err := ListLocalBranches(dir)
	if err != nil {
		t.Fatalf("ListLocalBranches() error = %v", err)
	}
	if branches != nil {
		t.Errorf("ListLocalBranches() = %v, want nil for empty output", branches)
	}
}

func TestListRefs_error(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "for-each-ref" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	refs, err := ListRefs(dir, "social/")
	if err != nil {
		t.Fatalf("ListRefs() error = %v (should swallow errors)", err)
	}
	if len(refs) != 0 {
		t.Errorf("len(refs) = %d, want 0 on error", len(refs))
	}
}

func TestValidatePushPreconditions_emptyRemoteName(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	ExecGit(dir, []string{"remote", "add", "origin", "https://github.com/user/repo.git"})

	// Empty remoteName should default to "origin"
	err := ValidatePushPreconditions(dir, "", "main")
	if err != nil {
		t.Errorf("ValidatePushPreconditions() error = %v", err)
	}
}

func TestGetCommitRange_error(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "log" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	commits, err := GetCommitRange(dir, "a", "b")
	if err != nil {
		t.Fatalf("GetCommitRange() error = %v (should swallow errors)", err)
	}
	if len(commits) != 0 {
		t.Errorf("len(commits) = %d, want 0 on error", len(commits))
	}
}

func TestMergeBranches_fastForward_plumbing(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	ExecGit(dir, []string{"checkout", "-b", "feature"})
	CreateCommit(dir, CommitOptions{Message: "feat", AllowEmpty: true})
	ExecGit(dir, []string{"checkout", "main"})
	// Switch to other branch so plumbing fast-forward path is used
	ExecGit(dir, []string{"checkout", "-b", "other"})

	hash, err := MergeBranches(dir, "main", "feature")
	if err != nil {
		t.Fatalf("MergeBranches() error = %v", err)
	}
	if hash == "" {
		t.Error("merge hash should not be empty")
	}
}

func TestGetDefaultBranch_noMainNoMaster(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	Init(dir, "develop")
	ExecGit(dir, []string{"config", "user.email", "test@test.com"})
	ExecGit(dir, []string{"config", "user.name", "Test User"})
	CreateCommit(dir, CommitOptions{Message: "initial", AllowEmpty: true})

	// Detach HEAD so symbolic-ref fails
	result, err := ExecGit(dir, []string{"rev-parse", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse HEAD error = %v", err)
	}
	ExecGit(dir, []string{"checkout", "--detach", result.Stdout})

	branch, err := GetDefaultBranch(dir)
	if err != nil {
		t.Fatalf("GetDefaultBranch() error = %v", err)
	}
	if branch != "main" {
		t.Errorf("GetDefaultBranch() = %q, want main (final fallback)", branch)
	}
}

// --- diff.go ---

func TestGetFileDiff_noMatchingFile(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa\n"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "add"})
	base, _ := ReadRef(dir, "HEAD")

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("modified\n"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "modify"})
	head, _ := ReadRef(dir, "HEAD")

	diff, err := GetFileDiff(dir, base, head, "nonexistent.txt")
	if err != nil {
		t.Fatalf("GetFileDiff() error = %v", err)
	}
	if diff != nil {
		t.Error("GetFileDiff() should return nil for unchanged file")
	}
}

func TestGetDiffStats_binaryFile(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	os.WriteFile(filepath.Join(dir, "bin.dat"), []byte{0x00, 0x01, 0x02}, 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "add binary"})
	base, _ := ReadRef(dir, "HEAD")

	os.WriteFile(filepath.Join(dir, "bin.dat"), []byte{0x03, 0x04, 0x05, 0x06}, 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "modify binary"})
	head, _ := ReadRef(dir, "HEAD")

	stats, err := GetDiffStats(dir, base, head)
	if err != nil {
		t.Fatalf("GetDiffStats() error = %v", err)
	}
	if stats.Files != 1 {
		t.Errorf("Files = %d, want 1", stats.Files)
	}
}

func TestGetFileDiff_emptyParsedDiffs(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "diff" {
			// Return non-empty output that doesn't contain "diff --git" → parseDiff returns []
			return &ExecResult{Stdout: "some random output\nwithout diff headers"}, nil, true
		}
		return nil, nil, false
	}))
	defer restore()

	diff, err := GetFileDiff(dir, "a", "b", "file.txt")
	if err != nil {
		t.Fatalf("GetFileDiff() error = %v", err)
	}
	if diff != nil {
		t.Error("GetFileDiff() should return nil when parseDiff returns empty")
	}
}

func TestGetDiffStats_malformedLines(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) >= 2 && args[0] == "diff" && args[1] == "--numstat" {
			// Include empty line, short line (< 3 parts), and valid line
			return &ExecResult{Stdout: "\njust-one-field\n5\t3\tfile.go"}, nil, true
		}
		return nil, nil, false
	}))
	defer restore()

	stats, err := GetDiffStats(dir, "a", "b")
	if err != nil {
		t.Fatalf("GetDiffStats() error = %v", err)
	}
	// Only the valid line should count
	if stats.Files != 1 {
		t.Errorf("Files = %d, want 1", stats.Files)
	}
	if stats.Added != 5 {
		t.Errorf("Added = %d, want 5", stats.Added)
	}
}

func TestParseFileDiff_consecutiveDiffs(t *testing.T) {
	t.Parallel()
	// Two diff headers back-to-back (no hunks for first file)
	lines := []string{
		"diff --git a/first.go b/first.go",
		"index abc..def 100644",
		"diff --git a/second.go b/second.go",
		"index ghi..jkl 100644",
		"--- a/second.go",
		"+++ b/second.go",
		"@@ -1,1 +1,1 @@",
		"-old",
		"+new",
	}
	diffs := parseDiff(strings.Join(lines, "\n"))
	if len(diffs) != 2 {
		t.Fatalf("len(diffs) = %d, want 2", len(diffs))
	}
	if diffs[0].NewPath != "first.go" {
		t.Errorf("first diff NewPath = %q, want first.go", diffs[0].NewPath)
	}
}

func TestParseFileDiff_strayLineAfterHunks(t *testing.T) {
	t.Parallel()
	// A non-@@ line between two hunks exercises the else branch in the hunk loop
	lines := []string{
		"diff --git a/file.go b/file.go",
		"index abc..def 100644",
		"--- a/file.go",
		"+++ b/file.go",
		"@@ -1,1 +1,1 @@",
		"-old",
		"+new",
		"some stray line",
		"@@ -10,1 +10,1 @@",
		"-old2",
		"+new2",
	}
	fd, _ := parseFileDiff(lines, 0)
	if len(fd.Hunks) != 2 {
		t.Errorf("len(Hunks) = %d, want 2", len(fd.Hunks))
	}
}

func TestParseFileDiff_shortHeader(t *testing.T) {
	t.Parallel()
	lines := []string{
		"diff --git",
		"@@ -1,1 +1,1 @@",
		"-old",
		"+new",
	}
	fd, _ := parseFileDiff(lines, 0)
	if fd.OldPath != "" || fd.NewPath != "" {
		t.Errorf("paths should be empty for short header, got (%q, %q)", fd.OldPath, fd.NewPath)
	}
}

// --- remotes.go ---

func TestListRemotes_error(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) >= 2 && args[0] == "remote" && args[1] == "-v" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	_, err := ListRemotes(dir)
	if err == nil {
		t.Fatal("ListRemotes() should error")
	}
}

func TestGetCommits_malformedEntry(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "log" {
			// Return malformed entries with < 6 unit-sep-delimited parts
			return &ExecResult{Stdout: "\x1Eshort\x1Fentry\x1E\x1Fhash\x1Fdate\x1Fname\x1Femail\x1Fmsg\x1Fref"}, nil, true
		}
		return nil, nil, false
	}))
	defer restore()

	commits, err := GetCommits(dir, nil)
	if err != nil {
		t.Fatalf("GetCommits() error = %v", err)
	}
	// The malformed entry (2 parts) should be skipped, valid entry (6 parts) kept
	if len(commits) != 1 {
		t.Errorf("len(commits) = %d, want 1 (malformed skipped)", len(commits))
	}
}

func TestGetCommitRange_malformedEntry(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "log" {
			return &ExecResult{Stdout: "\x1Eshort\x1Fentry"}, nil, true
		}
		return nil, nil, false
	}))
	defer restore()

	commits, err := GetCommitRange(dir, "a", "b")
	if err != nil {
		t.Fatalf("GetCommitRange() error = %v", err)
	}
	if len(commits) != 0 {
		t.Errorf("len(commits) = %d, want 0 (malformed skipped)", len(commits))
	}
}

func TestGetRemoteDefaultBranch_emptyBranch(t *testing.T) {
	dir := initTestRepo(t)
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) >= 2 && args[0] == "ls-remote" {
			return &ExecResult{Stdout: "ref: refs/heads/\tHEAD"}, nil, true
		}
		return nil, nil, false
	}))
	defer restore()

	branch := GetRemoteDefaultBranch(dir, "https://example.com/repo.git")
	if branch != "main" {
		t.Errorf("GetRemoteDefaultBranch() = %q, want main (fallback for empty branch)", branch)
	}
}

// --- utils.go ---

func TestGetFetchStartDate_alwaysMonday(t *testing.T) {
	t.Parallel()
	date := GetFetchStartDate()
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		t.Fatalf("invalid date format: %v", err)
	}
	if parsed.Weekday() != time.Monday {
		t.Errorf("GetFetchStartDate() = %s (%s), should be a Monday", date, parsed.Weekday())
	}
}
