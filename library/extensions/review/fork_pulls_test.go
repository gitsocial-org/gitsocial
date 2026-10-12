// fork_pulls_test.go - The selection of open fork pull requests and the fetch of their heads
package review

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/storage"
)

// createForkPR opens a pull request on the fork with the given base and returns its ref.
func createForkPR(t *testing.T, fork, subject, base string) string {
	t.Helper()
	created := CreatePR(fork, subject, "", CreatePROptions{Base: base, Head: "feature"})
	if !created.Success {
		t.Fatalf("CreatePR %s: %s", subject, created.Error.Message)
	}
	return created.Data.ID
}

// TestForkPulls_selection lists the open pull requests of registered forks whose base is this repository and no other.
func TestForkPulls_selection(t *testing.T) {
	setupTestDB(t)
	alice, bob, upstreamURL, forkURL := forkPRFixture(t)
	if err := gitmsg.AddFork(alice, forkURL); err != nil {
		t.Fatalf("AddFork: %v", err)
	}
	open := createForkPR(t, bob, "Open one", "#branch:main")
	nextSecond()
	if res := UpdatePR(bob, open, UpdatePROptions{Body: ptr("more detail")}); !res.Success {
		t.Fatalf("UpdatePR: %s", res.Error.Message)
	}
	closed := createForkPR(t, bob, "Closed one", "#branch:main")
	if res := ClosePR(bob, closed); !res.Success {
		t.Fatalf("ClosePR by the author: %s", res.Error.Message)
	}
	adopted := createForkPR(t, bob, "Adopted one", "#branch:main")
	if res := ClosePR(alice, adopted); !res.Success {
		t.Fatalf("ClosePR by the base owner: %s", res.Error.Message)
	}
	createForkPR(t, bob, "Other base", protocol.NormalizeURL(initBareOrigin(t))+"#branch:main")
	merged := createForkPR(t, bob, "Merged one", "#branch:main")
	if res := MergePR(alice, merged, MergeStrategyFF); !res.Success {
		t.Fatalf("MergePR: %s", res.Error.Message)
	}

	pulls, err := OpenForkPulls(alice, t.TempDir())
	if err != nil {
		t.Fatalf("OpenForkPulls: %v", err)
	}
	if len(pulls) != 1 {
		t.Fatalf("got %d fork pull requests, want the open one alone: %+v", len(pulls), pulls)
	}
	got := pulls[0]
	openHash := protocol.ParseRef(open).Value
	if got.Repo != forkURL || got.Head != "" || len(got.Commits) != 2 || got.Commits[0] != openHash {
		t.Errorf("fork pull = %+v, want repo %s, no head, the canonical %s then its edit", got, forkURL, openHash)
	}
	if got.Dir == "" || upstreamURL == "" {
		t.Errorf("fork pull names no fork repository: %+v", got)
	}
	if heads := OpenForkHeads(alice); len(heads) != 1 || len(heads[forkURL]) != 1 || heads[forkURL][0] != "feature" {
		t.Errorf("OpenForkHeads = %v, want the feature branch of %s", heads, forkURL)
	}
}

// TestFetchForks_bringsOpenHeads fetches the head of an open fork pull request with the fork, and alone when the fork was fetched without it.
func TestFetchForks_bringsOpenHeads(t *testing.T) {
	setupTestDB(t)
	alice, bob, upstreamURL, forkURL := forkPRFixture(t)
	if err := gitmsg.AddFork(alice, forkURL); err != nil {
		t.Fatalf("AddFork: %v", err)
	}
	createForkPR(t, bob, "Fix the bug", "#branch:main")
	want, err := git.ExecGit(bob, []string{"rev-parse", "refs/heads/feature"})
	if err != nil {
		t.Fatalf("read the fork feature tip: %v", err)
	}

	cacheDir := t.TempDir()
	if stats := fetch.FetchForks(alice, cacheDir, Processors(), OpenForkHeads(alice)); len(stats.Errors) != 0 {
		t.Fatalf("FetchForks errors: %+v", stats.Errors)
	}
	forkDir, err := storage.EnsureForkRepository(cacheDir, upstreamURL)
	if err != nil {
		t.Fatalf("EnsureForkRepository: %v", err)
	}
	if got := forkHeadSHA(forkDir, forkURL, "feature"); got != want.Stdout {
		t.Errorf("head after the fork fetch = %q, want %s", got, want.Stdout)
	}
	pulls, err := OpenForkPulls(alice, cacheDir)
	if err != nil || len(pulls) != 1 || pulls[0].Head != want.Stdout || pulls[0].Dir != forkDir {
		t.Errorf("OpenForkPulls = %+v, %v; want one pull at head %s in %s", pulls, err, want.Stdout, forkDir)
	}

	// A fork fetched before its pull request was known gets the head from the follow-up fetch.
	later := t.TempDir()
	if stats := fetch.FetchForks(alice, later, Processors(), nil); len(stats.Errors) != 0 {
		t.Fatalf("FetchForks without heads: %+v", stats.Errors)
	}
	laterDir, _ := storage.EnsureForkRepository(later, upstreamURL)
	if got := forkHeadSHA(laterDir, forkURL, "feature"); got != "" {
		t.Fatalf("head fetched without being asked: %s", got)
	}
	FetchMissingForkHeads(alice, later)
	if got := forkHeadSHA(laterDir, forkURL, "feature"); got != want.Stdout {
		t.Errorf("head after FetchMissingForkHeads = %q, want %s", got, want.Stdout)
	}
}

// ptr returns a pointer to v, for the optional fields of UpdatePROptions.
func ptr[T any](v T) *T { return &v }
