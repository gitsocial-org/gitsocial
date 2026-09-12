// error_codes_test.go - Error codes the PR merge path returns at the Result boundary
package review

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
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
