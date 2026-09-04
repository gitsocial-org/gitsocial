// clientfetch_test.go - tests for the shared thin-client fetch wiring: which
// extension processors each set carries, which items tables the post-fetch
// backfill scans, and which repos it scans them for.
//
// The processor sets are asserted by running them over a real temp cache and
// looking for the rows they must produce, not by counting functions: the drift
// this package exists to prevent (a fork fetch without social.Processors, so
// fork comments never got a social_items row) is exactly a missing row.
package clientfetch

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
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		n := 0
		err := db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE repo_url = ? AND hash = ?", testRepoURL, hash).Scan(&n)
		return n, err
	})
	if err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

// TestForkProcessorsIncludeSocial pins the split the package documents: social
// commits reached over a fork fetch get their social_items row only because
// ForkProcessors carries social.Processors, while the workspace set leaves them
// to social.Fetch. A fork set without it is the drift that once made fork
// comments invisible to threads.
func TestForkProcessorsIncludeSocial(t *testing.T) {
	testutil.OpenTempCache(t, "")
	fields := map[string]string{"type": "post"}

	viaExtra := dispatch(t, ExtraProcessors(), "aaaa000000000000000000000000000000000001", "social", fields)
	if got := rowCount(t, "social_items", viaExtra); got != 0 {
		t.Errorf("ExtraProcessors wrote %d social_items rows, want 0 (social.Fetch adds social.Processors itself)", got)
	}

	viaFork := dispatch(t, ForkProcessors(), "aaaa000000000000000000000000000000000002", "social", fields)
	if got := rowCount(t, "social_items", viaFork); got != 1 {
		t.Errorf("ForkProcessors wrote %d social_items rows for a fork post, want 1", got)
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
		{"ExtraProcessors", ExtraProcessors()},
		{"ForkProcessors", ForkProcessors()},
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
}

// TestExtraProcessorsRecordMentions checks the notification processors travel
// with both sets: a commit mentioning an address lands in core_mentions, which
// is what the mention notification provider reads.
func TestExtraProcessorsRecordMentions(t *testing.T) {
	testutil.OpenTempCache(t, "")
	commit := git.Commit{Hash: "cccc000000000000000000000000000000000001", Message: "ping @grace@example.com", Author: "Ada", Email: "ada@example.com", Timestamp: time.Now(), Refname: "gitmsg/social"}
	seedCommit(t, commit, "gitmsg/social")
	msg := &protocol.Message{Content: commit.Message, Header: protocol.Header{Ext: "social", V: "1", Fields: map[string]string{"type": "post"}}}
	for _, process := range ExtraProcessors() {
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
