// fetch_test.go - tests for the shared fetch wiring: the processor sets, the workspace syncs, the backfill specs and the repos it scans.
package client

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

const testRepoURL = "https://example.com/fork/repo"

// dispatch runs one synthetic commit of the given extension through a processor
// set and returns the commit hash it was stored under.
func dispatch(t *testing.T, processors []fetch.CommitProcessor, hash, ext string, fields map[string]string) string {
	t.Helper()
	commit := git.Commit{Hash: hash, Message: "test message", Author: "Ada", Email: "ada@example.com", Timestamp: time.Now(), Refname: "gitmsg/" + ext}
	seedCommit(t, commit, "gitmsg/"+ext)
	msg := &protocol.Message{Content: commit.Message, Header: protocol.Header{Ext: ext, V: "1", Fields: fields}}
	for _, process := range processors {
		process(commit, msg, testRepoURL, "gitmsg/"+ext)
	}
	return hash
}

// seedCommit stores one commit in core_commits, the row an extension item
// links to.
func seedCommit(t *testing.T, commit git.Commit, branch string) {
	t.Helper()
	row := cache.Commit{Hash: commit.Hash, RepoURL: testRepoURL, Branch: branch, AuthorName: commit.Author, AuthorEmail: commit.Email, Message: commit.Message, Timestamp: commit.Timestamp, FetchedAt: time.Now()}
	if err := cache.InsertCommits([]cache.Commit{row}); err != nil {
		t.Fatalf("seed commit: %v", err)
	}
}

// rowCount returns how many rows a table holds for one commit hash.
func rowCount(t *testing.T, table, hash string) int {
	t.Helper()
	return rowCountForRepo(t, table, testRepoURL, hash)
}

// rowCountForRepo returns how many rows a table holds for one repository's commit.
func rowCountForRepo(t *testing.T, table, repoURL, hash string) int {
	t.Helper()
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		n := 0
		err := db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE repo_url = ? AND hash = ?", repoURL, hash).Scan(&n)
		return n, err
	})
	if err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

// TestForkProcessorsIncludeSocial pins the split between the two sets: a fork
// commit gets its social_items row from the full set, while the extra set
// leaves social to social.Fetch.
func TestForkProcessorsIncludeSocial(t *testing.T) {
	testutil.OpenTempCache(t, "")
	fields := map[string]string{"type": "post"}

	viaExtra := dispatch(t, extraProcessors(), "aaaa000000000000000000000000000000000001", "social", fields)
	if got := rowCount(t, "social_items", viaExtra); got != 0 {
		t.Errorf("extraProcessors wrote %d social_items rows, want 0 (social.Fetch adds social.Processors itself)", got)
	}

	viaFork := dispatch(t, processors(), "aaaa000000000000000000000000000000000002", "social", fields)
	if got := rowCount(t, "social_items", viaFork); got != 1 {
		t.Errorf("processors wrote %d social_items rows for a fork post, want 1", got)
	}
}

// TestProcessorSetsCoverEveryExtension checks both sets carry the four
// non-social extensions, so a fetch through either path leaves no commit in
// core_commits without its extension row.
func TestProcessorSetsCoverEveryExtension(t *testing.T) {
	testutil.OpenTempCache(t, "")
	cases := []struct {
		ext    string
		table  string
		fields map[string]string
	}{
		{"pm", "pm_items", map[string]string{"type": "issue", "state": "open"}},
		{"review", "review_items", map[string]string{"type": "pr", "state": "open"}},
		{"release", "release_items", map[string]string{"version": "1.0.0", "tag": "v1.0.0"}},
		{"memo", "memo_items", map[string]string{"type": "memo"}},
	}
	sets := []struct {
		name       string
		processors []fetch.CommitProcessor
	}{
		{"extraProcessors", extraProcessors()},
		{"processors", processors()},
	}
	next := 0
	for _, set := range sets {
		for _, c := range cases {
			next++
			hash := dispatch(t, set.processors, fmt.Sprintf("%040x", next), c.ext, c.fields)
			if n := rowCount(t, c.table, hash); n != 1 {
				t.Errorf("%s wrote %d %s rows for a %s commit, want 1", set.name, n, c.table, c.ext)
			}
		}
	}
	if got := len(workspaceSyncs()); got != len(cases)+1 {
		t.Errorf("workspaceSyncs has %d entries, want %d (one per extension)", got, len(cases)+1)
	}
}

// TestExtraProcessorsRecordMentions checks the notification processors travel
// with both sets: a commit mentioning an address lands in core_mentions, which
// is what the mention notification provider reads.
func TestExtraProcessorsRecordMentions(t *testing.T) {
	testutil.OpenTempCache(t, "")
	commit := git.Commit{Hash: "cccc000000000000000000000000000000000001", Message: "ping @grace@example.com", Author: "Ada", Email: "ada@example.com", Timestamp: time.Now(), Refname: "gitmsg/social"}
	seedCommit(t, commit, "gitmsg/social")
	msg := &protocol.Message{Content: commit.Message, Header: protocol.Header{Ext: "social", V: "1", Fields: map[string]string{"type": "post"}}}
	for _, process := range extraProcessors() {
		process(commit, msg, testRepoURL, "gitmsg/social")
	}
	if got := rowCount(t, "core_mentions", commit.Hash); got != 1 {
		t.Errorf("core_mentions rows = %d, want 1 (the mention processor is missing from the set)", got)
	}
}

// TestBackfillSpecsCoverEveryExtension checks the backfill scans one items
// table per extension: a missing spec means an orphaned row is never repaired,
// since dedup skips the commit forever.
func TestBackfillSpecsCoverEveryExtension(t *testing.T) {
	want := map[string]string{
		"social":  "social_items",
		"pm":      "pm_items",
		"release": "release_items",
		"review":  "review_items",
		"memo":    "memo_items",
	}
	specs := backfillSpecs()
	if len(specs) != len(want) {
		t.Fatalf("backfillSpecs returned %d specs, want %d (one per extension)", len(specs), len(want))
	}
	seen := map[string]bool{}
	for _, s := range specs {
		table, ok := want[s.Extension]
		if !ok {
			t.Errorf("unexpected backfill spec for extension %q", s.Extension)
			continue
		}
		if s.ItemsTable != table {
			t.Errorf("backfill spec for %q scans %q, want %q", s.Extension, s.ItemsTable, table)
		}
		if seen[s.Extension] {
			t.Errorf("backfill spec for %q listed twice", s.Extension)
		}
		seen[s.Extension] = true
	}
	if got := len(workspaceSyncs()); got != len(want) {
		t.Errorf("workspaceSyncs has %d entries, want %d (one per extension)", got, len(want))
	}
}

// TestBackfillReposListsWorkspaceThenForks checks the backfill's repo set is
// the workspace plus every registered fork, in that order, so a fork commit
// with an orphaned extension row is in scope.
func TestBackfillReposListsWorkspaceThenForks(t *testing.T) {
	template, err := testutil.NewRepoTemplate()
	if err != nil {
		t.Fatalf("repo template: %v", err)
	}
	workdir := testutil.CopyRepo(t, template)

	if repos := backfillRepos(workdir); len(repos) != 1 || repos[0] != gitmsg.ResolveRepoURL(workdir) {
		t.Fatalf("backfillRepos without forks = %v, want just the workspace URL %q", repos, gitmsg.ResolveRepoURL(workdir))
	}
	for _, fork := range []string{"https://example.com/a/fork", "https://example.com/b/fork"} {
		if err := gitmsg.AddFork(workdir, fork); err != nil {
			t.Fatalf("add fork %s: %v", fork, err)
		}
	}
	repos := backfillRepos(workdir)
	if len(repos) != 3 {
		t.Fatalf("backfillRepos = %v, want the workspace plus 2 forks", repos)
	}
	if repos[0] != gitmsg.ResolveRepoURL(workdir) {
		t.Errorf("backfillRepos[0] = %q, want the workspace URL %q", repos[0], gitmsg.ResolveRepoURL(workdir))
	}
	found := map[string]bool{repos[1]: true, repos[2]: true}
	for _, fork := range []string{"https://example.com/a/fork", "https://example.com/b/fork"} {
		if !found[fork] {
			t.Errorf("backfillRepos = %v, missing registered fork %q", repos, fork)
		}
	}
}

// TestFetch_everyExtensionIngests runs the whole sequence over a workspace
// carrying one item per extension and one registered fork, and looks for the
// row each extension owes: the drift this package exists to close is a fetch
// path that leaves an item table empty.
func TestFetch_everyExtensionIngests(t *testing.T) {
	if testing.Short() {
		t.Skip("real git")
	}
	testutil.OpenTempCache(t, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	template, err := testutil.NewRepoTemplate()
	if err != nil {
		t.Fatalf("repo template: %v", err)
	}
	workdir := testutil.CopyRepo(t, template)
	items := []struct {
		ext    string
		table  string
		header string
	}{
		{"social", "social_items", `GitMsg: ext="social"; type="post"; v="0.1.0"`},
		{"pm", "pm_items", `GitMsg: ext="pm"; type="issue"; state="open"; v="0.1.0"`},
		{"review", "review_items", `GitMsg: ext="review"; type="pr"; state="open"; v="0.1.0"`},
		{"release", "release_items", `GitMsg: ext="release"; tag="v1.0.0"; version="1.0.0"; v="0.1.0"`},
		{"memo", "memo_items", `GitMsg: ext="memo"; type="memo"; v="0.1.0"`},
	}
	hashes := make(map[string]string, len(items))
	for _, item := range items {
		hash, err := git.CreateCommitOnBranch(workdir, "gitmsg/"+item.ext, item.ext+" item\n\n"+item.header)
		if err != nil {
			t.Fatalf("create %s commit: %v", item.ext, err)
		}
		hashes[item.ext] = hash
	}

	forkDir := t.TempDir()
	if _, err := git.ExecGit(forkDir, []string{"init", "--bare"}); err != nil {
		t.Fatalf("init fork: %v", err)
	}
	for _, cfg := range [][]string{{"user.name", "Test"}, {"user.email", "test@example.com"}} {
		if _, err := git.ExecGit(forkDir, []string{"config", cfg[0], cfg[1]}); err != nil {
			t.Fatalf("configure fork: %v", err)
		}
	}
	forkHash, err := git.CreateCommitOnBranch(forkDir, "gitmsg/pm", "fork issue\n\n"+`GitMsg: ext="pm"; type="issue"; state="open"; v="0.1.0"`)
	if err != nil {
		t.Fatalf("create fork commit: %v", err)
	}
	if err := gitmsg.AddFork(workdir, forkDir); err != nil {
		t.Fatalf("add fork: %v", err)
	}

	Fetch(workdir, t.TempDir(), FetchOptions{})

	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	for _, item := range items {
		if n := rowCountForRepo(t, item.table, workspaceURL, hashes[item.ext]); n != 1 {
			t.Errorf("Fetch wrote %d %s rows for the workspace %s item, want 1", n, item.table, item.ext)
		}
	}
	if n := rowCountForRepo(t, "pm_items", forkDir, forkHash); n != 1 {
		t.Errorf("Fetch wrote %d pm_items rows for the fork issue, want 1", n)
	}
}
