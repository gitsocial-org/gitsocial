// mirror_test.go - Unit tests for the mirror command's resolution logic: URL
// classification, workspace origin matching, target-remote reuse, and the
// advisory lock.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// initMirrorTestRepo creates a git repo with an origin remote.
func initMirrorTestRepo(t *testing.T, originURL string) string {
	t.Helper()
	dir := t.TempDir()
	if err := git.Init(dir, "main"); err != nil {
		t.Fatalf("init: %v", err)
	}
	if originURL != "" {
		if _, err := git.ExecGit(dir, []string{"remote", "add", "origin", originURL}); err != nil {
			t.Fatalf("add origin: %v", err)
		}
	}
	return dir
}

func TestClassifyMirrorTarget(t *testing.T) {
	tests := []struct {
		arg      string
		wantKind string
		wantVal  string
		wantErr  bool
	}{
		{"https://github.com/octocat/Hello-World", "forge", "https://github.com/octocat/Hello-World", false},
		{"https://github.com/Octocat/Hello-World.git", "forge", "https://github.com/Octocat/Hello-World", false},
		{"https://gitlab.com/org/repo", "forge", "https://gitlab.com/org/repo", false},
		{"s3://127.0.0.1:9111/demo/hello", "s3", "s3://127.0.0.1:9111/demo/hello", false},
		{"s3://s3.us-east-1.amazonaws.com/bucket/repo", "s3", "s3://s3.us-east-1.amazonaws.com/bucket/repo", false},
		{"git@github.com:octocat/Hello-World.git", "", "", true},
		{"http://github.com/octocat/Hello-World", "", "", true},
		{"https://github.com/octocat", "", "", true},
		{"octocat/Hello-World", "", "", true},
		{"s3://hostonly", "", "", true},
	}
	for _, tt := range tests {
		kind, val, err := classifyMirrorTarget(tt.arg)
		if tt.wantErr {
			if err == nil {
				t.Errorf("classifyMirrorTarget(%q) = %q/%q, want error", tt.arg, kind, val)
			}
			continue
		}
		if err != nil {
			t.Errorf("classifyMirrorTarget(%q): %v", tt.arg, err)
			continue
		}
		if kind != tt.wantKind || val != tt.wantVal {
			t.Errorf("classifyMirrorTarget(%q) = %q/%q, want %q/%q", tt.arg, kind, val, tt.wantKind, tt.wantVal)
		}
	}
}

func TestClassifyMirrorArgs_orderFree(t *testing.T) {
	forge, s3 := "https://github.com/octocat/Hello-World", "s3://127.0.0.1:9111/demo/hello"
	for _, args := range [][]string{{forge, s3}, {s3, forge}} {
		gotForge, gotS3, err := classifyMirrorArgs(args)
		if err != nil {
			t.Fatalf("classifyMirrorArgs(%v): %v", args, err)
		}
		if gotForge != forge || gotS3 != s3 {
			t.Errorf("classifyMirrorArgs(%v) = %q, %q", args, gotForge, gotS3)
		}
	}
}

func TestClassifyMirrorArgs_duplicateKindRefused(t *testing.T) {
	if _, _, err := classifyMirrorArgs([]string{"https://github.com/a/b", "https://github.com/c/d"}); err == nil {
		t.Error("two forge URLs should be refused")
	}
	if _, _, err := classifyMirrorArgs([]string{"s3://h:1/b/p", "s3://h:2/b/p"}); err == nil {
		t.Error("two s3 URLs should be refused")
	}
}

func TestNormalizedRepoURLEqual(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"https://github.com/octocat/Hello-World.git", "https://github.com/octocat/Hello-World", true},
		{"https://GitHub.com/Octocat/Hello-World", "https://github.com/octocat/hello-world", true},
		{"https://github.com/octocat/Hello-World/", "https://github.com/octocat/Hello-World", true},
		{"https://github.com/octocat/Hello-World", "https://github.com/octocat/Other", false},
		{"", "https://github.com/octocat/Hello-World", false},
	}
	for _, tt := range tests {
		if got := normalizedRepoURLEqual(tt.a, tt.b); got != tt.want {
			t.Errorf("normalizedRepoURLEqual(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestResolveMirrorWorkspace(t *testing.T) {
	forge := "https://github.com/octocat/Hello-World"

	t.Run("absent dir clones", func(t *testing.T) {
		base := t.TempDir()
		dir, action, err := resolveMirrorWorkspace(base, "", forge)
		if err != nil {
			t.Fatalf("resolveMirrorWorkspace: %v", err)
		}
		if action != "clone" || dir != filepath.Join(base, "Hello-World") {
			t.Errorf("got %q/%q", dir, action)
		}
	})

	t.Run("matching origin fetches", func(t *testing.T) {
		repo := initMirrorTestRepo(t, forge+".git")
		dir, action, err := resolveMirrorWorkspace(filepath.Dir(repo), filepath.Base(repo), forge)
		if err != nil {
			t.Fatalf("resolveMirrorWorkspace: %v", err)
		}
		if action != "fetch" || dir != repo {
			t.Errorf("got %q/%q", dir, action)
		}
	})

	t.Run("foreign origin errors naming both", func(t *testing.T) {
		repo := initMirrorTestRepo(t, "https://github.com/other/repo")
		_, _, err := resolveMirrorWorkspace(filepath.Dir(repo), filepath.Base(repo), forge)
		if err == nil || !strings.Contains(err.Error(), "other/repo") || !strings.Contains(err.Error(), "Hello-World") {
			t.Errorf("want error naming both URLs, got %v", err)
		}
	})

	t.Run("non-repo dir errors", func(t *testing.T) {
		base := t.TempDir()
		if err := os.Mkdir(filepath.Join(base, "taken"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, _, err := resolveMirrorWorkspace(base, "taken", forge); err == nil {
			t.Error("non-repo directory should be refused")
		}
	})
}

func TestResolveMirrorTargets(t *testing.T) {
	canonical := "s3://127.0.0.1:9111/demo/hello"

	t.Run("fresh clone names s3", func(t *testing.T) {
		targets, err := resolveMirrorTargets("/nonexistent", canonical, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 1 || targets[0].name != "s3" || targets[0].exists {
			t.Errorf("targets = %+v", targets)
		}
	})

	t.Run("matching remote reused", func(t *testing.T) {
		repo := initMirrorTestRepo(t, "https://github.com/octocat/Hello-World")
		if _, err := git.ExecGit(repo, []string{"remote", "add", "bucket", canonical}); err != nil {
			t.Fatal(err)
		}
		targets, err := resolveMirrorTargets(repo, canonical, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 1 || targets[0].name != "bucket" || !targets[0].exists {
			t.Errorf("targets = %+v", targets)
		}
	})

	t.Run("no match picks a free name", func(t *testing.T) {
		repo := initMirrorTestRepo(t, "https://github.com/octocat/Hello-World")
		if _, err := git.ExecGit(repo, []string{"remote", "add", "s3", "s3://other:9111/x/y"}); err != nil {
			t.Fatal(err)
		}
		targets, err := resolveMirrorTargets(repo, canonical, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 1 || targets[0].name != "s3-2" || targets[0].exists {
			t.Errorf("targets = %+v", targets)
		}
	})

	t.Run("refresh without s3 push remote errors", func(t *testing.T) {
		repo := initMirrorTestRepo(t, "https://github.com/octocat/Hello-World")
		if _, err := resolveMirrorTargets(repo, "", false); err == nil {
			t.Error("refresh form without an s3 remote should error")
		}
	})
}

func TestAcquireMirrorLock(t *testing.T) {
	repo := initMirrorTestRepo(t, "")
	lockPath := func() string {
		out, err := git.ExecGit(repo, []string{"rev-parse", "--absolute-git-dir"})
		if err != nil {
			t.Fatal(err)
		}
		return filepath.Join(strings.TrimSpace(out.Stdout), "gitsocial-mirror.lock")
	}()

	t.Run("acquire and release", func(t *testing.T) {
		release, err := acquireMirrorLock(repo)
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		if _, err := os.Stat(lockPath); err != nil {
			t.Errorf("lock file missing: %v", err)
		}
		release()
		if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
			t.Error("release should remove the lock file")
		}
	})

	t.Run("live pid conflicts", func(t *testing.T) {
		release, err := acquireMirrorLock(repo)
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		defer release()
		if _, err := acquireMirrorLock(repo); err == nil {
			t.Error("second acquire under a live pid should fail")
		}
	})

	t.Run("stale pid is taken over", func(t *testing.T) {
		if err := os.WriteFile(lockPath, []byte("1073741824\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		release, err := acquireMirrorLock(repo)
		if err != nil {
			t.Fatalf("stale lock should be taken over: %v", err)
		}
		release()
	})
}
