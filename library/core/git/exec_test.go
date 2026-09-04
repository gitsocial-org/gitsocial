// exec_test.go - Tests for the environment handed to git subprocesses
package git

import (
	"path/filepath"
	"testing"
)

// An ambient GIT_DIR/GIT_WORK_TREE overrides cmd.Dir, so without the test-time
// strip in testIsolationEnv every git call in the suite lands in whatever repo
// those name — a test building a temp repo would commit into the developer's
// checkout instead.
func TestExecGitIgnoresAmbientGitDir(t *testing.T) {
	bystander := initTestRepo(t)
	target := initTestRepo(t)
	bystanderHead := revParse(t, bystander, "HEAD")

	t.Setenv("GIT_DIR", filepath.Join(bystander, ".git"))
	t.Setenv("GIT_WORK_TREE", bystander)

	hash, err := CreateCommit(target, CommitOptions{Message: "target commit", AllowEmpty: true})
	if err != nil {
		t.Fatalf("CreateCommit() error = %v", err)
	}
	if got := revParse(t, target, "HEAD"); got != revParse(t, target, hash) {
		t.Errorf("target HEAD = %s, want the new commit %s", got, hash)
	}
	if got := revParse(t, bystander, "HEAD"); got != bystanderHead {
		t.Errorf("bystander HEAD = %s, want %s unchanged (ambient GIT_DIR redirected the commit)", got, bystanderHead)
	}
}

// WriteRef is the other half of the redirect risk: it names a ref rather than a
// path, so an ambient GIT_DIR sends the write to the wrong repository silently.
func TestWriteRefIgnoresAmbientGitDir(t *testing.T) {
	bystander := initTestRepo(t)
	target := initTestRepo(t)
	hash := revParse(t, target, "HEAD")

	t.Setenv("GIT_DIR", filepath.Join(bystander, ".git"))

	if err := WriteRef(target, "refs/gitmsg/social/config", hash); err != nil {
		t.Fatalf("WriteRef() error = %v", err)
	}
	if _, err := ExecGit(target, []string{"rev-parse", "--verify", "refs/gitmsg/social/config"}); err != nil {
		t.Errorf("target is missing the ref that was just written: %v", err)
	}
	if _, err := ExecGit(bystander, []string{"rev-parse", "--verify", "refs/gitmsg/social/config"}); err == nil {
		t.Error("bystander gained refs/gitmsg/social/config (ambient GIT_DIR redirected the write)")
	}
}
