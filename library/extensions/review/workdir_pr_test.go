// workdir_pr_test.go - Tests for same-repo PR collaboration: symmetric tip
// resolution, branch-existence validation, and idempotent UpdatePRTips.
package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/core/git"
)

func TestResolveBranchTip_PrefersRemoteTracking(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	// Match the workspace's origin URL so findRemoteForURL picks it up.
	repoURL := "https://github.com/test/repo"

	if _, err := git.CreateCommitOnBranch(dir, "feature", "local commit"); err != nil {
		t.Fatalf("CreateCommitOnBranch local: %v", err)
	}
	localTip, err := git.ReadRef(dir, "feature")
	if err != nil {
		t.Fatalf("ReadRef local: %v", err)
	}

	// Plant a different commit on refs/remotes/origin/feature to simulate
	// what `git fetch origin` would have produced after a teammate pushed.
	remoteHash, err := git.CreateCommitTree(dir, "remote-side commit\n", "")
	if err != nil {
		t.Fatalf("CreateCommitTree remote: %v", err)
	}
	if err := git.WriteRef(dir, "refs/remotes/origin/feature", remoteHash); err != nil {
		t.Fatalf("WriteRef remote-tracking: %v", err)
	}

	got, err := ResolveBranchTip(dir, repoURL, "feature")
	if err != nil {
		t.Fatalf("ResolveBranchTip: %v", err)
	}
	if got == localTip {
		t.Errorf("resolved to local tip %q; expected the remote-tracking tip", got)
	}
	if !strings.HasPrefix(remoteHash, got) {
		t.Errorf("resolved tip %q does not match remote-tracking commit %q", got, remoteHash)
	}
}

func TestResolveBranchTip_StrictRemote(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	repoURL := "https://github.com/test/repo"
	// Local-only branch with no refs/remotes/origin/<branch> entry — strict
	// remote resolution must error so observation paths can detect deletion
	// without local-fallback masking.
	if _, err := git.CreateCommitOnBranch(dir, "local-only-branch", "local-only"); err != nil {
		t.Fatalf("CreateCommitOnBranch: %v", err)
	}
	if _, err := ResolveBranchTip(dir, repoURL, "local-only-branch"); err == nil {
		t.Fatal("ResolveBranchTip should error for branch not present on remote")
	}
}

func TestCreatePR_RejectsMissingHead(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	res := CreatePR(dir, "Subj", "", CreatePROptions{
		Base: "main",
		Head: "nope-does-not-exist",
	})
	if res.Success {
		t.Fatal("CreatePR should reject unresolvable head")
	}
	if res.Error.Code != "HEAD_NOT_FOUND" {
		t.Errorf("Error.Code = %q, want HEAD_NOT_FOUND", res.Error.Code)
	}
}

func TestCreatePR_AllowUnpublishedHeadEscapeHatch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	res := CreatePR(dir, "Subj", "", CreatePROptions{
		Base:                 "main",
		Head:                 "nope-does-not-exist",
		AllowUnpublishedHead: true,
	})
	if !res.Success {
		t.Fatalf("CreatePR with AllowUnpublishedHead failed: %s", res.Error.Message)
	}
	if res.Data.HeadTip != "" {
		t.Errorf("HeadTip = %q, want empty", res.Data.HeadTip)
	}
}

func TestUpdatePRTips_NoOpWhenUnchanged(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	created := CreatePR(dir, "Subj", "", CreatePROptions{Base: "main", Head: "feature"})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}
	first := UpdatePRTips(dir, created.Data.ID)
	if !first.Success {
		t.Fatalf("UpdatePRTips: %s", first.Error.Message)
	}
	// First call may emit an edit (canonical had empty tips); second call
	// against the same branch state must be a no-op — same canonical, no
	// new edit on gitmsg/review.
	tipBefore, err := git.ReadRef(dir, "gitmsg/review")
	if err != nil {
		t.Fatalf("ReadRef gitmsg/review before: %v", err)
	}
	second := UpdatePRTips(dir, created.Data.ID)
	if !second.Success {
		t.Fatalf("UpdatePRTips second call: %s", second.Error.Message)
	}
	tipAfter, err := git.ReadRef(dir, "gitmsg/review")
	if err != nil {
		t.Fatalf("ReadRef gitmsg/review after: %v", err)
	}
	if tipBefore != tipAfter {
		t.Errorf("UpdatePRTips emitted an edit when nothing changed: before=%q after=%q", tipBefore, tipAfter)
	}
}

func TestUpdatePRTips_ErrorsOnDeletedHead(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	if _, err := git.CreateCommitOnBranch(dir, "doomed", "doomed branch"); err != nil {
		t.Fatalf("CreateCommitOnBranch: %v", err)
	}
	created := CreatePR(dir, "Subj", "", CreatePROptions{Base: "main", Head: "doomed"})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}
	// Delete the head branch.
	if _, err := git.ExecGit(dir, []string{"update-ref", "-d", "refs/heads/doomed"}); err != nil {
		t.Fatalf("update-ref -d: %v", err)
	}
	res := UpdatePRTips(dir, created.Data.ID)
	if res.Success {
		t.Fatal("UpdatePRTips should error when head is deleted")
	}
	if res.Error.Code != "HEAD_UNRESOLVED" {
		t.Errorf("Error.Code = %q, want HEAD_UNRESOLVED", res.Error.Code)
	}
}

func TestMergePR_AllowsDirtyTree(t *testing.T) {
	t.Parallel()
	// MergePR uses plumbing (merge-tree + commit-tree + update-ref) for
	// every strategy and never invokes `git merge`, so the user's working
	// tree is never touched. Untracked or modified files are preserved
	// across the merge — the dirty-tree guard from the earlier
	// `git merge`-based path is intentionally gone.
	dir := initTestRepo(t)
	if _, err := git.ExecGit(dir, []string{"checkout", "-b", "feature-dirty"}); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if _, err := git.CreateCommit(dir, git.CommitOptions{Message: "feature commit", AllowEmpty: true}); err != nil {
		t.Fatalf("CreateCommit: %v", err)
	}
	if _, err := git.ExecGit(dir, []string{"checkout", "main"}); err != nil {
		t.Fatalf("checkout main: %v", err)
	}
	created := CreatePR(dir, "Subj", "", CreatePROptions{Base: "main", Head: "feature-dirty"})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}
	junkPath := filepath.Join(dir, "junk.txt")
	if err := os.WriteFile(junkPath, []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatalf("write junk: %v", err)
	}
	res := MergePR(dir, created.Data.ID, MergeStrategyFF)
	if !res.Success {
		t.Fatalf("MergePR with dirty tree should succeed: %s", res.Error.Message)
	}
	if data, err := os.ReadFile(junkPath); err != nil || string(data) != "uncommitted\n" {
		t.Errorf("junk.txt was modified or removed by merge: data=%q err=%v", string(data), err)
	}
}

func TestMergePR_RejectsMissingHead(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	if _, err := git.CreateCommitOnBranch(dir, "doomed-merge", "doomed branch"); err != nil {
		t.Fatalf("CreateCommitOnBranch: %v", err)
	}
	created := CreatePR(dir, "Subj", "", CreatePROptions{Base: "main", Head: "doomed-merge"})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}
	if _, err := git.ExecGit(dir, []string{"update-ref", "-d", "refs/heads/doomed-merge"}); err != nil {
		t.Fatalf("update-ref -d: %v", err)
	}
	res := MergePR(dir, created.Data.ID, MergeStrategyFF)
	if res.Success {
		t.Fatal("MergePR should reject deleted head branch")
	}
	if res.Error.Code != "HEAD_NOT_FOUND" {
		t.Errorf("Error.Code = %q, want HEAD_NOT_FOUND", res.Error.Code)
	}
	// And the PR must not have been flipped to merged.
	pr := GetPR(created.Data.ID)
	if !pr.Success {
		t.Fatalf("GetPR after failed merge: %s", pr.Error.Message)
	}
	if pr.Data.State == PRStateMerged {
		t.Error("PR state was flipped to merged despite missing head branch")
	}
}
