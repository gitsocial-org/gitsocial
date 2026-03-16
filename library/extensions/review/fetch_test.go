// fetch_test.go - Tests for review fetch wrapper functions
package review

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
)

func TestProcessors(t *testing.T) {
	procs := Processors()
	if len(procs) != 1 {
		t.Errorf("len(Processors()) = %d, want 1", len(procs))
	}
	if procs[0] == nil {
		t.Error("Processors()[0] should not be nil")
	}
}

func TestFetchRepository_error(t *testing.T) {
	cacheDir := t.TempDir()
	cache.Reset()
	if err := cache.Open(cacheDir); err != nil {
		t.Fatalf("cache.Open() error = %v", err)
	}
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})

	res := FetchRepository(cacheDir, "file:///nonexistent/path/repo", "gitmsg/review")
	if res.Success {
		t.Error("FetchRepository() should fail for non-existent repo")
	}
}

func TestFetchRepository_defaultBranch(t *testing.T) {
	cacheDir := t.TempDir()
	cache.Reset()
	if err := cache.Open(cacheDir); err != nil {
		t.Fatalf("cache.Open() error = %v", err)
	}
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})

	res := FetchRepository(cacheDir, "file:///nonexistent/path/repo", "")
	if res.Success {
		t.Error("FetchRepository() should fail for non-existent repo")
	}
}

func TestFetchRepository_success(t *testing.T) {
	cacheDir := t.TempDir()
	cache.Reset()
	if err := cache.Open(cacheDir); err != nil {
		t.Fatalf("cache.Open() error = %v", err)
	}
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})

	srcDir := initTestRepo(t)
	git.CreateCommitOnBranch(srcDir, "gitmsg/review", "PR\n\n"+`--- GitMsg: ext="review"; type="pull-request"; state="open"; v="0.1.0" ---`)

	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})
	git.ExecGit(srcDir, []string{"push", bareDir, "gitmsg/review"})

	res := FetchRepository(cacheDir, bareDir, "gitmsg/review")
	if !res.Success {
		t.Fatalf("FetchRepository() failed: %s", res.Error.Message)
	}
	if res.Data.Items < 1 {
		t.Errorf("expected at least 1 item, got %d", res.Data.Items)
	}
}

func TestFetchForks(t *testing.T) {
	dir := initTestRepo(t)

	t.Run("noForks", func(t *testing.T) {
		stats := FetchForks(dir, t.TempDir())
		if stats.Forks != 0 {
			t.Errorf("Forks = %d, want 0", stats.Forks)
		}
		if stats.Items != 0 {
			t.Errorf("Items = %d, want 0", stats.Items)
		}
	})

	t.Run("withSuccess", func(t *testing.T) {
		srcDir := initTestRepo(t)
		git.CreateCommitOnBranch(srcDir, "gitmsg/review", "PR\n\n"+`--- GitMsg: ext="review"; type="pull-request"; state="open"; v="0.1.0" ---`)
		bareDir := t.TempDir()
		git.ExecGit(bareDir, []string{"init", "--bare"})
		git.ExecGit(srcDir, []string{"push", bareDir, "gitmsg/review"})

		SaveReviewConfig(dir, ReviewConfig{
			Version: "0.1.0",
			Forks:   []string{bareDir},
		})

		cacheDir := t.TempDir()
		cache.Reset()
		if err := cache.Open(cacheDir); err != nil {
			t.Fatalf("cache.Open() error = %v", err)
		}
		t.Cleanup(func() {
			cache.Reset()
			cache.Open(testCacheDir)
		})

		stats := FetchForks(dir, cacheDir)
		if stats.Forks != 1 {
			t.Errorf("Forks = %d, want 1", stats.Forks)
		}
		if stats.Items < 1 {
			t.Errorf("expected at least 1 item, got %d", stats.Items)
		}
		if len(stats.Errors) != 0 {
			t.Errorf("expected 0 errors, got %d", len(stats.Errors))
		}
	})

	t.Run("withError", func(t *testing.T) {
		SaveReviewConfig(dir, ReviewConfig{
			Version: "0.1.0",
			Forks:   []string{"file:///nonexistent/fork/repo"},
		})

		cacheDir := t.TempDir()
		cache.Reset()
		if err := cache.Open(cacheDir); err != nil {
			t.Fatalf("cache.Open() error = %v", err)
		}
		t.Cleanup(func() {
			cache.Reset()
			cache.Open(testCacheDir)
		})

		stats := FetchForks(dir, cacheDir)
		if stats.Forks != 1 {
			t.Errorf("Forks = %d, want 1", stats.Forks)
		}
		if len(stats.Errors) != 1 {
			t.Errorf("len(Errors) = %d, want 1", len(stats.Errors))
		}
	})
}
