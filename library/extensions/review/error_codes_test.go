// error_codes_test.go - Error codes the PR merge, stack, version and suggestion paths return at the Result boundary
package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
)

// initConflictingPRRepo builds a repo whose `conflict-head` branch and `main` change the same line.
func initConflictingPRRepo(t *testing.T) string {
	t.Helper()
	dir := initTestRepo(t)
	commitFile(t, dir, "shared.txt", "base\n", "shared base")
	publish(t, dir, "main")
	git.ExecGit(dir, []string{"checkout", "-b", "conflict-head"})
	commitFile(t, dir, "shared.txt", "head side\n", "head rewrites the line")
	publish(t, dir, "conflict-head")
	git.ExecGit(dir, []string{"checkout", "main"})
	commitFile(t, dir, "shared.txt", "trunk side\n", "trunk rewrites the line")
	publish(t, dir, "main")
	return dir
}

// TestMergePR_draft asserts DRAFT_PR when the pull request is still a draft.
func TestMergePR_draft(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	created := CreatePR(dir, "Draft work", "", CreatePROptions{Base: "main", Head: "feature", Draft: true})
	if !created.Success {
		t.Fatalf("CreatePR() failed: %s", created.Error.Message)
	}
	res := MergePR(dir, created.Data.ID, MergeStrategyFF)
	if res.Success || res.Error.Code != "DRAFT_PR" {
		t.Errorf("MergePR() on a draft = %+v, want DRAFT_PR", res)
	}
	if pr := GetPR(created.Data.ID); pr.Success && pr.Data.State == PRStateMerged {
		t.Error("draft PR was flipped to merged")
	}
}

// TestMergePR_unmergedDependency asserts UNMET_DEPENDENCY when a depends-on target is still open.
func TestMergePR_unmergedDependency(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	dependency := CreatePR(dir, "Lower PR", "", CreatePROptions{Base: "main", Head: "feature"})
	if !dependency.Success {
		t.Fatalf("CreatePR(dependency) failed: %s", dependency.Error.Message)
	}
	dependent := CreatePR(dir, "Upper PR", "", CreatePROptions{
		Base: "main", Head: "feature", DependsOn: []string{dependency.Data.ID},
	})
	if !dependent.Success {
		t.Fatalf("CreatePR(dependent) failed: %s", dependent.Error.Message)
	}
	res := MergePR(dir, dependent.Data.ID, MergeStrategyFF)
	if res.Success || res.Error.Code != "UNMET_DEPENDENCY" {
		t.Errorf("MergePR() above an open dependency = %+v, want UNMET_DEPENDENCY", res)
	}
}

// TestMergePR_incompleteMergeRecord asserts MERGE_INCOMPLETE where merge-base or merge-head cannot be recorded.
func TestMergePR_incompleteMergeRecord(t *testing.T) {
	setupTestDB(t)

	t.Run("base is not a branch ref", func(t *testing.T) {
		dir := initTestRepo(t)
		created := CreatePR(dir, "No base", "", CreatePROptions{Head: "feature"})
		if !created.Success {
			t.Fatalf("CreatePR() failed: %s", created.Error.Message)
		}
		res := MergePR(dir, created.Data.ID, MergeStrategyFF)
		if res.Success || res.Error.Code != "MERGE_INCOMPLETE" {
			t.Errorf("MergePR() without a base branch = %+v, want MERGE_INCOMPLETE", res)
		}
	})

	t.Run("state=merged edit carries no merge-base", func(t *testing.T) {
		dir := initTestRepo(t)
		created := CreatePR(dir, "Direct edit", "", CreatePROptions{Base: "main", Head: "feature"})
		if !created.Success {
			t.Fatalf("CreatePR() failed: %s", created.Error.Message)
		}
		merged := PRStateMerged
		res := UpdatePR(dir, created.Data.ID, UpdatePROptions{State: &merged})
		if res.Success || res.Error.Code != "MERGE_INCOMPLETE" {
			t.Errorf("UpdatePR() to merged without merge-base = %+v, want MERGE_INCOMPLETE", res)
		}
	})
}

// TestMergePR_missingBaseBranch asserts BASE_NOT_FOUND when the base branch is gone locally.
func TestMergePR_missingBaseBranch(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	created := CreatePR(dir, "Onto a ghost", "", CreatePROptions{Base: "no-such-base", Head: "feature"})
	if !created.Success {
		t.Fatalf("CreatePR() failed: %s", created.Error.Message)
	}
	res := MergePR(dir, created.Data.ID, MergeStrategyFF)
	if res.Success || res.Error.Code != "BASE_NOT_FOUND" {
		t.Errorf("MergePR() onto a missing base = %+v, want BASE_NOT_FOUND", res)
	}
}

// TestMergePR_conflictingBranches asserts MERGE_FAILED when the merge itself conflicts.
func TestMergePR_conflictingBranches(t *testing.T) {
	setupTestDB(t)
	dir := initConflictingPRRepo(t)

	created := CreatePR(dir, "Conflicting work", "", CreatePROptions{Base: "main", Head: "conflict-head"})
	if !created.Success {
		t.Fatalf("CreatePR() failed: %s", created.Error.Message)
	}
	mainTip, err := git.ReadRef(dir, "main")
	if err != nil {
		t.Fatalf("read main: %v", err)
	}
	res := MergePR(dir, created.Data.ID, MergeStrategyFF)
	if res.Success || res.Error.Code != "MERGE_FAILED" {
		t.Errorf("MergePR() over a conflict = %+v, want MERGE_FAILED", res)
	}
	if got, _ := git.ReadRef(dir, "main"); got != mainTip {
		t.Errorf("main = %s after the failed merge, want it untouched at %s", got, mainTip)
	}
}

// TestMarkReady_notDraft asserts NOT_DRAFT when the pull request is already ready.
func TestMarkReady_notDraft(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	created := CreatePR(dir, "Ready already", "", CreatePROptions{Base: "main", Head: "feature"})
	if !created.Success {
		t.Fatalf("CreatePR() failed: %s", created.Error.Message)
	}
	res := MarkReady(dir, created.Data.ID)
	if res.Success || res.Error.Code != "NOT_DRAFT" {
		t.Errorf("MarkReady() on a ready PR = %+v, want NOT_DRAFT", res)
	}
}

// TestConvertToDraft_alreadyDraft asserts ALREADY_DRAFT when the pull request is a draft.
func TestConvertToDraft_alreadyDraft(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	created := CreatePR(dir, "Draft already", "", CreatePROptions{Base: "main", Head: "feature", Draft: true})
	if !created.Success {
		t.Fatalf("CreatePR() failed: %s", created.Error.Message)
	}
	res := ConvertToDraft(dir, created.Data.ID)
	if res.Success || res.Error.Code != "ALREADY_DRAFT" {
		t.Errorf("ConvertToDraft() on a draft = %+v, want ALREADY_DRAFT", res)
	}
}

// TestUpdatePRTips_deletedBase asserts BASE_UNRESOLVED when the base branch no longer resolves.
func TestUpdatePRTips_deletedBase(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	if _, err := git.CreateCommitOnBranch(dir, "doomed-base", "doomed base branch"); err != nil {
		t.Fatalf("CreateCommitOnBranch: %v", err)
	}
	created := CreatePR(dir, "Onto a doomed base", "", CreatePROptions{Base: "doomed-base", Head: "feature"})
	if !created.Success {
		t.Fatalf("CreatePR() failed: %s", created.Error.Message)
	}
	if _, err := git.ExecGit(dir, []string{"update-ref", "-d", "refs/heads/doomed-base"}); err != nil {
		t.Fatalf("update-ref -d: %v", err)
	}
	res := UpdatePRTips(dir, created.Data.ID)
	if res.Success || res.Error.Code != "BASE_UNRESOLVED" {
		t.Errorf("UpdatePRTips() with a deleted base = %+v, want BASE_UNRESOLVED", res)
	}
}

// TestSyncPRBranch_unsetRefs asserts INVALID_REFS when the pull request has no base branch.
func TestSyncPRBranch_unsetRefs(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	created := CreatePR(dir, "No base to sync from", "", CreatePROptions{Head: "feature"})
	if !created.Success {
		t.Fatalf("CreatePR() failed: %s", created.Error.Message)
	}
	res := SyncPRBranch(dir, created.Data.ID, "rebase")
	if res.Success || res.Error.Code != "INVALID_REFS" {
		t.Errorf("SyncPRBranch() without a base = %+v, want INVALID_REFS", res)
	}
}

// TestSyncPRBranch_conflictingBranches asserts SYNC_FAILED when the sync itself conflicts.
func TestSyncPRBranch_conflictingBranches(t *testing.T) {
	setupTestDB(t)
	dir := initConflictingPRRepo(t)

	created := CreatePR(dir, "Conflicting work", "", CreatePROptions{Base: "main", Head: "conflict-head"})
	if !created.Success {
		t.Fatalf("CreatePR() failed: %s", created.Error.Message)
	}
	headTip, err := git.ReadRef(dir, "conflict-head")
	if err != nil {
		t.Fatalf("read conflict-head: %v", err)
	}
	res := SyncPRBranch(dir, created.Data.ID, "rebase")
	if res.Success || res.Error.Code != "SYNC_FAILED" {
		t.Errorf("SyncPRBranch() over a conflict = %+v, want SYNC_FAILED", res)
	}
	if got, _ := git.ReadRef(dir, "conflict-head"); got != headTip {
		t.Errorf("conflict-head = %s after the failed sync, want it untouched at %s", got, headTip)
	}

	merged := SyncPRBranch(dir, created.Data.ID, "merge")
	if merged.Success || merged.Error.Code != "SYNC_FAILED" {
		t.Errorf("SyncPRBranch(merge) over a conflict = %+v, want SYNC_FAILED", merged)
	}
}

// TestMergePR_foreignBase asserts INVALID_TARGET when the pull request targets another repository.
func TestMergePR_foreignBase(t *testing.T) {
	setupTestDB(t)

	upstream := cloneAs(t, initBareOrigin(t), "alice", "alice@test.com")
	fork := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")
	upstreamURL := gitmsg.ResolveRepoURL(upstream)

	if _, err := git.ExecGit(fork, []string{"checkout", "-b", "feature"}); err != nil {
		t.Fatalf("checkout feature: %v", err)
	}
	if _, err := git.CreateCommit(fork, git.CommitOptions{Message: "fork work", AllowEmpty: true}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := git.ExecGit(fork, []string{"push", "origin", "feature"}); err != nil {
		t.Fatalf("push feature: %v", err)
	}
	created := CreatePR(fork, "Upstream work", "", CreatePROptions{Base: upstreamURL + "#branch:main", Head: "feature"})
	if !created.Success {
		t.Fatalf("CreatePR() failed: %s", created.Error.Message)
	}

	res := MergePR(fork, created.Data.ID, MergeStrategyFF)
	if res.Success || res.Error.Code != "INVALID_TARGET" {
		t.Errorf("MergePR() from the fork = %+v, want INVALID_TARGET", res)
	}
	closed := ClosePR(fork, created.Data.ID)
	if closed.Success || closed.Error.Code != "INVALID_TARGET" {
		t.Errorf("ClosePR() from the fork = %+v, want INVALID_TARGET", closed)
	}
	if pr := GetPR(created.Data.ID); pr.Success && pr.Data.State != PRStateOpen {
		t.Errorf("PR state = %q after the refused merge and close, want open", pr.Data.State)
	}
}

// TestGetStack_standalonePR asserts NOT_A_STACK when the pull request has no stack neighbors.
func TestGetStack_standalonePR(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	created := CreatePR(dir, "On its own", "", CreatePROptions{Base: "main", Head: "feature"})
	if !created.Success {
		t.Fatalf("CreatePR() failed: %s", created.Error.Message)
	}
	res := GetStack(created.Data.ID)
	if res.Success || res.Error.Code != "NOT_A_STACK" {
		t.Errorf("GetStack() on a standalone PR = %+v, want NOT_A_STACK", res)
	}
}

// TestRebaseStack_conflictingDependent asserts REBASE_FAILED when a dependent cannot be rebased.
func TestRebaseStack_conflictingDependent(t *testing.T) {
	setupTestDB(t)
	dir := initConflictingPRRepo(t)

	lower := CreatePR(dir, "Lower PR", "", CreatePROptions{Base: "main", Head: "feature"})
	if !lower.Success {
		t.Fatalf("CreatePR(lower) failed: %s", lower.Error.Message)
	}
	upper := CreatePR(dir, "Upper PR", "", CreatePROptions{
		Base: "main", Head: "conflict-head", DependsOn: []string{lower.Data.ID},
	})
	if !upper.Success {
		t.Fatalf("CreatePR(upper) failed: %s", upper.Error.Message)
	}
	headTip, err := git.ReadRef(dir, "conflict-head")
	if err != nil {
		t.Fatalf("read conflict-head: %v", err)
	}

	res := RebaseStack(dir, lower.Data.ID)
	if res.Success || res.Error.Code != "REBASE_FAILED" {
		t.Errorf("RebaseStack() over a conflicting dependent = %+v, want REBASE_FAILED", res)
	}
	if got, _ := git.ReadRef(dir, "conflict-head"); got != headTip {
		t.Errorf("conflict-head = %s after the failed stack rebase, want it untouched at %s", got, headTip)
	}
}

// TestGetPRVersions_cacheClosed asserts RESOLVE_FAILED when the version lookup has no cache to read.
func TestGetPRVersions_cacheClosed(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	created := CreatePR(dir, "Versions without a cache", "", CreatePROptions{Base: "main", Head: "feature"})
	if !created.Success {
		t.Fatalf("CreatePR() failed: %s", created.Error.Message)
	}
	cache.Reset()

	res := GetPRVersions(created.Data.ID, reviewTestRepoURL)
	if res.Success || res.Error.Code != "RESOLVE_FAILED" {
		t.Errorf("GetPRVersions() over a closed cache = %+v, want RESOLVE_FAILED", res)
	}
}

// TestComparePRVersions_missingTips asserts MISSING_TIPS when a version records no base-tip.
func TestComparePRVersions_missingTips(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	created := CreatePR(dir, "Onto a ghost base", "", CreatePROptions{Base: "no-such-base", Head: "feature"})
	if !created.Success {
		t.Fatalf("CreatePR() failed: %s", created.Error.Message)
	}
	res := ComparePRVersions(dir, t.TempDir(), created.Data.ID, 0, 0)
	if res.Success || res.Error.Code != "MISSING_TIPS" {
		t.Errorf("ComparePRVersions() without a base-tip = %+v, want MISSING_TIPS", res)
	}
}

// TestComparePRVersions_tipsOutsideWorkdir asserts TIPS_UNAVAILABLE when the version tips are not local.
func TestComparePRVersions_tipsOutsideWorkdir(t *testing.T) {
	setupTestDB(t)
	dir, _, _ := initDivergedPRRepo(t)

	created := CreatePR(dir, "Add feature", "", CreatePROptions{Base: "main", Head: "feature"})
	if !created.Success {
		t.Fatalf("CreatePR() failed: %s", created.Error.Message)
	}
	git.ExecGit(dir, []string{"checkout", "feature"})
	commitFile(t, dir, "feature.txt", "one\ntwo\n", "feature two")
	publish(t, dir, "feature")
	git.ExecGit(dir, []string{"checkout", "main"})
	if updated := UpdatePRTips(dir, created.Data.ID); !updated.Success {
		t.Fatalf("UpdatePRTips() failed: %s", updated.Error.Message)
	}
	versions := GetPRVersions(created.Data.ID, reviewTestRepoURL)
	if !versions.Success || len(versions.Data) < 2 {
		t.Fatalf("GetPRVersions() = %+v, want at least two versions", versions)
	}

	elsewhere := initTestRepo(t)
	res := ComparePRVersions(elsewhere, t.TempDir(), created.Data.ID, 0, len(versions.Data)-1)
	if res.Success || res.Error.Code != "TIPS_UNAVAILABLE" {
		t.Errorf("ComparePRVersions() from a repository without the tips = %+v, want TIPS_UNAVAILABLE", res)
	}
}

// TestApplySuggestion_writeFailed asserts WRITE_ERROR when the target file is read-only.
func TestApplySuggestion_writeFailed(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores the file mode that blocks the write")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o400); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	res := ApplySuggestion(dir, Feedback{
		Suggestion: true,
		File:       "main.go",
		NewLine:    2,
		Content:    "```suggestion\nreplacement\n```",
	})
	if res.Success || res.Error.Code != "WRITE_ERROR" {
		t.Errorf("ApplySuggestion() over a read-only file = %+v, want WRITE_ERROR", res)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "line2") {
		t.Errorf("the file changed after the refused write: %q err=%v", data, err)
	}
}
