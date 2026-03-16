// diff_test.go - Tests for cross-repository diff resolution helpers
package review

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

func TestBranchValue_branchType(t *testing.T) {
	parsed := protocol.ParseRef("#branch:main")
	if got := branchValue(parsed, "#branch:main"); got != "main" {
		t.Errorf("branchValue() = %q, want main", got)
	}
}

func TestBranchValue_otherType(t *testing.T) {
	parsed := protocol.ParseRef("#commit:abc123")
	raw := "#commit:abc123"
	if got := branchValue(parsed, raw); got != raw {
		t.Errorf("branchValue() = %q, want %q", got, raw)
	}
}

func TestUrlHash_consistent(t *testing.T) {
	h1 := urlHash("https://github.com/user/repo")
	h2 := urlHash("https://github.com/user/repo")
	if h1 != h2 {
		t.Errorf("urlHash not consistent: %q != %q", h1, h2)
	}
	if len(h1) != 8 {
		t.Errorf("urlHash length = %d, want 8", len(h1))
	}
}

func TestUrlHash_different(t *testing.T) {
	h1 := urlHash("https://github.com/user/repo1")
	h2 := urlHash("https://github.com/user/repo2")
	if h1 == h2 {
		t.Error("urlHash should differ for different URLs")
	}
}

func TestDiffContext(t *testing.T) {
	t.Parallel()

	t.Run("localOnly", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		ctx := ResolveDiffContext(dir, t.TempDir(), "#branch:main", "#branch:feature")
		if ctx.Workdir != dir {
			t.Errorf("Workdir = %q, want workspace dir", ctx.Workdir)
		}
		if ctx.Base != "main" {
			t.Errorf("Base = %q, want main", ctx.Base)
		}
		if ctx.Head != "feature" {
			t.Errorf("Head = %q, want feature", ctx.Head)
		}
	})

	t.Run("workspaceURLMatch", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		ctx := ResolveDiffContext(dir, t.TempDir(), "https://github.com/test/repo#branch:main", "#branch:feature")
		if ctx.Workdir != dir {
			t.Errorf("Workdir should be workspace dir when URL matches")
		}
		if ctx.Base != "main" {
			t.Errorf("Base = %q, want main", ctx.Base)
		}
	})

	t.Run("headURLMatchesWorkspace", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		ctx := ResolveDiffContext(dir, t.TempDir(), "#branch:main", "https://github.com/test/repo#branch:feature")
		if ctx.Workdir != dir {
			t.Errorf("Workdir should be workspace dir when head URL matches")
		}
	})

	t.Run("remoteBase", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		cacheDir := t.TempDir()
		ctx := ResolveDiffContext(dir, cacheDir, "https://github.com/upstream/repo#branch:main", "#branch:feature")
		if ctx.Error == "" {
			t.Error("Error should report unfetchable remote base")
		}
	})

	t.Run("remoteHead", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		cacheDir := t.TempDir()
		ctx := ResolveDiffContext(dir, cacheDir, "#branch:main", "https://github.com/fork/repo#branch:feature")
		if ctx.Error == "" {
			t.Error("Error should report unfetchable remote head")
		}
	})

	t.Run("bothRemote", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		cacheDir := t.TempDir()
		ctx := ResolveDiffContext(dir, cacheDir, "https://github.com/upstream/repo#branch:main", "https://github.com/fork/repo#branch:feature")
		if ctx.Error == "" {
			t.Error("Error should report unfetchable remote refs")
		}
	})
}

func TestFetchHelpers(t *testing.T) {
	t.Parallel()

	t.Run("FetchFromUpstream", func(t *testing.T) {
		t.Parallel()
		srcDir := initTestRepo(t)
		bareDir := t.TempDir()
		git.ExecGit(bareDir, []string{"init", "--bare"})
		git.ExecGit(srcDir, []string{"push", bareDir, "main"})

		forkDir := t.TempDir()
		git.ExecGit(forkDir, []string{"init", "--bare"})

		fetchFromUpstream(forkDir, bareDir, "main")
	})

	t.Run("FetchFromUpstream_multipleRemotes", func(t *testing.T) {
		t.Parallel()
		srcDir := initTestRepo(t)
		bareDir := t.TempDir()
		git.ExecGit(bareDir, []string{"init", "--bare"})
		git.ExecGit(srcDir, []string{"push", bareDir, "main"})

		forkDir := t.TempDir()
		git.ExecGit(forkDir, []string{"init", "--bare"})

		// Fetch from two different URLs into same bare repo
		fetchFromUpstream(forkDir, bareDir, "main")
		fetchFromUpstream(forkDir, bareDir, "main") // idempotent
	})

	t.Run("FetchFromWorkspace", func(t *testing.T) {
		t.Parallel()
		srcDir := initTestRepo(t)
		forkDir := t.TempDir()
		git.ExecGit(forkDir, []string{"init", "--bare"})

		fetchFromWorkspace(forkDir, srcDir, "main")
	})
}
