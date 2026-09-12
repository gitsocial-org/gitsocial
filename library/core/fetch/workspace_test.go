// workspace_test.go - Tests for the workspace sync window and its stale marking
package fetch

import (
	"database/sql"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

// datedCommit creates an empty commit with a fixed author and committer date and returns its short hash.
func datedCommit(t *testing.T, dir, message string, when time.Time) string {
	t.Helper()
	cmd := exec.Command("git", "commit", "-q", "--allow-empty", "-m", message)
	cmd.Dir = dir
	env := make([]string, 0, len(os.Environ())+6)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_DIR=") && !strings.HasPrefix(kv, "GIT_WORK_TREE=") {
			env = append(env, kv)
		}
	}
	stamp := when.Format(time.RFC3339)
	cmd.Env = append(env,
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@t.com",
		"GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit %q: %v\n%s", message, err, out)
	}
	res, err := git.ExecGit(dir, []string{"rev-parse", "--short=12", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(res.Stdout)
}

// countCachedCommits returns how many commits the cache holds for a repository.
func countCachedCommits(t *testing.T, repoURL string) int {
	t.Helper()
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow("SELECT COUNT(*) FROM core_commits WHERE repo_url = ?", repoURL).Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("count cached commits: %v", err)
	}
	return count
}

// isStale reports whether a cached commit carries a stale marker.
func isStale(t *testing.T, repoURL, hash string) bool {
	t.Helper()
	stale, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(
			"SELECT COUNT(*) FROM core_commits WHERE repo_url = ? AND hash = ? AND stale_since IS NOT NULL",
			repoURL, hash).Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("query stale marker: %v", err)
	}
	return stale > 0
}

// TestSyncWorkspaceLocal_secondSyncWalksTheNewCommitsAndStalesTheRemovedOne
// covers the window: the second sync ingests what landed since the newest
// cached commit, leaves the older history unwalked, and still marks a commit
// that left the repository stale.
func TestSyncWorkspaceLocal_secondSyncWalksTheNewCommitsAndStalesTheRemovedOne(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)

	dir := t.TempDir()
	if err := git.Init(dir, "main"); err != nil {
		t.Fatalf("git.Init() error = %v", err)
	}
	git.ExecGit(dir, []string{"config", "user.email", "t@t.com"})
	git.ExecGit(dir, []string{"config", "user.name", "T"})
	now := time.Now()
	oldest := datedCommit(t, dir, "oldest", now.AddDate(0, 0, -30))
	middle := datedCommit(t, dir, "middle", now.AddDate(0, 0, -20))
	datedCommit(t, dir, "newest cached", now)

	repoURL := gitmsg.ResolveRepoURL(dir)
	var walked []string
	procs := []WorkspaceSyncFunc{func(commits []git.Commit, _, _, _ string) {
		for _, c := range commits {
			walked = append(walked, c.Hash)
		}
	}}

	if _, err := SyncWorkspaceLocal(dir, procs); err != nil {
		t.Fatalf("first SyncWorkspaceLocal() error = %v", err)
	}
	if got := countCachedCommits(t, repoURL); got != 3 {
		t.Fatalf("cached commits after first sync = %d, want 3", got)
	}

	git.ExecGit(dir, []string{"checkout", "-q", "-b", "topic"})
	topic := datedCommit(t, dir, "on topic", now)
	git.ExecGit(dir, []string{"checkout", "-q", "main"})
	added := datedCommit(t, dir, "on main", now)

	walked = nil
	if _, err := SyncWorkspaceLocal(dir, procs); err != nil {
		t.Fatalf("second SyncWorkspaceLocal() error = %v", err)
	}
	for _, want := range []string{added, topic} {
		if !contains(walked, want) {
			t.Errorf("second sync did not walk %s", want)
		}
	}
	for _, skipped := range []string{oldest, middle} {
		if contains(walked, skipped) {
			t.Errorf("second sync walked %s, want the window to leave it out", skipped)
		}
	}
	if got := countCachedCommits(t, repoURL); got != 5 {
		t.Errorf("cached commits after second sync = %d, want 5", got)
	}

	git.ExecGit(dir, []string{"branch", "-q", "-D", "topic"})
	if _, err := SyncWorkspaceLocal(dir, procs); err != nil {
		t.Fatalf("third SyncWorkspaceLocal() error = %v", err)
	}
	if !isStale(t, repoURL, topic) {
		t.Errorf("commit %s left the repository and is not marked stale", topic)
	}
	if isStale(t, repoURL, oldest) {
		t.Errorf("commit %s is still in the repository and is marked stale", oldest)
	}
}

// contains reports whether a hash is in the list.
func contains(hashes []string, hash string) bool {
	for _, h := range hashes {
		if h == hash {
			return true
		}
	}
	return false
}
