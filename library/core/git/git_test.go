// git_test.go - Tests for git package operations
package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

var baseRepoDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "git-test-base-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	Init(dir, "main")
	ExecGit(dir, []string{"config", "user.email", "test@test.com"})
	ExecGit(dir, []string{"config", "user.name", "Test User"})
	CreateCommit(dir, CommitOptions{Message: "Initial commit", AllowEmpty: true})
	baseRepoDir = dir
	os.Exit(m.Run())
}

// cloneFixture copies the base repo into a t.TempDir() via cp -a.
func cloneFixture(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	cmd := exec.Command("cp", "-a", baseRepoDir+"/.", dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cloneFixture: %v: %s", err, out)
	}
	return dst
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	return cloneFixture(t)
}

func TestIsRepository(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	if IsRepository(tmpDir) {
		t.Error("Expected non-repo dir to return false")
	}

	err := Init(tmpDir, "main")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if !IsRepository(tmpDir) {
		t.Error("Expected initialized repo to return true")
	}
}

func TestInit(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	err := Init(tmpDir, "main")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	gitDir := filepath.Join(tmpDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Error("Expected .git directory to exist")
	}
}

func TestCreateCommit(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	err := Init(tmpDir, "main")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	ExecGit(tmpDir, []string{"config", "user.email", "test@test.com"})
	ExecGit(tmpDir, []string{"config", "user.name", "Test User"})

	hash, err := CreateCommit(tmpDir, CommitOptions{
		Message:    "Initial commit",
		AllowEmpty: true,
	})
	if err != nil {
		t.Fatalf("CreateCommit failed: %v", err)
	}

	if len(hash) != 12 {
		t.Errorf("Expected 12-char hash, got %d chars: %s", len(hash), hash)
	}
}

func TestGetCurrentBranch(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	err := Init(tmpDir, "main")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	ExecGit(tmpDir, []string{"config", "user.email", "test@test.com"})
	ExecGit(tmpDir, []string{"config", "user.name", "Test User"})

	_, err = CreateCommit(tmpDir, CommitOptions{
		Message:    "Initial commit",
		AllowEmpty: true,
	})
	if err != nil {
		t.Fatalf("CreateCommit failed: %v", err)
	}

	branch, err := GetCurrentBranch(tmpDir)
	if err != nil {
		t.Fatalf("GetCurrentBranch failed: %v", err)
	}

	if branch != "main" {
		t.Errorf("Expected 'main', got '%s'", branch)
	}
}

func TestMergeCommitsChronologically(t *testing.T) {
	t.Parallel()
	now := time.Now()
	c1 := Commit{Hash: "a", Timestamp: now.Add(-1 * time.Hour)}
	c2 := Commit{Hash: "b", Timestamp: now}
	c3 := Commit{Hash: "c", Timestamp: now.Add(-2 * time.Hour)}

	merged := MergeCommitsChronologically([]Commit{c1}, []Commit{c2, c3})

	if len(merged) != 3 {
		t.Fatalf("Expected 3 commits, got %d", len(merged))
	}

	if merged[0].Hash != "b" {
		t.Errorf("Expected newest first, got %s", merged[0].Hash)
	}
	if merged[2].Hash != "c" {
		t.Errorf("Expected oldest last, got %s", merged[2].Hash)
	}
}

func TestGetFetchStartDate(t *testing.T) {
	t.Parallel()
	date := GetFetchStartDate()
	if len(date) != 10 {
		t.Errorf("Expected YYYY-MM-DD format, got %s", date)
	}
}

func TestGitError(t *testing.T) {
	t.Parallel()
	err := &GitError{
		Op:     "git status",
		Err:    ErrGitExec,
		Stderr: "fatal: not a git repository",
		Code:   128,
	}

	msg := err.Error()
	if msg != "git status: fatal: not a git repository" {
		t.Errorf("Unexpected error message: %s", msg)
	}
}

func TestGitError_noStderr(t *testing.T) {
	t.Parallel()
	err := &GitError{Op: "git push", Err: ErrGitExec}
	if err.Error() != "git push: git command failed" {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestGitError_noErr(t *testing.T) {
	t.Parallel()
	err := &GitError{Op: "git push"}
	if err.Error() != "git push: unknown error" {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestGitError_unwrap(t *testing.T) {
	t.Parallel()
	inner := ErrGitExec
	err := &GitError{Op: "test", Err: inner}
	if err.Unwrap() != inner {
		t.Error("Unwrap() should return inner error")
	}
}

func TestGetRootDir(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	root, err := GetRootDir(dir)
	if err != nil {
		t.Fatalf("GetRootDir() error = %v", err)
	}
	if root == "" {
		t.Error("GetRootDir() returned empty string")
	}
}

func TestGetUserEmail(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	email := GetUserEmail(dir)
	if email != "test@test.com" {
		t.Errorf("GetUserEmail() = %q, want %q", email, "test@test.com")
	}
}

func TestGetUserEmail_fromRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	Init(dir, "main")
	ExecGit(dir, []string{"config", "user.email", "local@test.com"})

	email := GetUserEmail(dir)
	if email != "local@test.com" {
		t.Errorf("GetUserEmail() = %q, want %q", email, "local@test.com")
	}
}

func TestGetCommits(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	CreateCommit(dir, CommitOptions{Message: "Second commit", AllowEmpty: true})

	commits, err := GetCommits(dir, &GetCommitsOptions{Branch: "main", Limit: 10})
	if err != nil {
		t.Fatalf("GetCommits() error = %v", err)
	}
	if len(commits) < 2 {
		t.Errorf("len(commits) = %d, want >= 2", len(commits))
	}
}

func TestGetCommits_withLimit(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	CreateCommit(dir, CommitOptions{Message: "Second", AllowEmpty: true})
	CreateCommit(dir, CommitOptions{Message: "Third", AllowEmpty: true})

	commits, err := GetCommits(dir, &GetCommitsOptions{Branch: "main", Limit: 1})
	if err != nil {
		t.Fatalf("GetCommits() error = %v", err)
	}
	if len(commits) != 1 {
		t.Errorf("len(commits) = %d, want 1 (limited)", len(commits))
	}
}

func TestGetCommit(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	hash, _ := CreateCommit(dir, CommitOptions{Message: "Test message", AllowEmpty: true})

	commit, err := GetCommit(dir, hash)
	if err != nil {
		t.Fatalf("GetCommit() error = %v", err)
	}
	if commit == nil {
		t.Fatal("GetCommit() returned nil")
	}
	if commit.Message == "" {
		t.Error("Message should not be empty")
	}
}

func TestGetCommitMessage(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	hash, _ := CreateCommit(dir, CommitOptions{Message: "Specific message", AllowEmpty: true})

	msg, err := GetCommitMessage(dir, hash)
	if err != nil {
		t.Fatalf("GetCommitMessage() error = %v", err)
	}
	if msg != "Specific message" {
		t.Errorf("GetCommitMessage() = %q, want %q", msg, "Specific message")
	}
}

func TestReadRef(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	val, err := ReadRef(dir, "HEAD")
	if err != nil {
		t.Fatalf("ReadRef() error = %v", err)
	}
	if val == "" {
		t.Error("ReadRef(HEAD) should not be empty")
	}
}

func TestWriteRef(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	head, _ := ReadRef(dir, "HEAD")
	err := WriteRef(dir, "refs/test/custom", head)
	if err != nil {
		t.Fatalf("WriteRef() error = %v", err)
	}

	val, _ := ReadRef(dir, "refs/test/custom")
	if val != head {
		t.Errorf("ReadRef() = %q, want %q", val, head)
	}
}

func TestBranchExists(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	if !BranchExists(dir, "main") {
		t.Error("BranchExists(main) should be true")
	}
	if BranchExists(dir, "nonexistent") {
		t.Error("BranchExists(nonexistent) should be false")
	}
}

func TestListLocalBranches(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	branches, err := ListLocalBranches(dir)
	if err != nil {
		t.Fatalf("ListLocalBranches() error = %v", err)
	}
	if len(branches) == 0 {
		t.Error("ListLocalBranches() returned empty")
	}
	found := false
	for _, b := range branches {
		if b == "main" {
			found = true
		}
	}
	if !found {
		t.Errorf("ListLocalBranches() = %v, missing 'main'", branches)
	}
}

func TestMergeCommitsChronologically_empty(t *testing.T) {
	t.Parallel()
	merged := MergeCommitsChronologically(nil, nil)
	if len(merged) != 0 {
		t.Errorf("len(merged) = %d, want 0", len(merged))
	}
}

func TestMergeCommitsChronologically_oneEmpty(t *testing.T) {
	t.Parallel()
	now := time.Now()
	commits := []Commit{{Hash: "a", Timestamp: now}}
	merged := MergeCommitsChronologically(commits, nil)
	if len(merged) != 1 {
		t.Errorf("len(merged) = %d, want 1", len(merged))
	}
}

func TestDeleteRef(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	head, _ := ReadRef(dir, "HEAD")
	WriteRef(dir, "refs/test/todelete", head)

	if err := DeleteRef(dir, "refs/test/todelete"); err != nil {
		t.Fatalf("DeleteRef() error = %v", err)
	}

	_, err := ReadRef(dir, "refs/test/todelete")
	if err == nil {
		t.Error("ReadRef should fail after DeleteRef")
	}
}

func TestExecGit(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	result, err := ExecGit(dir, []string{"status"})
	if err != nil {
		t.Fatalf("ExecGit() error = %v", err)
	}
	if result.Stdout == "" {
		t.Error("ExecGit(status) should produce output")
	}
}

func TestExecGitContext(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	result, err := ExecGitContext(context.Background(), dir, []string{"status"})
	if err != nil {
		t.Fatalf("ExecGitContext() error = %v", err)
	}
	if result.Stdout == "" {
		t.Error("ExecGitContext(status) should produce output")
	}
}

func TestExecGitContext_withTimeout(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := ExecGitContext(ctx, dir, []string{"status"})
	if err != nil {
		t.Fatalf("ExecGitContext() with timeout error = %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
}

func TestExecGit_errorCode(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	_, err := ExecGit(dir, []string{"log", "--invalid-flag-xyz"})
	if err == nil {
		t.Fatal("ExecGit() should error for invalid flag")
	}
	var gitErr *GitError
	if !errors.As(err, &gitErr) {
		t.Fatalf("expected GitError, got %T", err)
	}
	if gitErr.Code == 0 {
		t.Error("GitError.Code should be non-zero")
	}
}

func TestCreateCommitTree(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	hash, err := CreateCommitTree(dir, "test message", "")
	if err != nil {
		t.Fatalf("CreateCommitTree() error = %v", err)
	}
	if hash == "" {
		t.Error("hash should not be empty")
	}
}

func TestCreateCommitTree_withParent(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	head, _ := ReadRef(dir, "HEAD")

	// Resolve to full hash for parent
	result, _ := ExecGit(dir, []string{"rev-parse", head})
	fullHash := result.Stdout

	hash, err := CreateCommitTree(dir, "child commit", fullHash)
	if err != nil {
		t.Fatalf("CreateCommitTree() with parent error = %v", err)
	}
	if hash == "" {
		t.Error("hash should not be empty")
	}
}

func TestCreateOrphanBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	err := CreateOrphanBranch(dir, "orphan-test")
	if err != nil {
		t.Fatalf("CreateOrphanBranch() error = %v", err)
	}

	if !BranchExists(dir, "orphan-test") {
		t.Error("orphan branch should exist")
	}
}

func TestCreateCommitOnBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	hash, err := CreateCommitOnBranch(dir, "feature", "feature commit")
	if err != nil {
		t.Fatalf("CreateCommitOnBranch() error = %v", err)
	}
	if hash == "" {
		t.Error("hash should not be empty")
	}
	if !BranchExists(dir, "feature") {
		t.Error("feature branch should exist after CreateCommitOnBranch")
	}
}

func TestCreateCommitOnBranch_existingBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// Create first commit on branch
	hash1, err := CreateCommitOnBranch(dir, "feature", "first")
	if err != nil {
		t.Fatalf("first CreateCommitOnBranch() error = %v", err)
	}

	// Create second commit (should have parent)
	hash2, err := CreateCommitOnBranch(dir, "feature", "second")
	if err != nil {
		t.Fatalf("second CreateCommitOnBranch() error = %v", err)
	}
	if hash1 == hash2 {
		t.Error("second commit should have different hash")
	}
}

func TestGetDefaultBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	branch, err := GetDefaultBranch(dir)
	if err != nil {
		t.Fatalf("GetDefaultBranch() error = %v", err)
	}
	if branch != "main" {
		t.Errorf("GetDefaultBranch() = %q, want main", branch)
	}
}

func TestGetMergeBase(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// Create a second branch from main
	ExecGit(dir, []string{"checkout", "-b", "feature"})
	CreateCommit(dir, CommitOptions{Message: "feature commit", AllowEmpty: true})
	ExecGit(dir, []string{"checkout", "main"})
	CreateCommit(dir, CommitOptions{Message: "main commit", AllowEmpty: true})

	base, err := GetMergeBase(dir, "main", "feature")
	if err != nil {
		t.Fatalf("GetMergeBase() error = %v", err)
	}
	if base == "" {
		t.Error("merge base should not be empty")
	}
}

func TestMergeBranches_fastForward(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// Create a branch from main and add commits
	ExecGit(dir, []string{"checkout", "-b", "feature"})
	CreateCommit(dir, CommitOptions{Message: "feature1", AllowEmpty: true})
	ExecGit(dir, []string{"checkout", "main"})

	// Fast-forward main to feature (main is ancestor of feature)
	hash, err := MergeBranches(dir, "main", "feature")
	if err != nil {
		t.Fatalf("MergeBranches() error = %v", err)
	}
	if hash == "" {
		t.Error("merge hash should not be empty")
	}
}

func TestMergeBranches_currentBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// Create diverged branches
	ExecGit(dir, []string{"checkout", "-b", "feature"})
	os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "feature change"})

	ExecGit(dir, []string{"checkout", "main"})
	os.WriteFile(filepath.Join(dir, "main.txt"), []byte("main"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "main change"})

	// Merge feature into current branch (main)
	hash, err := MergeBranches(dir, "main", "feature")
	if err != nil {
		t.Fatalf("MergeBranches() error = %v", err)
	}
	if hash == "" {
		t.Error("merge hash should not be empty")
	}
}

func TestListRefs(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	head, _ := ReadRef(dir, "HEAD")
	result, _ := ExecGit(dir, []string{"rev-parse", head})
	fullHash := result.Stdout

	WriteRef(dir, "refs/gitmsg/social/config", fullHash)
	WriteRef(dir, "refs/gitmsg/social/lists/following", fullHash)

	refs, err := ListRefs(dir, "social/")
	if err != nil {
		t.Fatalf("ListRefs() error = %v", err)
	}
	if len(refs) < 2 {
		t.Errorf("len(refs) = %d, want >= 2", len(refs))
	}
}

func TestListRefs_empty(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	refs, err := ListRefs(dir, "nonexistent/")
	if err != nil {
		t.Fatalf("ListRefs() error = %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("len(refs) = %d, want 0", len(refs))
	}
}

func TestGetCommitRange(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	base, _ := ReadRef(dir, "HEAD")

	CreateCommit(dir, CommitOptions{Message: "second", AllowEmpty: true})
	CreateCommit(dir, CommitOptions{Message: "third", AllowEmpty: true})
	head, _ := ReadRef(dir, "HEAD")

	commits, err := GetCommitRange(dir, base, head)
	if err != nil {
		t.Fatalf("GetCommitRange() error = %v", err)
	}
	if len(commits) != 2 {
		t.Errorf("len(commits) = %d, want 2", len(commits))
	}
}

func TestGetUnpushedCommits(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// No remote tracking, should return empty map
	hashes, err := GetUnpushedCommits(dir, "main")
	if err != nil {
		t.Fatalf("GetUnpushedCommits() error = %v", err)
	}
	if len(hashes) != 0 {
		t.Errorf("len(hashes) = %d, want 0 (no remote)", len(hashes))
	}
}

func TestValidatePushPreconditions_noRemote(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	err := ValidatePushPreconditions(dir, "origin", "main")
	if err == nil {
		t.Fatal("should error when no remote")
	}
	if !errors.Is(err, ErrGitRemote) {
		t.Errorf("error = %v, want ErrGitRemote", err)
	}
}

func TestValidatePushPreconditions_noBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	ExecGit(dir, []string{"remote", "add", "origin", "https://github.com/user/repo.git"})

	err := ValidatePushPreconditions(dir, "origin", "nonexistent")
	if err == nil {
		t.Fatal("should error when branch doesn't exist")
	}
	if !errors.Is(err, ErrBranch) {
		t.Errorf("error = %v, want ErrBranch", err)
	}
}

func TestValidatePushPreconditions_valid(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	ExecGit(dir, []string{"remote", "add", "origin", "https://github.com/user/repo.git"})

	// Should pass — branch exists, remote configured, not detached
	err := ValidatePushPreconditions(dir, "origin", "main")
	if err != nil {
		t.Errorf("ValidatePushPreconditions() error = %v", err)
	}
}

func TestGetUpstreamBranch_noUpstream(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	_, err := GetUpstreamBranch(dir, "main")
	if err == nil {
		t.Error("should error when no upstream configured")
	}
}

func TestGetCommits_all(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	CreateCommit(dir, CommitOptions{Message: "second", AllowEmpty: true})

	commits, err := GetCommits(dir, &GetCommitsOptions{All: true})
	if err != nil {
		t.Fatalf("GetCommits() error = %v", err)
	}
	if len(commits) < 2 {
		t.Errorf("len(commits) = %d, want >= 2", len(commits))
	}
}

func TestGetCommits_since(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	yesterday := time.Now().Add(-24 * time.Hour)
	commits, err := GetCommits(dir, &GetCommitsOptions{
		Branch: "main",
		Since:  &yesterday,
	})
	if err != nil {
		t.Fatalf("GetCommits() error = %v", err)
	}
	if len(commits) == 0 {
		t.Error("should find commits since yesterday")
	}
}

func TestCreateCommit_withParent(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	head, _ := ReadRef(dir, "HEAD")

	// Resolve to full hash
	result, _ := ExecGit(dir, []string{"rev-parse", head})
	fullHash := result.Stdout

	hash, err := CreateCommit(dir, CommitOptions{
		Message: "child commit",
		Parent:  fullHash,
	})
	if err != nil {
		t.Fatalf("CreateCommit() with parent error = %v", err)
	}
	if hash == "" {
		t.Error("hash should not be empty")
	}
}

func TestCreateCommit_noChanges(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	_, err := CreateCommit(dir, CommitOptions{
		Message: "should fail",
	})
	if err != ErrNoChanges {
		t.Errorf("CreateCommit() error = %v, want ErrNoChanges", err)
	}
}

func TestCreateCommit_withFileChanges(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	os.WriteFile(filepath.Join(dir, "newfile.txt"), []byte("content"), 0644)

	hash, err := CreateCommit(dir, CommitOptions{Message: "add file"})
	if err != nil {
		t.Fatalf("CreateCommit() error = %v", err)
	}
	if hash == "" {
		t.Error("hash should not be empty")
	}
}

func TestMergeBranches_nonFastForward(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// Create diverged branches
	ExecGit(dir, []string{"checkout", "-b", "feature"})
	os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "feature change"})

	ExecGit(dir, []string{"checkout", "main"})
	os.WriteFile(filepath.Join(dir, "main.txt"), []byte("main"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "main change"})

	// Switch away from main so we hit the plumbing merge path
	ExecGit(dir, []string{"checkout", "feature"})

	hash, err := MergeBranches(dir, "main", "feature")
	if err != nil {
		t.Fatalf("MergeBranches() error = %v", err)
	}
	if hash == "" {
		t.Error("merge hash should not be empty")
	}
}

func TestMergeBranches_conflict(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// Create conflicting changes
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

	// Merge on current branch (main) with conflict
	_, err := MergeBranches(dir, "main", "feature")
	if err == nil {
		t.Error("MergeBranches() should error on conflict")
	}
}

func TestGetUnpushedCommits_withRemote(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	// Create a bare repo as "remote"
	bareDir := t.TempDir()
	ExecGit(bareDir, []string{"init", "--bare"})

	dir := initTestRepo(t)
	ExecGit(dir, []string{"remote", "add", "origin", bareDir})
	ExecGit(dir, []string{"push", "origin", "main"})

	// Create a new commit that isn't pushed
	CreateCommit(dir, CommitOptions{Message: "unpushed", AllowEmpty: true})
	ExecGit(dir, []string{"fetch", "origin"})

	hashes, err := GetUnpushedCommits(dir, "main")
	if err != nil {
		t.Fatalf("GetUnpushedCommits() error = %v", err)
	}
	if len(hashes) != 1 {
		t.Errorf("len(hashes) = %d, want 1", len(hashes))
	}
}

func TestGetDefaultBranch_detachedHead(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	head, _ := ReadRef(dir, "HEAD")
	result, _ := ExecGit(dir, []string{"rev-parse", head})
	fullHash := result.Stdout

	// Detach HEAD
	ExecGit(dir, []string{"checkout", "--detach", fullHash})

	branch, err := GetDefaultBranch(dir)
	if err != nil {
		t.Fatalf("GetDefaultBranch() error = %v", err)
	}
	// Should fallback to "main" since it exists
	if branch != "main" {
		t.Errorf("GetDefaultBranch() = %q, want main (fallback)", branch)
	}
}

func TestGetDefaultBranch_masterOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	Init(dir, "master")
	ExecGit(dir, []string{"config", "user.email", "test@test.com"})
	ExecGit(dir, []string{"config", "user.name", "Test User"})
	CreateCommit(dir, CommitOptions{Message: "initial", AllowEmpty: true})

	// Detach HEAD so symbolic-ref fails, and only "master" exists
	head, _ := ReadRef(dir, "HEAD")
	result, _ := ExecGit(dir, []string{"rev-parse", head})
	ExecGit(dir, []string{"checkout", "--detach", result.Stdout})

	branch, err := GetDefaultBranch(dir)
	if err != nil {
		t.Fatalf("GetDefaultBranch() error = %v", err)
	}
	if branch != "master" {
		t.Errorf("GetDefaultBranch() = %q, want master", branch)
	}
}

func TestValidatePushPreconditions_detachedHead(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	ExecGit(dir, []string{"remote", "add", "origin", "https://github.com/user/repo.git"})

	head, _ := ReadRef(dir, "HEAD")
	result, _ := ExecGit(dir, []string{"rev-parse", head})
	ExecGit(dir, []string{"checkout", "--detach", result.Stdout})

	err := ValidatePushPreconditions(dir, "origin", "main")
	if !errors.Is(err, ErrDetachedHead) {
		t.Errorf("error = %v, want ErrDetachedHead", err)
	}
}

func TestValidatePushPreconditions_diverged(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	bareDir := t.TempDir()
	ExecGit(bareDir, []string{"init", "--bare"})

	dir := initTestRepo(t)
	ExecGit(dir, []string{"remote", "add", "origin", bareDir})
	ExecGit(dir, []string{"push", "origin", "main"})
	ExecGit(dir, []string{"fetch", "origin"})

	// Create a local commit
	CreateCommit(dir, CommitOptions{Message: "local", AllowEmpty: true})

	// Create a commit on the remote to simulate divergence
	dir2 := t.TempDir()
	ExecGit(dir2, []string{"clone", "-b", "main", bareDir, dir2 + "/clone"})
	ExecGit(dir2+"/clone", []string{"config", "user.email", "test@test.com"})
	ExecGit(dir2+"/clone", []string{"config", "user.name", "Test"})
	os.WriteFile(filepath.Join(dir2, "clone", "remote.txt"), []byte("remote"), 0644)
	ExecGit(dir2+"/clone", []string{"add", "."})
	ExecGit(dir2+"/clone", []string{"commit", "-m", "remote change"})
	ExecGit(dir2+"/clone", []string{"push", "origin", "main"})

	// Fetch so local knows about remote's commit
	ExecGit(dir, []string{"fetch", "origin"})

	err := ValidatePushPreconditions(dir, "origin", "main")
	if !errors.Is(err, ErrDiverged) {
		t.Errorf("error = %v, want ErrDiverged", err)
	}
}

func TestGetRemoteDefaultBranch_localRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	bareDir := t.TempDir()
	ExecGit(bareDir, []string{"init", "--bare"})

	dir := initTestRepo(t)
	ExecGit(dir, []string{"remote", "add", "origin", bareDir})
	ExecGit(dir, []string{"push", "origin", "main"})

	// Should detect the default branch from the bare repo
	branch := GetRemoteDefaultBranch(dir, bareDir)
	if branch != "main" {
		t.Errorf("GetRemoteDefaultBranch() = %q, want main", branch)
	}
}

func TestGetCommit_notFound(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	commit, err := GetCommit(dir, "nonexistent-ref")
	if err != nil {
		// May error or return nil
		return
	}
	if commit != nil {
		t.Error("GetCommit() should return nil for nonexistent ref")
	}
}

func TestGetCommits_nilOpts(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	commits, err := GetCommits(dir, nil)
	if err != nil {
		t.Fatalf("GetCommits(nil) error = %v", err)
	}
	if len(commits) == 0 {
		t.Error("should find commits with nil opts")
	}
}

func TestGetCommits_withUntil(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	tomorrow := time.Now().Add(24 * time.Hour)
	commits, err := GetCommits(dir, &GetCommitsOptions{
		Branch: "main",
		Until:  &tomorrow,
	})
	if err != nil {
		t.Fatalf("GetCommits() error = %v", err)
	}
	if len(commits) == 0 {
		t.Error("should find commits until tomorrow")
	}
}

func TestGetCommits_includeRefs(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	CreateCommitOnBranch(dir, "gitmsg/social", "social commit")

	commits, err := GetCommits(dir, &GetCommitsOptions{
		Branch:      "main",
		IncludeRefs: []string{"refs/heads/gitmsg/social"},
	})
	if err != nil {
		t.Fatalf("GetCommits() error = %v", err)
	}
	if len(commits) < 2 {
		t.Errorf("len(commits) = %d, want >= 2 (main + included ref)", len(commits))
	}
}

func TestListLocalBranches_excludesGitmsg(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	CreateCommitOnBranch(dir, "gitmsg/social", "social")
	ExecGit(dir, []string{"checkout", "-b", "feature"})
	CreateCommit(dir, CommitOptions{Message: "feature", AllowEmpty: true})
	ExecGit(dir, []string{"checkout", "main"})

	branches, err := ListLocalBranches(dir)
	if err != nil {
		t.Fatalf("ListLocalBranches() error = %v", err)
	}
	for _, b := range branches {
		if b == "gitmsg/social" {
			t.Error("ListLocalBranches should exclude gitmsg/ branches")
		}
	}
}

func TestGetUpstreamBranch_emptyLocalBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	_, err := GetUpstreamBranch(dir, "")
	if err == nil {
		t.Error("should error with no upstream")
	}
}

func TestInit_noInitialBranch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := Init(dir, "")
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if !IsRepository(dir) {
		t.Error("should be a repository after Init")
	}
}

func TestGetRootDir_notARepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := GetRootDir(dir)
	if err == nil {
		t.Error("GetRootDir should error for non-repo")
	}
}

func TestGetUserEmail_notConfigured(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	Init(dir, "main")
	// Don't configure email
	email := GetUserEmail(dir)
	// May return system-level email or empty
	_ = email
}
