// workspace_test.go - Tests for the workspace sync: home branches, the ref gate and stale marking
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

// TestSyncWorkspaceLocal_secondSyncWalksTheNewCommitsAndStalesTheRemovedOne: the second sync processes only the commits the cache lacks, and a commit that left the repository goes stale.
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
			t.Errorf("second sync processed %s, which the cache already had", skipped)
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

// TestSyncWindow_itemInsertDoesNotStarve: a direct item insert stamped after a commit does not hide the commit from the next sync.
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

	// The merged commit lands, then an item creation inserts its row directly, stamped after the commit's date.
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

// TestSyncWindow_backdatedArrivalIngests: a commit dated before every previous sync gets its row when its ref arrives.
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

// TestSyncWindow_resetRepositoryRebuilds: a tip row that survives a repository reset does not keep the next sync from a rebuild.
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

// TestSyncWorkspaceContinue_midWalkCommitIngests: a commit that lands between the quick pass and the continuation gets its row.
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

// TestWorkspaceSync_tipNeedsFullState: the tip does not advance while a commit has no row under its home, so a failed read retries.
func TestWorkspaceSync_tipNeedsFullState(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir, repoURL := syncTestRepo(t)
	datedCommit(t, dir, "read failed for me", time.Now())
	ctx, err := resolveWorkspaceSyncContext(dir, nil)
	if err != nil || ctx == nil {
		t.Fatalf("the gate stayed closed after a new commit: %v", err)
	}
	lists, err := stableHashes(dir, ctx.stable, ctx.since, 0)
	if err != nil {
		t.Fatalf("stableHashes() error = %v", err)
	}
	before, _ := cache.GetSyncTip(ctx.tipKey)
	finalizeWorkspaceSync(ctx, ctx.stable, lists, lists)
	after, _ := cache.GetSyncTip("workspace:" + repoURL)
	if after != before {
		t.Errorf("tip advanced with a row missing: %q -> %q", before, after)
	}
}

// TestFinalizeWorkspaceSync_mergeTipAdvances: a branch tipped by a merge commit does not hold the tip back, because a merge has no row.
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
	if !strings.HasPrefix(tip, homeMarker) {
		t.Errorf("tip did not advance past a merge-tipped branch: %q", tip)
	}
}

// TestSyncTip_oldFormatTriggersSync: a tip row from older code (no home marker) opens the gate one time, and the sync adds the rows the cache lacks.
func TestSyncTip_oldFormatTriggersSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir, repoURL := syncTestRepo(t)
	starved := datedCommit(t, dir, "starved by the old window", time.Now())
	if _, err := SyncWorkspaceLocal(dir, nil); err != nil {
		t.Fatalf("second SyncWorkspaceLocal() error = %v", err)
	}
	// The state of an older cache: the commit has no row and the tip has no marker.
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
	if err := cache.SetSyncTip(tipKey, strings.TrimPrefix(marked, homeMarker)); err != nil {
		t.Fatalf("SetSyncTip() error = %v", err)
	}
	if _, err := SyncWorkspaceLocal(dir, nil); err != nil {
		t.Fatalf("post-upgrade SyncWorkspaceLocal() error = %v", err)
	}
	if cachedCount(t, repoURL, starved) != 1 {
		t.Errorf("starved commit %s was not added by the sync after the upgrade", starved)
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

// mustGit runs git in dir and fails the test on an error.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	res, err := git.ExecGit(dir, args)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(res.Stdout)
}

// initWorkspace creates a workspace on main with one commit an hour old.
func initWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := git.Init(dir, "main"); err != nil {
		t.Fatalf("git.Init() error = %v", err)
	}
	mustGit(t, dir, "config", "user.email", "t@t.com")
	mustGit(t, dir, "config", "user.name", "T")
	datedCommit(t, dir, "base", time.Now().Add(-time.Hour))
	return dir
}

// syncLocal runs the workspace sync with no extension and fails the test on an error.
func syncLocal(t *testing.T, dir string) {
	t.Helper()
	if _, err := SyncWorkspaceLocal(dir, nil); err != nil {
		t.Fatalf("SyncWorkspaceLocal() error = %v", err)
	}
}

// rowBranches returns hash to branch for the rows of a repository, the live ones or the stale ones.
func rowBranches(t *testing.T, repoURL string, stale bool) map[string]string {
	t.Helper()
	rows, err := cache.QueryLocked(func(db *sql.DB) (map[string]string, error) {
		res, err := db.Query("SELECT hash, branch FROM core_commits WHERE repo_url = ? AND (stale_since IS NOT NULL) = ?", repoURL, stale)
		if err != nil {
			return nil, err
		}
		defer res.Close()
		out := make(map[string]string)
		for res.Next() {
			var hash, branch string
			if err := res.Scan(&hash, &branch); err != nil {
				return nil, err
			}
			out[hash] = branch
		}
		return out, res.Err()
	})
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	return rows
}

// liveRows returns each live row of a repository as hash@branch, sorted, so a second live row of one hash shows.
func liveRows(t *testing.T, repoURL string) []string {
	t.Helper()
	rows, err := cache.QueryLocked(func(db *sql.DB) ([]string, error) {
		res, err := db.Query("SELECT hash || '@' || branch FROM core_commits WHERE repo_url = ? AND stale_since IS NULL ORDER BY 1", repoURL)
		if err != nil {
			return nil, err
		}
		defer res.Close()
		var out []string
		for res.Next() {
			var row string
			if err := res.Scan(&row); err != nil {
				return nil, err
			}
			out = append(out, row)
		}
		return out, res.Err()
	})
	if err != nil {
		t.Fatalf("query live rows: %v", err)
	}
	return rows
}

// TestWorkspaceSync_rebuildEqualsIncremental: after each step, the live rows of the long-lived cache equal the live rows of a cache built from the same refs.
func TestWorkspaceSync_rebuildEqualsIncremental(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir := initWorkspace(t)
	repoURL := gitmsg.ResolveRepoURL(dir)
	compare := func(step string) {
		t.Helper()
		syncLocal(t, dir)
		rebuilt := testutil.CopyRepo(t, dir)
		syncLocal(t, rebuilt)
		got, want := liveRows(t, repoURL), liveRows(t, gitmsg.ResolveRepoURL(rebuilt))
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s: live rows\n  %v\na rebuild has\n  %v", step, got, want)
		}
	}
	compare("first sync")
	mustGit(t, dir, "checkout", "-q", "-b", "feature/x")
	datedCommit(t, dir, "on the feature", time.Now())
	compare("feature commit")
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "merge", "-q", "--ff-only", "feature/x")
	compare("merge")
	mustGit(t, dir, "branch", "-q", "-D", "feature/x")
	compare("branch deleted")
	mustGit(t, dir, "branch", "-q", "aaa", "HEAD~1")
	datedCommit(t, dir, "after the merge", time.Now())
	compare("a branch at an old commit")
	mustGit(t, dir, "checkout", "-q", "-b", "stack/base")
	datedCommit(t, dir, "stack base", time.Now())
	mustGit(t, dir, "checkout", "-q", "-b", "stack/a-top")
	datedCommit(t, dir, "stack top", time.Now())
	mustGit(t, dir, "checkout", "-q", "main")
	compare("a stack")
	mustGit(t, dir, "reset", "-q", "--hard", "HEAD~1")
	compare("the default branch rewound")
}

// TestWorkspaceSync_mergeMovesHome: a merged commit has a live row under the default branch, and its row under the feature branch is stale.
func TestWorkspaceSync_mergeMovesHome(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir := initWorkspace(t)
	repoURL := gitmsg.ResolveRepoURL(dir)
	mustGit(t, dir, "checkout", "-q", "-b", "feature/x")
	commit := datedCommit(t, dir, "on the feature", time.Now())
	syncLocal(t, dir)
	if got := rowBranches(t, repoURL, false)[commit]; got != "feature/x" {
		t.Fatalf("home before the merge = %q, want feature/x", got)
	}
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "merge", "-q", "--ff-only", "feature/x")
	syncLocal(t, dir)
	if got := rowBranches(t, repoURL, false)[commit]; got != "main" {
		t.Errorf("live row after the merge = %q, want main", got)
	}
	if got := rowBranches(t, repoURL, true)[commit]; got != "feature/x" {
		t.Errorf("stale row after the merge = %q, want feature/x", got)
	}
}

// TestWorkspaceSync_contentBranchHome: a content commit is under its content branch when only the local ref has it, when only origin has it, and when a remote that sorts before origin has the tip.
func TestWorkspaceSync_contentBranchHome(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir := initWorkspace(t)
	repoURL := gitmsg.ResolveRepoURL(dir)
	mustGit(t, dir, "checkout", "-q", "-b", "gitmsg/pm")
	item := datedCommit(t, dir, "an issue", time.Now())
	mustGit(t, dir, "checkout", "-q", "main")
	check := func(step string) {
		t.Helper()
		syncLocal(t, dir)
		if got := rowBranches(t, repoURL, false)[item]; got != "gitmsg/pm" {
			t.Errorf("%s: home = %q, want gitmsg/pm", step, got)
		}
	}
	check("local ref only")
	pushToBare(t, dir, true)
	mustGit(t, dir, "branch", "-q", "-D", "gitmsg/pm")
	check("origin ref only")
	other := t.TempDir()
	mustGit(t, other, "init", "-q", "--bare")
	mustGit(t, dir, "remote", "add", "aaa", other)
	mustGit(t, dir, "push", "-q", "aaa", "refs/remotes/origin/gitmsg/pm:refs/heads/gitmsg/pm")
	datedCommit(t, dir, "moves the gate", time.Now())
	check("a remote before origin has the tip")
	for hash, branch := range rowBranches(t, repoURL, false) {
		if strings.HasPrefix(branch, "aaa/") || strings.HasPrefix(branch, "origin/") {
			t.Errorf("%s has a remote name in its branch value %q", hash, branch)
		}
	}
}

// TestWorkspaceSync_otherRemoteAddsNothing: a commit that only a remote other than origin has gets no row.
func TestWorkspaceSync_otherRemoteAddsNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir := initWorkspace(t)
	repoURL := gitmsg.ResolveRepoURL(dir)
	other := t.TempDir()
	mustGit(t, other, "init", "-q", "--bare")
	mustGit(t, dir, "remote", "add", "aaa", other)
	mustGit(t, dir, "checkout", "-q", "-b", "side")
	only := datedCommit(t, dir, "only on the other remote", time.Now())
	mustGit(t, dir, "push", "-q", "aaa", "side")
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "branch", "-q", "-D", "side")
	syncLocal(t, dir)
	if cachedCount(t, repoURL, only) != 0 {
		t.Errorf("commit %s of a remote other than origin is in the cache", only)
	}
}

// TestHomeBranch_precedence: a branch from an old commit of the default branch owns only the commit it adds, whatever its name.
func TestHomeBranch_precedence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir := initWorkspace(t)
	repoURL := gitmsg.ResolveRepoURL(dir)
	datedCommit(t, dir, "second", time.Now())
	datedCommit(t, dir, "third", time.Now())
	mustGit(t, dir, "checkout", "-q", "-b", "aaa", "HEAD~1")
	own := datedCommit(t, dir, "on aaa", time.Now())
	mustGit(t, dir, "checkout", "-q", "main")
	syncLocal(t, dir)
	live := rowBranches(t, repoURL, false)
	if len(live) != 4 {
		t.Errorf("live rows = %d, want 4", len(live))
	}
	for hash, branch := range live {
		want := "main"
		if hash == own {
			want = "aaa"
		}
		if branch != want {
			t.Errorf("%s is under %q, want %q", hash, branch, want)
		}
	}
}

// TestHomeBranch_stackAncestorFirst: in a stack each branch owns the commits it adds, for each choice of names; unrelated branches keep their own.
func TestHomeBranch_stackAncestorFirst(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	for _, names := range [][2]string{{"feature/zzz-base", "feature/aaa-top"}, {"feature/aaa-base", "feature/zzz-top"}} {
		lower, upper := names[0], names[1]
		t.Run(lower, func(t *testing.T) {
			testutil.OpenTempCache(t, sharedCacheDir)
			dir := initWorkspace(t)
			repoURL := gitmsg.ResolveRepoURL(dir)
			mustGit(t, dir, "checkout", "-q", "-b", lower)
			first := datedCommit(t, dir, "lower one", time.Now())
			second := datedCommit(t, dir, "lower two", time.Now())
			mustGit(t, dir, "checkout", "-q", "-b", upper)
			top := datedCommit(t, dir, "upper", time.Now())
			mustGit(t, dir, "checkout", "-q", "-b", "feature/mmm", "main")
			apart := datedCommit(t, dir, "unrelated", time.Now())
			mustGit(t, dir, "checkout", "-q", "main")
			syncLocal(t, dir)
			live := rowBranches(t, repoURL, false)
			for hash, want := range map[string]string{first: lower, second: lower, top: upper, apart: "feature/mmm"} {
				if live[hash] != want {
					t.Errorf("%s is under %q, want %q", hash, live[hash], want)
				}
			}
		})
	}
}

// TestWorkspaceSync_gateEqualsWalkSet: a change of a walked ref opens the gate, and a change of a ref that is not walked does not.
func TestWorkspaceSync_gateEqualsWalkSet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir, _ := syncTestRepo(t)
	head := mustGit(t, dir, "rev-parse", "HEAD")
	for _, ref := range []string{"refs/remotes/aaa/main", "refs/tags/v1", "refs/gitmsg/social/config", "refs/gitsocial/tracking/origin/gitmsg/social/config"} {
		mustGit(t, dir, "update-ref", ref, head)
		if ran, _ := SyncWorkspaceLocal(dir, nil); ran {
			t.Errorf("a change of %s opened the gate", ref)
		}
	}
	mustGit(t, dir, "branch", "-q", "renamed-later")
	if ran, _ := SyncWorkspaceLocal(dir, nil); !ran {
		t.Error("a new branch did not open the gate")
	}
	mustGit(t, dir, "branch", "-q", "-m", "renamed-later", "renamed")
	if ran, _ := SyncWorkspaceLocal(dir, nil); !ran {
		t.Error("a branch rename did not open the gate")
	}
}

// TestWorkspaceSync_skipsStateRefs: a commit that only a state ref or a tag reaches gets no row.
func TestWorkspaceSync_skipsStateRefs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir := initWorkspace(t)
	repoURL := gitmsg.ResolveRepoURL(dir)
	tree := mustGit(t, dir, "rev-parse", "HEAD^{tree}")
	stateRefs := []string{"refs/gitmsg/social/config", "refs/gitsocial/tracking/origin/gitmsg/social/config", "refs/tags/v1"}
	hidden := make([]string, 0, len(stateRefs))
	for _, ref := range stateRefs {
		hash := mustGit(t, dir, "commit-tree", "-m", "state of "+ref, tree)
		mustGit(t, dir, "update-ref", ref, hash)
		hidden = append(hidden, hash[:12])
	}
	syncLocal(t, dir)
	for _, hash := range hidden {
		if cachedCount(t, repoURL, hash) != 0 {
			t.Errorf("commit %s of a ref that is not a branch is in the cache", hash)
		}
	}
	if got := countCachedCommits(t, repoURL); got != 1 {
		t.Errorf("cached commits = %d, want 1", got)
	}
}

// TestWorkspaceSync_selfHeals: a cache with a content commit under a remote name converges in one sync, and the extension gets the commit on its branch.
func TestWorkspaceSync_selfHeals(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir := initWorkspace(t)
	repoURL := gitmsg.ResolveRepoURL(dir)
	mustGit(t, dir, "checkout", "-q", "-b", "gitmsg/pm")
	item := datedCommit(t, dir, "an issue", time.Now())
	mustGit(t, dir, "checkout", "-q", "main")
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: item, RepoURL: repoURL, Branch: "codeberg/gitmsg/pm",
		AuthorName: "T", AuthorEmail: "t@t.com", Message: "an issue", Timestamp: time.Now(),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	var onContentBranch []string
	procs := []WorkspaceSyncFunc{func(commits []git.Commit, _, _, _ string) {
		for _, c := range commits {
			if CleanRefname(c.Refname) == "gitmsg/pm" {
				onContentBranch = append(onContentBranch, c.Hash)
			}
		}
	}}
	if _, err := SyncWorkspaceLocal(dir, procs); err != nil {
		t.Fatalf("SyncWorkspaceLocal() error = %v", err)
	}
	if !contains(onContentBranch, item) {
		t.Errorf("the extension did not get %s on gitmsg/pm", item)
	}
	if got := rowBranches(t, repoURL, false)[item]; got != "gitmsg/pm" {
		t.Errorf("live row = %q, want gitmsg/pm", got)
	}
	if got := rowBranches(t, repoURL, true)[item]; got != "codeberg/gitmsg/pm" {
		t.Errorf("stale row = %q, want codeberg/gitmsg/pm", got)
	}
}

// TestWorkspaceSync_failedListingKeepsRows: a ref listing that fails stops the sync, and no row goes stale.
func TestWorkspaceSync_failedListingKeepsRows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir := t.TempDir()
	repoURL := gitmsg.ResolveRepoURL(dir)
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: "abcabcabcabc", RepoURL: repoURL, Branch: "main",
		AuthorName: "T", AuthorEmail: "t@t.com", Message: "cached before", Timestamp: time.Now(),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	if _, err := SyncWorkspaceLocal(dir, nil); err == nil {
		t.Error("SyncWorkspaceLocal() in a directory that is not a repository gave no error")
	}
	if isStale(t, repoURL, "abcabcabcabc") {
		t.Error("a failed ref listing marked a row stale")
	}
}

// TestWorkspaceSync_rewindTakesTheFullPath: a stable branch that only grew takes the fast path, and one that lost a commit takes the full path, which marks the commit stale.
func TestWorkspaceSync_rewindTakesTheFullPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir, repoURL := syncTestRepo(t)
	dropped := datedCommit(t, dir, "dropped later", time.Now())
	if ctx, err := resolveWorkspaceSyncContext(dir, nil); err != nil || ctx == nil || ctx.since == nil {
		t.Fatalf("a branch that only grew did not take the fast path: %v", err)
	}
	syncLocal(t, dir)
	mustGit(t, dir, "reset", "-q", "--hard", "HEAD~1")
	if ctx, err := resolveWorkspaceSyncContext(dir, nil); err != nil || ctx == nil || ctx.since != nil {
		t.Fatalf("a rewound branch did not take the full path: %v", err)
	}
	syncLocal(t, dir)
	if !isStale(t, repoURL, dropped) {
		t.Errorf("commit %s left the default branch and is not stale", dropped)
	}
}

// TestWorkspaceSync_refChangeDuringSyncSkipsFinalize: when a ref moves during a sync, a row inserted in that time is not marked stale and the tip does not advance.
func TestWorkspaceSync_refChangeDuringSyncSkipsFinalize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir, repoURL := syncTestRepo(t)
	datedCommit(t, dir, "opens the gate", time.Now())
	tipKey := "workspace:" + repoURL
	before, _ := cache.GetSyncTip(tipKey)
	procs := []WorkspaceSyncFunc{func(_ []git.Commit, _, _, _ string) {
		mustGit(t, dir, "branch", "-q", "made-during-the-sync")
		if err := cache.InsertCommits([]cache.Commit{{
			Hash: "feedfeedfeed", RepoURL: repoURL, Branch: "gitmsg/pm",
			AuthorName: "T", AuthorEmail: "t@t.com", Message: "an item created during the sync", Timestamp: time.Now(),
		}}); err != nil {
			t.Errorf("InsertCommits() error = %v", err)
		}
	}}
	if _, err := SyncWorkspaceLocal(dir, procs); err != nil {
		t.Fatalf("SyncWorkspaceLocal() error = %v", err)
	}
	if isStale(t, repoURL, "feedfeedfeed") {
		t.Error("a row inserted during the sync is stale")
	}
	if after, _ := cache.GetSyncTip(tipKey); after != before {
		t.Errorf("tip advanced over a ref change: %q -> %q", before, after)
	}
}

// defaultOf returns the default branch that the home sync reads from the refs of a workspace.
func defaultOf(t *testing.T, dir string) string {
	t.Helper()
	gate, err := listHomeRefs(dir)
	if err != nil {
		t.Fatalf("listHomeRefs() error = %v", err)
	}
	stable, _ := parseHomeRefs(gate)
	if len(stable) == 0 {
		return ""
	}
	return stable[0].name
}

// TestDefaultBranch_ignoresCheckout: the default branch is from the refs, and the checked-out branch does not change it.
func TestDefaultBranch_ignoresCheckout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	dir := initWorkspace(t)
	mustGit(t, dir, "checkout", "-q", "-b", "zzz")
	if got := defaultOf(t, dir); got != "main" {
		t.Errorf("default branch on a feature checkout = %q, want main", got)
	}
	bare := t.TempDir()
	if err := git.Init(bare, "beta"); err != nil {
		t.Fatalf("git.Init() error = %v", err)
	}
	mustGit(t, bare, "config", "user.email", "t@t.com")
	mustGit(t, bare, "config", "user.name", "T")
	datedCommit(t, bare, "base", time.Now())
	mustGit(t, bare, "branch", "-q", "alpha")
	for _, checkout := range []string{"beta", "alpha"} {
		mustGit(t, bare, "checkout", "-q", checkout)
		if got := defaultOf(t, bare); got != "alpha" {
			t.Errorf("default branch on checkout %s = %q, want alpha, the first by name", checkout, got)
		}
	}
}

// TestSyncWorkspaceOrigin_runsTheWorkspaceSyncs: the origin sync ingests through the workspace sync functions of its options, with the home of each commit; with none it leaves the gate open.
func TestSyncWorkspaceOrigin_runsTheWorkspaceSyncs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	testutil.OpenTempCache(t, sharedCacheDir)
	dir := initWorkspace(t)
	second := datedCommit(t, dir, "second", time.Now())
	pushToBare(t, dir, false)
	if _, stats := SyncWorkspaceOrigin(dir, nil, nil); stats.Items != 0 {
		t.Errorf("items with no workspace sync = %d, want 0", stats.Items)
	}
	seen := make(map[string]string)
	syncs := []WorkspaceSyncFunc{func(commits []git.Commit, _, _, _ string) {
		for _, c := range commits {
			seen[c.Hash] = CleanRefname(c.Refname)
		}
	}}
	_, stats := SyncWorkspaceOrigin(dir, &Options{WorkspaceSyncs: syncs}, nil)
	if stats.Items != 2 {
		t.Errorf("items = %d, want 2", stats.Items)
	}
	if seen[second] != "main" {
		t.Errorf("sync branch for %s = %q, want main", second, seen[second])
	}
}
