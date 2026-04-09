// fetch_test.go - Tests for release fetch wrapper
package release

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/core/git"
)

func TestProcessors(t *testing.T) {
	procs := Processors()
	if len(procs) != 1 {
		t.Errorf("len(Processors()) = %d, want 1", len(procs))
	}
}

func TestFetchRepository_error(t *testing.T) {
	setupTestDB(t)
	res := FetchRepository(t.TempDir(), "file:///nonexistent/path/repo", "gitmsg/release")
	if res.Success {
		t.Error("FetchRepository() should fail for non-existent repo")
	}
}

func TestFetchRepository_defaultBranch(t *testing.T) {
	setupTestDB(t)
	res := FetchRepository(t.TempDir(), "file:///nonexistent/path/repo", "")
	if res.Success {
		t.Error("FetchRepository() should fail for non-existent repo")
	}
}

func TestFetchRepository_success(t *testing.T) {
	setupTestDB(t)
	srcDir := initTestRepo(t)
	git.CreateCommitOnBranch(srcDir, "gitmsg/release", "Release v1.0.0\n\n"+`GitMsg: ext="release"; tag="v1.0.0"; v="0.1.0"`)

	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})
	git.ExecGit(srcDir, []string{"push", bareDir, "gitmsg/release"})

	cacheDir := t.TempDir()
	res := FetchRepository(cacheDir, bareDir, "gitmsg/release")
	if !res.Success {
		t.Fatalf("FetchRepository() failed: %s", res.Error.Message)
	}
	if res.Data.Items < 1 {
		t.Errorf("expected at least 1 item, got %d", res.Data.Items)
	}
}
