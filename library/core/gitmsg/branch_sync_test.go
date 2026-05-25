// branch_sync_test.go - Tests for FetchAndMergeBranch / PushBranchWithMerge.
package gitmsg

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

const testBranch = "gitmsg/test"

// setupBareRepoWithOrigin creates a remote bare repo and a local bare repo
// with `origin` pointing at it. Both have user.name/email set so commit-tree
// doesn't fail in CI.
func setupBareRepoWithOrigin(t *testing.T) (local, remote string) {
	t.Helper()
	remote = t.TempDir()
	if err := git.EnsureBareRepo(remote); err != nil {
		t.Fatalf("init remote: %v", err)
	}
	local = t.TempDir()
	if err := git.EnsureBareRepo(local); err != nil {
		t.Fatalf("init local: %v", err)
	}
	if _, err := git.ExecGit(local, []string{"remote", "add", "origin", remote}); err != nil {
		t.Fatalf("add origin: %v", err)
	}
	for _, p := range []string{local, remote} {
		for _, kv := range [][2]string{
			{"user.name", "Sync Test"},
			{"user.email", "sync-test@example.com"},
		} {
			if _, err := git.ExecGit(p, []string{"config", kv[0], kv[1]}); err != nil {
				t.Fatalf("config %s: %v", kv[0], err)
			}
		}
	}
	return local, remote
}

// addEmptyCommit appends an empty-tree commit on top of testBranch's current
// tip (or creates the branch fresh if it doesn't exist). Returns the new tip.
func addEmptyCommit(t *testing.T, repoPath, subject string) string {
	t.Helper()
	branchRef := "refs/heads/" + testBranch
	parent := fullHash(repoPath, branchRef)
	args := []string{"commit-tree", git.EmptyTreeHash, "-m", subject}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	out, err := git.ExecGit(repoPath, args)
	if err != nil {
		t.Fatalf("commit-tree: %v", err)
	}
	hash := strings.TrimSpace(out.Stdout)
	if err := git.WriteRef(repoPath, branchRef, hash); err != nil {
		t.Fatalf("update-ref: %v", err)
	}
	return hash
}

// TestFetchAndMergeBranch_freshLocal — remote has commits, local has no branch
// yet → adopt the remote tip.
func TestFetchAndMergeBranch_freshLocal(t *testing.T) {
	local, remote := setupBareRepoWithOrigin(t)

	// Seed remote with a commit on testBranch via a separate bare repo.
	seeder := t.TempDir()
	if err := git.EnsureBareRepo(seeder); err != nil {
		t.Fatalf("init seeder: %v", err)
	}
	if _, err := git.ExecGit(seeder, []string{"remote", "add", "origin", remote}); err != nil {
		t.Fatalf("seeder add origin: %v", err)
	}
	for _, kv := range [][2]string{{"user.name", "Seeder"}, {"user.email", "seeder@example.com"}} {
		git.ExecGit(seeder, []string{"config", kv[0], kv[1]})
	}
	remoteTip := addEmptyCommit(t, seeder, "remote commit")
	if _, err := git.ExecGit(seeder, []string{"push", "origin", testBranch}); err != nil {
		t.Fatalf("seeder push: %v", err)
	}

	if err := FetchAndMergeBranch(local, testBranch); err != nil {
		t.Fatalf("FetchAndMergeBranch: %v", err)
	}
	gotTip := fullHash(local, "refs/heads/"+testBranch)
	if gotTip != remoteTip {
		t.Errorf("local tip = %q, want remote tip %q", gotTip, remoteTip)
	}
}

// TestFetchAndMergeBranch_alreadyEqual — no-op when both sides match.
func TestFetchAndMergeBranch_alreadyEqual(t *testing.T) {
	local, _ := setupBareRepoWithOrigin(t)
	tip := addEmptyCommit(t, local, "shared")
	if _, err := git.ExecGit(local, []string{"push", "origin", testBranch}); err != nil {
		t.Fatalf("push: %v", err)
	}
	if err := FetchAndMergeBranch(local, testBranch); err != nil {
		t.Fatalf("FetchAndMergeBranch no-op: %v", err)
	}
	got := fullHash(local, "refs/heads/"+testBranch)
	if got != tip {
		t.Errorf("tip changed unexpectedly: %q → %q", tip, got)
	}
}

// TestPush_autoMergesDivergentExtensionBranch is the higher-level scenario:
// two workspace clones each commit to gitmsg/social between syncs; the second
// `gitmsg.Push` would have failed non-fast-forward pre-fix, dropping the user
// into raw git. With PushBranchWithMerge wired in, it auto-merges and succeeds.
func TestPush_autoMergesDivergentExtensionBranch(t *testing.T) {
	// Shared remote.
	remote := t.TempDir()
	if err := git.EnsureBareRepo(remote); err != nil {
		t.Fatalf("init remote: %v", err)
	}

	// Two workspace clones. Use real working repos (not bare) so Push's
	// GetExtBranches / GetUnpushedCounts work as in production.
	cloneA := t.TempDir()
	cloneB := t.TempDir()
	for _, w := range []string{cloneA, cloneB} {
		git.Init(w, "main")
		git.ExecGit(w, []string{"config", "user.email", "tester@example.com"})
		git.ExecGit(w, []string{"config", "user.name", "Tester"})
		git.CreateCommit(w, git.CommitOptions{Message: "init", AllowEmpty: true})
		if _, err := git.ExecGit(w, []string{"remote", "add", "origin", remote}); err != nil {
			t.Fatalf("add origin to %s: %v", w, err)
		}
		// Push main so the remote has a default branch (required for some git
		// ops). The second clone's push will be rejected non-FF — either side
		// winning is fine; we don't care which.
		_, _ = git.ExecGit(w, []string{"push", "origin", "main"})
	}

	// A commits + pushes the initial gitmsg/social.
	if _, err := git.CreateCommitOnBranch(cloneA, "gitmsg/social", "A: initial post"); err != nil {
		t.Fatalf("A initial commit: %v", err)
	}
	if _, err := Push(cloneA, false); err != nil {
		t.Fatalf("A initial push: %v", err)
	}

	// B fetches and commits its own post on top.
	if err := FetchAndMergeBranch(cloneB, "gitmsg/social"); err != nil {
		t.Fatalf("B initial fetch: %v", err)
	}
	if _, err := git.CreateCommitOnBranch(cloneB, "gitmsg/social", "B: my reply"); err != nil {
		t.Fatalf("B commit: %v", err)
	}
	if _, err := Push(cloneB, false); err != nil {
		t.Fatalf("B push: %v", err)
	}

	// A commits without re-fetching — now divergent with remote.
	if _, err := git.CreateCommitOnBranch(cloneA, "gitmsg/social", "A: second post"); err != nil {
		t.Fatalf("A second commit: %v", err)
	}

	// Pre-fix, this push fails non-fast-forward and the user is stuck.
	// Post-fix, PushBranchWithMerge auto-merges and the push succeeds.
	result, err := Push(cloneA, false)
	if err != nil {
		t.Fatalf("A divergent push: %v", err)
	}
	if result.Commits == 0 {
		t.Errorf("expected non-zero pushed-commits count, got 0")
	}

	// Sanity: remote should now have a merge commit at the tip of gitmsg/social
	// with both sides' commits as ancestors.
	cmd := exec.Command("git", "--git-dir="+remote, "rev-list", "--count", "gitmsg/social")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-list remote: %v: %s", err, out)
	}
	// 3 leaf commits (A1, B1, A2) + 1 merge = 4 reachable.
	if got := strings.TrimSpace(string(out)); got != "4" {
		t.Errorf("remote gitmsg/social rev-list count = %s, want 4 (A1+B1+A2+merge)", got)
	}
}

// TestPushBranchWithMerge_divergent — the headline two-machines scenario:
// machine A and machine B each commit between syncs; A's naive push would
// fail non-fast-forward; PushBranchWithMerge auto-merges and succeeds. Both
// sides' commits must remain reachable from the merged tip.
func TestPushBranchWithMerge_divergent(t *testing.T) {
	remote := t.TempDir()
	if err := git.EnsureBareRepo(remote); err != nil {
		t.Fatalf("init remote: %v", err)
	}
	machineA := t.TempDir()
	machineB := t.TempDir()
	for _, m := range []string{machineA, machineB} {
		if err := git.EnsureBareRepo(m); err != nil {
			t.Fatalf("init bare: %v", err)
		}
		if _, err := git.ExecGit(m, []string{"remote", "add", "origin", remote}); err != nil {
			t.Fatalf("add origin: %v", err)
		}
		for _, kv := range [][2]string{{"user.name", "Tester"}, {"user.email", "t@x"}} {
			git.ExecGit(m, []string{"config", kv[0], kv[1]})
		}
	}

	// A commits + pushes the initial commit.
	tipA1 := addEmptyCommit(t, machineA, "A: initial")
	if err := PushBranchWithMerge(machineA, testBranch); err != nil {
		t.Fatalf("A initial push: %v", err)
	}

	// B fetches, adds its own commit on top.
	if err := FetchAndMergeBranch(machineB, testBranch); err != nil {
		t.Fatalf("B initial fetch: %v", err)
	}
	tipB1 := addEmptyCommit(t, machineB, "B: my note")
	if err := PushBranchWithMerge(machineB, testBranch); err != nil {
		t.Fatalf("B first push: %v", err)
	}

	// A meanwhile commits without re-fetching — now divergent with remote.
	tipA2 := addEmptyCommit(t, machineA, "A: second note")

	// Naive push would fail non-FF. PushBranchWithMerge must auto-merge + retry.
	if err := PushBranchWithMerge(machineA, testBranch); err != nil {
		t.Fatalf("A divergent push: %v", err)
	}

	// Merged tip on A should have two parents (the divergent merge).
	finalTip := fullHash(machineA, "refs/heads/"+testBranch)
	revOut, err := git.ExecGit(machineA, []string{"rev-list", "--parents", "-n", "1", finalTip})
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	fields := strings.Fields(strings.TrimSpace(revOut.Stdout))
	parents := fields[1:]
	if len(parents) != 2 {
		t.Fatalf("merge commit has %d parents, want 2 (rev-list: %q)", len(parents), revOut.Stdout)
	}

	// Both sides' original commits must be reachable from the final tip.
	for _, h := range []string{tipA1, tipB1, tipA2} {
		if _, err := git.ExecGit(machineA, []string{"merge-base", "--is-ancestor", h, finalTip}); err != nil {
			t.Errorf("commit %s not reachable from merged tip", h)
		}
	}
}
