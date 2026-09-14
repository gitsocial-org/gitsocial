// address_test.go - The address a followed repository or fork is fetched from
package fetch

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/storage"
)

// pushToNamedBare pushes a repo to a bare repo of the given directory name, and returns its file URL.
// Names here end in .git twice: git's local lookup resolves one .git suffix on its
// own, so a single suffix would leave the identity reachable and prove nothing.
func pushToNamedBare(t *testing.T, repoDir, name string) string {
	t.Helper()
	bareDir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(bareDir, 0o755); err != nil {
		t.Fatalf("create bare dir: %v", err)
	}
	if _, err := git.ExecGit(bareDir, []string{"init", "--bare"}); err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	if _, err := git.ExecGit(repoDir, []string{"remote", "add", "origin", bareDir}); err != nil {
		t.Fatalf("add origin: %v", err)
	}
	if _, err := git.ExecGit(repoDir, []string{"push", "origin", "--all"}); err != nil {
		t.Fatalf("push to bare repo: %v", err)
	}
	return "file://" + bareDir
}

// TestFetchAll_followsByAddress fetches a followed repository from the spelling the user gave.
func TestFetchAll_followsByAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	repoDir, _ := initTestRepo(t, 2)
	address := pushToNamedBare(t, repoDir, "followed.git.git")
	identity := protocol.NormalizeURL(address)
	if identity == address {
		t.Fatalf("identity %q should differ from the address %q", identity, address)
	}

	wsDir, _ := initTestRepo(t, 1)
	cacheDir := t.TempDir()
	res := FetchAll(wsDir, cacheDir, &Options{}, []RepoInfo{
		{URL: identity, Address: address, Branch: "main", ListID: "address-list"},
	}, nil, nil)
	if !res.Success {
		t.Fatalf("FetchAll() failed: %v", res.Error)
	}
	if len(res.Data.Errors) > 0 {
		t.Fatalf("FetchAll() errors = %+v, want none (the address names the only repo that exists)", res.Data.Errors)
	}
	if res.Data.Items < 2 {
		t.Errorf("items = %d, want >= 2", res.Data.Items)
	}

	// The cache is keyed by the identity.
	cached, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow("SELECT COUNT(*) FROM core_commits WHERE repo_url = ?", identity).Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("QueryLocked() error = %v", err)
	}
	if cached < 2 {
		t.Errorf("commits cached under %q = %d, want >= 2", identity, cached)
	}

	// Storage is named from the identity and fetches from the address.
	storageDir := storage.GetStorageDir(cacheDir, identity)
	upstream, err := git.ExecGit(storageDir, []string{"config", "--get", "remote.upstream.url"})
	if err != nil {
		t.Fatalf("read remote.upstream.url: %v", err)
	}
	if got := strings.TrimSpace(upstream.Stdout); got != address {
		t.Errorf("remote.upstream.url = %q, want the address %q", got, address)
	}

	// A re-fetch that knows only the identity leaves the address alone.
	if res := FetchRepository(cacheDir, identity, "main", "", nil, nil); !res.Success {
		t.Fatalf("FetchRepository() by identity failed: %v", res.Error)
	}
	upstream, err = git.ExecGit(storageDir, []string{"config", "--get", "remote.upstream.url"})
	if err != nil {
		t.Fatalf("read remote.upstream.url: %v", err)
	}
	if got := strings.TrimSpace(upstream.Stdout); got != address {
		t.Errorf("remote.upstream.url after an identity re-fetch = %q, want the address %q", got, address)
	}
}

// TestFetchForks_byAddress fetches a registered fork from the spelling the user gave.
func TestFetchForks_byAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	forkDir, _ := initTestRepo(t, 1)
	if _, err := git.ExecGit(forkDir, []string{"checkout", "-b", "gitmsg/social"}); err != nil {
		t.Fatalf("create the fork's gitmsg branch: %v", err)
	}
	if _, err := git.CreateCommit(forkDir, git.CommitOptions{Message: "fork post", AllowEmpty: true}); err != nil {
		t.Fatalf("commit on the fork's gitmsg branch: %v", err)
	}
	address := pushToNamedBare(t, forkDir, "fork.git.git")
	identity := protocol.NormalizeURL(address)

	wsDir, _ := initTestRepo(t, 1)
	cacheDir := t.TempDir()
	// Registered under the identity first, the fork remote is created at a URL that serves nothing.
	if err := gitmsg.AddFork(wsDir, identity); err != nil {
		t.Fatalf("AddFork() by identity error = %v", err)
	}
	if stats := FetchForks(wsDir, cacheDir, nil); len(stats.Errors) == 0 {
		t.Fatalf("FetchForks() by identity succeeded, want the fetch to fail at %q", identity)
	}
	if err := gitmsg.RemoveFork(wsDir, identity); err != nil {
		t.Fatalf("RemoveFork() error = %v", err)
	}
	if err := gitmsg.AddFork(wsDir, address); err != nil {
		t.Fatalf("AddFork() error = %v", err)
	}
	if forks := gitmsg.GetForks(wsDir); len(forks) != 1 || forks[0] != identity {
		t.Fatalf("GetForks() = %v, want [%s]", forks, identity)
	}

	stats := FetchForks(wsDir, cacheDir, nil)
	if len(stats.Errors) > 0 {
		t.Fatalf("FetchForks() errors = %+v, want none", stats.Errors)
	}
	if stats.Items < 1 {
		t.Errorf("items = %d, want >= 1", stats.Items)
	}

	// The remote name hashes the identity; its URL is the address.
	sharedDir, err := storage.EnsureForkRepository(cacheDir, gitmsg.ResolveRepoURL(wsDir))
	if err != nil {
		t.Fatalf("EnsureForkRepository() error = %v", err)
	}
	remote, err := git.ExecGit(sharedDir, []string{"config", "--get", "remote.remote-" + URLHash(identity) + ".url"})
	if err != nil {
		t.Fatalf("read the fork remote URL: %v", err)
	}
	if got := strings.TrimSpace(remote.Stdout); got != address {
		t.Errorf("fork remote URL = %q, want the address %q", got, address)
	}
}
