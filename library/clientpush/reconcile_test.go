// reconcile_test.go - the git-side of the push-path tracking-ref reconcile: a
// deleted bucket ref with an intact tracking ref must delete the stale tracking
// ref (so the next count restores the branch), a differing bucket ref updates
// the tracking ref, and gitmsg state refs map into the tracking namespace.
package clientpush

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
)

// headSHA returns the repo's current HEAD commit sha.
func headSHA(t *testing.T, work string) string {
	t.Helper()
	out, err := git.ExecGit(work, []string{"rev-parse", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(out.Stdout)
}

// refValue returns a ref's sha, or "" when it doesn't exist.
func refValue(work, ref string) string {
	out, err := git.ExecGit(work, []string{"rev-parse", "--verify", ref})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out.Stdout)
}

func TestApplyTrackingReconcile_deletesStaleBranchTracking(t *testing.T) {
	work := setupWork(t, t.TempDir())
	sha := headSHA(t, work)
	// A prior push left a tracking ref; the bucket no longer advertises the branch.
	if err := git.WriteRef(work, "refs/remotes/r/main", sha); err != nil {
		t.Fatalf("seed tracking ref: %v", err)
	}
	applyTrackingReconcile(work, "r", map[string]string{})
	if v := refValue(work, "refs/remotes/r/main"); v != "" {
		t.Errorf("stale tracking ref survived (=%s); a deleted bucket ref must delete it so the branch is re-counted", v)
	}
}

func TestApplyTrackingReconcile_updatesAndWritesFromBucket(t *testing.T) {
	work := setupWork(t, t.TempDir())
	old := headSHA(t, work)
	// A stale tracking ref points at the old tip; a second local commit is the tip
	// the bucket now advertises.
	if err := git.WriteRef(work, "refs/remotes/r/main", old); err != nil {
		t.Fatalf("seed tracking ref: %v", err)
	}
	if _, err := git.CreateCommit(work, git.CommitOptions{Message: "second", AllowEmpty: true}); err != nil {
		t.Fatalf("second commit: %v", err)
	}
	newTip := headSHA(t, work)

	bucket := map[string]string{
		"refs/heads/main":         newTip,
		"refs/gitmsg/core/config": newTip, // any local object; maps into the tracking prefix
		"refs/tags/v1":            newTip, // tags are uncountable offline: must be skipped
	}
	applyTrackingReconcile(work, "r", bucket)

	if v := refValue(work, "refs/remotes/r/main"); v != newTip {
		t.Errorf("branch tracking ref = %s, want the bucket tip %s", v, newTip)
	}
	if v := refValue(work, gitmsg.TrackingRef("r", "refs/gitmsg/core/config")); v != newTip {
		t.Errorf("gitmsg tracking ref = %s, want %s", v, newTip)
	}
	if v := refValue(work, "refs/remotes/r/v1"); v != "" {
		t.Errorf("tag must not be mirrored into the tracking namespace, got %s", v)
	}
}
