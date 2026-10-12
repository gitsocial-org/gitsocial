// workdir_pr_test.go - Tests for same-repo PR collaboration: symmetric tip
// resolution, branch-existence validation, and idempotent UpdatePRTips.
package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
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

	got, err := resolveBranchTip(dir, repoURL, "feature", nil)
	if err != nil {
		t.Fatalf("resolveBranchTip: %v", err)
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
	// A local-only branch with no tracking ref, which strict
	// remote resolution must error so observation paths can detect deletion
	// without local-fallback masking.
	if _, err := git.CreateCommitOnBranch(dir, "local-only-branch", "local-only"); err != nil {
		t.Fatalf("CreateCommitOnBranch: %v", err)
	}
	if _, err := resolveBranchTip(dir, repoURL, "local-only-branch", nil); err == nil {
		t.Fatal("resolveBranchTip should error for branch not present on remote")
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
	// against the same branch state must be a no-op: the same canonical, no
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
	// Every merge strategy runs over plumbing, so a dirty working tree is untouched.
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

// fullRef reads the full hash a ref names, or fails the test.
func fullRef(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := git.ExecGit(dir, []string{"rev-parse", "--verify", ref})
	if err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(out.Stdout)
}

// TestMergePR_writesMergedHeadRef keeps the full merge head at the merged ref for each strategy, after the head branches are gone and gc has run.
func TestMergePR_writesMergedHeadRef(t *testing.T) {
	t.Parallel()
	for _, strategy := range []MergeStrategy{MergeStrategyFF, MergeStrategySquash, MergeStrategyRebase, MergeStrategyMerge} {
		t.Run(string(strategy), func(t *testing.T) {
			dir := initTestRepo(t)
			head := "head-" + string(strategy)
			if _, err := git.ExecGit(dir, []string{"branch", head, "main"}); err != nil {
				t.Fatalf("branch %s: %v", head, err)
			}
			if _, err := git.CreateCommitOnBranch(dir, head, "work on "+head); err != nil {
				t.Fatalf("commit on %s: %v", head, err)
			}
			headFull := fullRef(t, dir, "refs/heads/"+head)
			created := CreatePR(dir, "Subj", "", CreatePROptions{Base: "main", Head: head})
			if !created.Success {
				t.Fatalf("CreatePR: %s", created.Error.Message)
			}
			merged := MergePR(dir, created.Data.ID, strategy)
			if !merged.Success {
				t.Fatalf("MergePR(%s): %s", strategy, merged.Error.Message)
			}
			if _, err := git.ExecGit(dir, []string{"rev-parse", "--verify", "--quiet", "refs/heads/_gitmsg-merge-tmp"}); err == nil {
				t.Error("the temporary head branch remains after the merge")
			}
			ref := MergedHeadRefPrefix + extractRefHash(merged.Data.ID)
			if got := fullRef(t, dir, ref); got != headFull {
				t.Errorf("%s = %s, want the full merge head %s", ref, got, headFull)
			}
			if err := git.DeleteRef(dir, "refs/heads/"+head); err != nil {
				t.Fatalf("delete %s: %v", head, err)
			}
			for _, args := range [][]string{{"reflog", "expire", "--expire-unreachable=now", "--all"}, {"gc", "--prune=now", "--quiet"}} {
				if _, err := git.ExecGit(dir, args); err != nil {
					t.Fatalf("git %v: %v", args, err)
				}
			}
			if _, err := git.ExecGit(dir, []string{"cat-file", "-e", headFull}); err != nil {
				t.Errorf("gc pruned the merge head %s: %v", headFull, err)
			}
		})
	}
}

// TestMergePR_mergedHeadRef_crossRepo writes the merged ref for a head that another repository serves by URL.
func TestMergePR_mergedHeadRef_crossRepo(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	if _, err := git.ExecGit(dir, []string{"branch", "remote-head", "main"}); err != nil {
		t.Fatalf("branch: %v", err)
	}
	if _, err := git.CreateCommitOnBranch(dir, "remote-head", "remote work"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	headFull := fullRef(t, dir, "refs/heads/remote-head")
	bareDir := t.TempDir()
	if _, err := git.ExecGit(bareDir, []string{"init", "--bare"}); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	if _, err := git.ExecGit(dir, []string{"push", bareDir, "remote-head"}); err != nil {
		t.Fatalf("push: %v", err)
	}
	if err := git.DeleteRef(dir, "refs/heads/remote-head"); err != nil {
		t.Fatalf("delete the local branch: %v", err)
	}
	created := CreatePR(dir, "Remote head", "", CreatePROptions{Base: "#branch:main", Head: bareDir + "#branch:remote-head"})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}
	merged := MergePR(dir, created.Data.ID, MergeStrategySquash)
	if !merged.Success {
		t.Fatalf("MergePR: %s", merged.Error.Message)
	}
	ref := MergedHeadRefPrefix + extractRefHash(merged.Data.ID)
	if got := fullRef(t, dir, ref); got != headFull {
		t.Errorf("%s = %s, want the full merge head %s", ref, got, headFull)
	}
}
