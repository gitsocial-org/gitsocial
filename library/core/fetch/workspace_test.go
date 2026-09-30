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

// TestSyncWindow_itemInsertDoesNotStarve: a direct item insert stamped after a
// commit must not move the window past it; the watermark, not MAX(timestamp),
// bounds the walk.
func TestSyncWindow_itemInsertDoesNotStarve(t *testing.T) {
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
	datedCommit(t, dir, "base", now.Add(-time.Hour))
	repoURL := gitmsg.ResolveRepoURL(dir)
	if _, err := SyncWorkspaceLocal(dir, nil); err != nil {
		t.Fatalf("first SyncWorkspaceLocal() error = %v", err)
	}

	// The merged commit lands, then an item creation inserts its row directly,
	// stamped well after the commit's date (the starvation scenario).
	added := datedCommit(t, dir, "merged after the first sync", now)
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: "feedfeedfeedfeedfeedfeedfeedfeedfeedfeed", RepoURL: repoURL, Branch: "gitmsg/pm",
		AuthorName: "T", AuthorEmail: "t@t.com", Message: "an item created now", Timestamp: now.Add(10 * time.Minute),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}

	if _, err := SyncWorkspaceLocal(dir, nil); err != nil {
		t.Fatalf("second SyncWorkspaceLocal() error = %v", err)
	}
	found, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow("SELECT COUNT(*) FROM core_commits WHERE repo_url = ? AND hash LIKE ?", repoURL, added+"%").Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("query added commit: %v", err)
	}
	if found != 1 {
		t.Errorf("commit %s is not in the cache: the item insert starved the walk window", added)
	}
}

// syncTestRepo initializes a workspace with one commit an hour old and syncs it once.
func syncTestRepo(t *testing.T) (dir, repoURL string) {
	t.Helper()
	dir = t.TempDir()
	if err := git.Init(dir, "main"); err != nil {
		t.Fatalf("git.Init() error = %v", err)
	}
	git.ExecGit(dir, []string{"config", "user.email", "t@t.com"})
	git.ExecGit(dir, []string{"config", "user.name", "T"})
	datedCommit(t, dir, "base", time.Now().Add(-time.Hour))
	repoURL = gitmsg.ResolveRepoURL(dir)
	if _, err := SyncWorkspaceLocal(dir, nil); err != nil {
		t.Fatalf("first SyncWorkspaceLocal() error = %v", err)
	}
	return dir, repoURL
}

// cachedCount returns how many cached commits match a hash prefix.
func cachedCount(t *testing.T, repoURL, prefix string) int {
	t.Helper()
	found, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow("SELECT COUNT(*) FROM core_commits WHERE repo_url = ? AND hash LIKE ?", repoURL, prefix+"%").Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("query cached commit: %v", err)
	}
	return found
}

// TestSyncWindow_backdatedArrivalIngests: a commit whose commit date predates
// every previous sync still ingests when its ref arrives, since the walk
// windows by ancestry, not by any clock.
func TestSyncWindow_backdatedArrivalIngests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir, repoURL := syncTestRepo(t)
	late := datedCommit(t, dir, "committed long ago, arriving now", time.Now().AddDate(0, 0, -7))
	if _, err := SyncWorkspaceLocal(dir, nil); err != nil {
		t.Fatalf("second SyncWorkspaceLocal() error = %v", err)
	}
	if cachedCount(t, repoURL, late) != 1 {
		t.Errorf("backdated commit %s did not ingest", late)
	}
}

// TestSyncWindow_resetRepositoryRebuilds: a tip row surviving a repository
// reset must not window the next walk over an emptied cache.
func TestSyncWindow_resetRepositoryRebuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir, repoURL := syncTestRepo(t)
	if err := cache.ResetRepositoryData(repoURL); err != nil {
		t.Fatalf("ResetRepositoryData() error = %v", err)
	}
	// A new commit moves the tips so the gate lets the sync run.
	datedCommit(t, dir, "after the reset", time.Now())
	if _, err := SyncWorkspaceLocal(dir, nil); err != nil {
		t.Fatalf("SyncWorkspaceLocal() after reset error = %v", err)
	}
	if got := countCachedCommits(t, repoURL); got != 2 {
		t.Errorf("cached commits after reset and resync = %d, want 2 (full rebuild)", got)
	}
}

// TestSyncWorkspaceContinue_midWalkCommitIngests: a commit landing between the
// quick pass and the continuation is in the continuation's own delta.
func TestSyncWorkspaceContinue_midWalkCommitIngests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir, repoURL := syncTestRepo(t)
	datedCommit(t, dir, "second", time.Now())
	if err := SyncWorkspaceQuick(dir, nil); err != nil {
		t.Fatalf("SyncWorkspaceQuick() error = %v", err)
	}
	mid := datedCommit(t, dir, "landed mid-walk", time.Now())
	if err := SyncWorkspaceContinue(dir, nil, nil); err != nil {
		t.Fatalf("SyncWorkspaceContinue() error = %v", err)
	}
	if cachedCount(t, repoURL, mid) != 1 {
		t.Errorf("mid-walk commit %s did not ingest", mid)
	}
}

// TestFinalizeWorkspaceSync_skipsIncompleteWalk: the tip does not advance while
// a current tip commit is missing from the cache, so a failed walk retries.
func TestFinalizeWorkspaceSync_skipsIncompleteWalk(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir, repoURL := syncTestRepo(t)
	// A new commit whose walk "failed": nothing of it is in the cache.
	missing := datedCommit(t, dir, "walk missed me", time.Now())
	full, err := git.ExecGit(dir, []string{"rev-parse", missing})
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	tipKey := "workspace:" + repoURL
	before, _ := cache.GetSyncTip(tipKey)
	ctx := &workspaceSyncContext{workdir: dir, repoURL: repoURL, tipKey: tipKey, combinedTip: strings.TrimSpace(full.Stdout)}
	if err := finalizeWorkspaceSync(ctx); err != nil {
		t.Fatalf("finalizeWorkspaceSync() error = %v", err)
	}
	after, _ := cache.GetSyncTip(tipKey)
	if after != before {
		t.Errorf("tip advanced over an incomplete walk: %q -> %q", before, after)
	}
}

// TestFinalizeWorkspaceSync_mergeTipAdvances: a branch tipped by a merge commit
// does not wedge the completeness guard, since --no-merges keeps merges uncached.
func TestFinalizeWorkspaceSync_mergeTipAdvances(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir, repoURL := syncTestRepo(t)
	git.ExecGit(dir, []string{"checkout", "-q", "-b", "side"})
	datedCommit(t, dir, "side work", time.Now())
	git.ExecGit(dir, []string{"checkout", "-q", "main"})
	datedCommit(t, dir, "main work", time.Now())
	if _, err := git.ExecGit(dir, []string{"merge", "--no-ff", "-q", "-m", "merge side", "side"}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, err := SyncWorkspaceLocal(dir, nil); err != nil {
		t.Fatalf("SyncWorkspaceLocal() error = %v", err)
	}
	tip, _ := cache.GetSyncTip("workspace:" + repoURL)
	if !strings.HasPrefix(tip, ancestryMarker) {
		t.Errorf("tip did not advance past a merge-tipped branch: %q", tip)
	}
}

// TestSyncTip_oldFormatTriggersFullWalk: a tip row from the time-window code
// (no ancestry marker) yields one full walk, backfilling a starved cache.
func TestSyncTip_oldFormatTriggersFullWalk(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir, repoURL := syncTestRepo(t)
	starved := datedCommit(t, dir, "starved by the old window", time.Now())
	if _, err := SyncWorkspaceLocal(dir, nil); err != nil {
		t.Fatalf("second SyncWorkspaceLocal() error = %v", err)
	}
	// Simulate the pre-fix state: the commit's rows vanish while the tip,
	// stripped to the old unmarked format, still claims a finished sync.
	if err := cache.ExecLocked(func(db *sql.DB) error {
		if _, err := db.Exec("DELETE FROM core_commits WHERE repo_url = ? AND hash LIKE ?", repoURL, starved+"%"); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("delete starved rows: %v", err)
	}
	tipKey := "workspace:" + repoURL
	marked, _ := cache.GetSyncTip(tipKey)
	if err := cache.SetSyncTip(tipKey, strings.TrimPrefix(marked, ancestryMarker)); err != nil {
		t.Fatalf("SetSyncTip() error = %v", err)
	}
	if _, err := SyncWorkspaceLocal(dir, nil); err != nil {
		t.Fatalf("post-upgrade SyncWorkspaceLocal() error = %v", err)
	}
	if cachedCount(t, repoURL, starved) != 1 {
		t.Errorf("starved commit %s was not backfilled by the migration walk", starved)
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
