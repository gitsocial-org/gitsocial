// provider_divergence_test.go - Tests for the diverged gitmsg branch notification provider
package notifications

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// runGit runs one git command in dir and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := git.ExecGit(dir, args); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

// emptyCommit adds one empty commit to the checked out branch.
func emptyCommit(t *testing.T, dir, message string) {
	t.Helper()
	if _, err := git.CreateCommit(dir, git.CommitOptions{Message: message, AllowEmpty: true}); err != nil {
		t.Fatalf("CreateCommit(%q) error = %v", message, err)
	}
}

// divergedRepo returns a workdir whose gitmsg/social branch is one commit ahead
// of and one commit behind the same branch on its bare origin.
func divergedRepo(t *testing.T) string {
	t.Helper()
	workdir := cloneFixture(t)
	origin := t.TempDir()
	runGit(t, origin, "init", "--bare", "-b", "main")
	runGit(t, workdir, "remote", "add", "origin", origin)
	runGit(t, workdir, "checkout", "-b", "gitmsg/social")
	emptyCommit(t, workdir, "social: shared base")
	runGit(t, workdir, "push", "origin", "gitmsg/social")
	emptyCommit(t, workdir, "social: published")
	runGit(t, workdir, "push", "origin", "gitmsg/social")
	runGit(t, workdir, "reset", "--hard", "HEAD~1")
	emptyCommit(t, workdir, "social: rewritten locally")
	return workdir
}

// aheadBehindRepo returns a workdir with one gitmsg branch ahead of its origin and one behind it.
func aheadBehindRepo(t *testing.T) string {
	t.Helper()
	workdir := cloneFixture(t)
	origin := t.TempDir()
	runGit(t, origin, "init", "--bare", "-b", "main")
	runGit(t, workdir, "remote", "add", "origin", origin)
	runGit(t, workdir, "checkout", "-b", "gitmsg/social")
	emptyCommit(t, workdir, "social: shared base")
	runGit(t, workdir, "push", "origin", "gitmsg/social")
	emptyCommit(t, workdir, "social: unpushed")
	runGit(t, workdir, "checkout", "-b", "gitmsg/pm", "gitmsg/social~1")
	emptyCommit(t, workdir, "pm: shared base")
	runGit(t, workdir, "push", "origin", "gitmsg/pm")
	emptyCommit(t, workdir, "pm: published")
	runGit(t, workdir, "push", "origin", "gitmsg/pm")
	runGit(t, workdir, "reset", "--hard", "HEAD~1")
	return workdir
}

// countingExecutor installs a git executor counting the processes it spawns, restored on cleanup.
func countingExecutor(t *testing.T) *atomic.Int64 {
	t.Helper()
	var calls atomic.Int64
	restore := git.SetExecutor(func(ctx context.Context, workdir string, args []string) (*git.ExecResult, error) {
		calls.Add(1)
		return git.DefaultExec(ctx, workdir, args)
	})
	t.Cleanup(restore)
	return &calls
}

// divergenceItems returns the notifications this provider contributed to GetAll.
func divergenceItems(t *testing.T, workdir string) []Notification {
	t.Helper()
	all, err := GetAll(workdir, Filter{})
	if err != nil {
		t.Fatalf("GetAll() error = %v", err)
	}
	var out []Notification
	for _, n := range all {
		if n.Source == "gitmsg-divergence" {
			out = append(out, n)
		}
	}
	return out
}

// TestDivergenceProvider_reportsDivergedBranchThroughGetAll checks a diverged branch surfaces once and clears on fast-forward.
func TestDivergenceProvider_reportsDivergedBranchThroughGetAll(t *testing.T) {
	setupTestDB(t)
	workdir := divergedRepo(t)

	items := divergenceItems(t, workdir)
	if len(items) != 1 {
		t.Fatalf("GetAll() returned %d divergence notifications, want 1", len(items))
	}
	if items[0].Type != "branch-diverged" {
		t.Errorf("Type = %q, want %q", items[0].Type, "branch-diverged")
	}
	if items[0].Branch != "gitmsg/social" {
		t.Errorf("Branch = %q, want %q", items[0].Branch, "gitmsg/social")
	}
	payload, ok := items[0].Item.(DivergenceNotification)
	if !ok {
		t.Fatalf("Item = %T, want DivergenceNotification", items[0].Item)
	}
	if payload.Branch != "gitmsg/social" {
		t.Errorf("payload Branch = %q, want %q", payload.Branch, "gitmsg/social")
	}

	runGit(t, workdir, "reset", "--hard", "origin/gitmsg/social")

	if items := divergenceItems(t, workdir); len(items) != 0 {
		t.Errorf("GetAll() returned %d divergence notifications after fast-forward, want 0", len(items))
	}
}

// TestDivergenceProvider_countsDivergedBranches checks the unread count reports one diverged branch.
func TestDivergenceProvider_countsDivergedBranches(t *testing.T) {
	setupTestDB(t)
	workdir := divergedRepo(t)

	p := &divergenceProvider{}
	count, err := p.GetUnreadCount(workdir)
	if err != nil {
		t.Fatalf("GetUnreadCount() error = %v", err)
	}
	if count != 1 {
		t.Errorf("GetUnreadCount() = %d, want 1", count)
	}
}

// TestDivergenceProvider_aheadOrBehindIsNotDiverged checks a branch only ahead of or only behind its remote reports nothing.
func TestDivergenceProvider_aheadOrBehindIsNotDiverged(t *testing.T) {
	setupTestDB(t)
	workdir := aheadBehindRepo(t)

	p := &divergenceProvider{}
	notifs, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if len(notifs) != 0 {
		t.Errorf("GetNotifications() returned %d notifications, want 0: %+v", len(notifs), notifs)
	}
}

// TestDivergenceProvider_convergedRepositoryOneProcess checks a converged repository costs one git process per count.
func TestDivergenceProvider_convergedRepositoryOneProcess(t *testing.T) {
	setupTestDB(t)
	workdir := divergedRepo(t)
	runGit(t, workdir, "reset", "--hard", "origin/gitmsg/social")
	calls := countingExecutor(t)

	p := &divergenceProvider{}
	if _, err := p.GetUnreadCount(workdir); err != nil {
		t.Fatalf("GetUnreadCount() error = %v", err)
	}
	warm := calls.Load()
	count, err := p.GetUnreadCount(workdir)
	if err != nil {
		t.Fatalf("GetUnreadCount() error = %v", err)
	}
	if count != 0 {
		t.Errorf("GetUnreadCount() = %d, want 0", count)
	}
	if spawned := calls.Load() - warm; spawned != 1 {
		t.Errorf("GetUnreadCount() spawned %d git processes, want 1", spawned)
	}
}
