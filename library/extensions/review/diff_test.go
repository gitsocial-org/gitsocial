// diff_test.go - Tests for cross-repository diff resolution helpers
package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/storage"
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

// forkPRSetup builds the shape a cross-repo PR diff resolves against: a bare
// upstream holding main, and a workspace whose feature branch carries a change
// no other repo has. Returns the workspace, the upstream URL and a cache dir.
func forkPRSetup(t *testing.T) (workdir, upstreamURL, cacheDir string) {
	t.Helper()
	workdir = initTestRepo(t)
	upstreamURL = t.TempDir()
	if _, err := git.ExecGit(upstreamURL, []string{"init", "--bare"}); err != nil {
		t.Fatalf("init upstream: %v", err)
	}
	if _, err := git.ExecGit(workdir, []string{"push", upstreamURL, "main"}); err != nil {
		t.Fatalf("push upstream: %v", err)
	}
	// Advance upstream's main past the workspace, so the base side of the diff can
	// only come from upstream and never from the borrowed workspace ODB.
	git.ExecGit(upstreamURL, []string{"config", "user.email", "test@test.com"})
	git.ExecGit(upstreamURL, []string{"config", "user.name", "Test User"})
	pushed, err := git.ExecGit(upstreamURL, []string{"rev-parse", "main"})
	if err != nil {
		t.Fatalf("resolve upstream main: %v", err)
	}
	upstreamOnly, err := git.CreateCommit(upstreamURL, git.CommitOptions{Message: "upstream-only", Parent: strings.TrimSpace(pushed.Stdout)})
	if err != nil {
		t.Fatalf("commit upstream-only: %v", err)
	}
	if _, err := git.ExecGit(upstreamURL, []string{"update-ref", "refs/heads/main", upstreamOnly}); err != nil {
		t.Fatalf("advance upstream main: %v", err)
	}
	if _, err := git.ExecGit(workdir, []string{"checkout", "feature"}); err != nil {
		t.Fatalf("checkout feature: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "borrowed.txt"), []byte("borrowed\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := git.CreateCommit(workdir, git.CommitOptions{Message: "workspace-only change"}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return workdir, upstreamURL, t.TempDir()
}

// forkRepoDir returns the fork network repo a cross-repo diff against upstreamURL uses.
func forkRepoDir(t *testing.T, cacheDir, upstreamURL string) string {
	t.Helper()
	dir, err := storage.EnsureForkRepository(cacheDir, upstreamURL)
	if err != nil {
		t.Fatalf("EnsureForkRepository() error = %v", err)
	}
	return dir
}

func TestResolveDiffContext_borrowsWorkspaceObjects(t *testing.T) {
	t.Parallel()
	workdir, upstreamURL, cacheDir := forkPRSetup(t)

	ctx := ResolveDiffContext(workdir, cacheDir, upstreamURL+"#branch:main", "#branch:feature")

	if ctx.Error != "" {
		t.Fatalf("cross-repo diff did not resolve: %s", ctx.Error)
	}
	forkDir := forkRepoDir(t, cacheDir, upstreamURL)
	alternates := filepath.Join(forkDir, "objects", "info", "alternates")
	content, err := os.ReadFile(alternates)
	if err != nil {
		t.Fatalf("read alternates: %v", err)
	}
	want := filepath.Join(workdir, ".git", "objects")
	if got := strings.TrimSpace(string(content)); got != want {
		t.Errorf("alternates = %q, want %q", got, want)
	}
	// The diff reads the workspace side's commit, tree and blob through the borrow.
	diff, err := git.ExecGit(forkDir, []string{"diff", "--name-only", ctx.Base, ctx.Head})
	if err != nil {
		t.Fatalf("diff in fork repo: %v", err)
	}
	if !strings.Contains(diff.Stdout, "borrowed.txt") {
		t.Errorf("diff = %q, want the workspace-only file", diff.Stdout)
	}
	// None of those objects are the fork repo's own: without the alternate they vanish.
	tip, err := git.ExecGit(workdir, []string{"rev-parse", "feature"})
	if err != nil {
		t.Fatalf("resolve workspace tip: %v", err)
	}
	sha := strings.TrimSpace(tip.Stdout)
	os.Remove(alternates)
	if _, err := git.ExecGit(forkDir, []string{"cat-file", "-e", sha}); err == nil {
		t.Error("fork repo copied the workspace's objects; it should only borrow them")
	}
}

func TestResolveDiffContext_repairsPrunedForkRepo(t *testing.T) {
	t.Parallel()
	workdir, upstreamURL, cacheDir := forkPRSetup(t)

	if ctx := ResolveDiffContext(workdir, cacheDir, upstreamURL+"#branch:main", "#branch:feature"); ctx.Error != "" {
		t.Fatalf("first resolve failed: %s", ctx.Error)
	}
	// Prune the objects the fork repo resolved through, as a donor gc does, and
	// mark the directory so the rebuild is observable.
	forkDir := forkRepoDir(t, cacheDir, upstreamURL)
	pruneObjects(t, forkDir)
	marker := filepath.Join(forkDir, "marker")
	if err := os.WriteFile(marker, []byte("x"), 0644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	ctx := ResolveDiffContext(workdir, cacheDir, upstreamURL+"#branch:main", "#branch:feature")

	if ctx.Error != "" {
		t.Fatalf("pruned fork repo surfaced as a user-visible failure: %s", ctx.Error)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("fork repo was not rebuilt")
	}
	if _, err := git.ExecGit(forkDir, []string{"diff", "--name-only", ctx.Base, ctx.Head}); err != nil {
		t.Errorf("diff after repair: %v", err)
	}
}

func TestResolveDiffContext_bloblessWorkspaceIsNotADonor(t *testing.T) {
	t.Parallel()
	workdir, upstreamURL, cacheDir := forkPRSetup(t)
	// A blobless workspace cannot lend objects it fetches lazily.
	if _, err := git.ExecGit(workdir, []string{"config", "remote.origin.partialclonefilter", "blob:none"}); err != nil {
		t.Fatalf("set partialclonefilter: %v", err)
	}

	ctx := ResolveDiffContext(workdir, cacheDir, upstreamURL+"#branch:main", "#branch:feature")

	if ctx.Error != "" {
		t.Fatalf("blobless fallback did not resolve: %s", ctx.Error)
	}
	forkDir := forkRepoDir(t, cacheDir, upstreamURL)
	if _, err := os.Stat(filepath.Join(forkDir, "objects", "info", "alternates")); !os.IsNotExist(err) {
		t.Error("blobless workspace must not be borrowed from")
	}
	// The fallback fetch copied the workspace side in, so the diff still resolves.
	diff, err := git.ExecGit(forkDir, []string{"diff", "--name-only", ctx.Base, ctx.Head})
	if err != nil {
		t.Fatalf("diff in fork repo: %v", err)
	}
	if !strings.Contains(diff.Stdout, "borrowed.txt") {
		t.Errorf("diff = %q, want the workspace-only file", diff.Stdout)
	}
}

// pruneObjects empties a repo's object database, leaving refs and config intact —
// the state a borrower is left in when its donor gcs the objects it borrowed.
func pruneObjects(t *testing.T, dir string) {
	t.Helper()
	objects := filepath.Join(dir, "objects")
	entries, err := os.ReadDir(objects)
	if err != nil {
		t.Fatalf("read objects dir: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() == "info" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(objects, entry.Name())); err != nil {
			t.Fatalf("prune objects: %v", err)
		}
	}
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
