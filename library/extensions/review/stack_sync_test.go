// stack_sync_test.go - Tests for branch sync, stack rebase, and version comparison over real branches
package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// commitFile writes a file on the current branch and commits it.
func commitFile(t *testing.T, dir, name, content, message string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if _, err := git.ExecGit(dir, []string{"add", name}); err != nil {
		t.Fatalf("git add %s: %v", name, err)
	}
	if _, err := git.ExecGit(dir, []string{"commit", "-m", message}); err != nil {
		t.Fatalf("git commit %q: %v", message, err)
	}
	tip, err := git.ReadRef(dir, "HEAD")
	if err != nil {
		t.Fatalf("read HEAD: %v", err)
	}
	return tip
}

// publish points the origin tracking ref at the branch's local tip, standing in
// for a push so PR tip resolution sees the branch as published.
func publish(t *testing.T, dir, branch string) string {
	t.Helper()
	tip, err := git.ReadRef(dir, branch)
	if err != nil {
		t.Fatalf("read %s: %v", branch, err)
	}
	if err := git.WriteRef(dir, "refs/remotes/origin/"+branch, tip); err != nil {
		t.Fatalf("write tracking ref for %s: %v", branch, err)
	}
	return tip
}

// initDivergedPRRepo builds a repo whose `feature` branch and `main` have both
// moved on since they parted, and returns the repo plus both tips.
func initDivergedPRRepo(t *testing.T) (dir, mainTip, featureTip string) {
	t.Helper()
	dir = initTestRepo(t)
	git.ExecGit(dir, []string{"checkout", "feature"})
	commitFile(t, dir, "feature.txt", "one\n", "feature one")
	featureTip = publish(t, dir, "feature")

	git.ExecGit(dir, []string{"checkout", "main"})
	commitFile(t, dir, "main.txt", "main\n", "main moves on")
	mainTip = publish(t, dir, "main")
	return dir, mainTip, featureTip
}

func TestSyncPRBranch_rebase(t *testing.T) {
	setupTestDB(t)
	dir, mainTip, featureTip := initDivergedPRRepo(t)

	pr := CreatePR(dir, "Add feature", "", CreatePROptions{Base: "main", Head: "feature"})
	if !pr.Success {
		t.Fatalf("CreatePR() failed: %s", pr.Error.Message)
	}

	synced := SyncPRBranch(dir, pr.Data.ID, "rebase")
	if !synced.Success {
		t.Fatalf("SyncPRBranch() failed: %s", synced.Error.Message)
	}

	newFeatureTip, _ := git.ReadRef(dir, "feature")
	if newFeatureTip == featureTip {
		t.Error("feature was not rewritten by the rebase")
	}
	if got, _ := git.ReadRef(dir, "main"); got != mainTip {
		t.Errorf("main = %s, want it untouched at %s", got, mainTip)
	}
	if behind, err := git.GetBehindCount(dir, "refs/heads/main", "refs/heads/feature"); err != nil || behind != 0 {
		t.Errorf("GetBehindCount() = %d, %v, want 0 after the rebase", behind, err)
	}
	parents, err := git.ExecGit(dir, []string{"rev-list", "--parents", "-n", "1", newFeatureTip})
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	fields := strings.Fields(strings.TrimSpace(parents.Stdout))
	if len(fields) != 2 || !strings.HasPrefix(fields[1], mainTip) {
		t.Errorf("rebased tip parents = %v, want a single parent at the base tip %s", fields[1:], mainTip)
	}
	// The recorded head-tip tracks the published branch, which the local rebase
	// has not been pushed to yet.
	originTip, _ := git.ReadRef(dir, "refs/remotes/origin/feature")
	if synced.Data.HeadTip != originTip {
		t.Errorf("PR head-tip = %q, want the published tip %s", synced.Data.HeadTip, originTip)
	}
}

func TestSyncPRBranch_merge(t *testing.T) {
	setupTestDB(t)
	dir, mainTip, featureTip := initDivergedPRRepo(t)

	pr := CreatePR(dir, "Add feature", "", CreatePROptions{Base: "main", Head: "feature"})
	if !pr.Success {
		t.Fatalf("CreatePR() failed: %s", pr.Error.Message)
	}

	synced := SyncPRBranch(dir, pr.Data.ID, "merge")
	if !synced.Success {
		t.Fatalf("SyncPRBranch() failed: %s", synced.Error.Message)
	}

	newFeatureTip, _ := git.ReadRef(dir, "feature")
	parents, err := git.ExecGit(dir, []string{"rev-list", "--parents", "-n", "1", newFeatureTip})
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	fields := strings.Fields(strings.TrimSpace(parents.Stdout))
	if len(fields) != 3 || !strings.HasPrefix(fields[1], featureTip) || !strings.HasPrefix(fields[2], mainTip) {
		t.Errorf("merge tip parents = %v, want [%s %s]", fields[1:], featureTip, mainTip)
	}
	if got, _ := git.ReadRef(dir, "main"); got != mainTip {
		t.Errorf("main = %s, want it untouched at %s", got, mainTip)
	}
}

func TestSyncPRBranch_errors(t *testing.T) {
	setupTestDB(t)
	dir, _, _ := initDivergedPRRepo(t)

	if res := SyncPRBranch(dir, "#commit:aaaaaaaaaaaa", "rebase"); res.Success || res.Error.Code != "NOT_FOUND" {
		t.Errorf("SyncPRBranch() on an unknown PR = %+v, want NOT_FOUND", res)
	}

	pr := CreatePR(dir, "Add feature", "", CreatePROptions{Base: "main", Head: "feature"})
	if !pr.Success {
		t.Fatalf("CreatePR() failed: %s", pr.Error.Message)
	}
	if closed := ClosePR(dir, pr.Data.ID); !closed.Success {
		t.Fatalf("ClosePR() failed: %s", closed.Error.Message)
	}
	res := SyncPRBranch(dir, pr.Data.ID, "rebase")
	if res.Success || res.Error.Code != "INVALID_STATE" {
		t.Errorf("SyncPRBranch() on a closed PR = %+v, want INVALID_STATE", res)
	}
}

func TestRebaseStack(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	// feature (PR A) branches off main; feature2 (PR B) branches off feature.
	git.ExecGit(dir, []string{"checkout", "feature"})
	commitFile(t, dir, "feature.txt", "one\n", "feature one")
	publish(t, dir, "feature")
	git.ExecGit(dir, []string{"checkout", "-b", "feature2"})
	commitFile(t, dir, "feature2.txt", "two\n", "feature two")
	featureTwoTip := publish(t, dir, "feature2")

	git.ExecGit(dir, []string{"checkout", "main"})
	commitFile(t, dir, "main.txt", "main\n", "main moves on")
	publish(t, dir, "main")

	prA := CreatePR(dir, "Feature A", "", CreatePROptions{Base: "main", Head: "feature"})
	if !prA.Success {
		t.Fatalf("CreatePR(A) failed: %s", prA.Error.Message)
	}
	prB := CreatePR(dir, "Feature B", "", CreatePROptions{
		Base: "feature", Head: "feature2", DependsOn: []string{prA.Data.ID},
	})
	if !prB.Success {
		t.Fatalf("CreatePR(B) failed: %s", prB.Error.Message)
	}

	// Rebase A onto the advanced main, then carry the stack above it.
	if synced := SyncPRBranch(dir, prA.Data.ID, "rebase"); !synced.Success {
		t.Fatalf("SyncPRBranch(A) failed: %s", synced.Error.Message)
	}
	rebased := RebaseStack(dir, prA.Data.ID)
	if !rebased.Success {
		t.Fatalf("RebaseStack() failed: %s", rebased.Error.Message)
	}
	if len(rebased.Data) != 1 || rebased.Data[0].ID != prB.Data.ID {
		t.Fatalf("RebaseStack() updated %d PRs, want just B", len(rebased.Data))
	}

	newTwoTip, _ := git.ReadRef(dir, "feature2")
	if newTwoTip == featureTwoTip {
		t.Error("feature2 was not rewritten by the stack rebase")
	}
	if behind, err := git.GetBehindCount(dir, "refs/heads/feature", "refs/heads/feature2"); err != nil || behind != 0 {
		t.Errorf("GetBehindCount(feature, feature2) = %d, %v, want 0", behind, err)
	}
	if behind, err := git.GetBehindCount(dir, "refs/heads/main", "refs/heads/feature2"); err != nil || behind != 0 {
		t.Errorf("feature2 is still %d commits behind main after the stack rebase (err %v)", behind, err)
	}
}

func TestRebaseStack_noDependents(t *testing.T) {
	setupTestDB(t)
	dir, _, _ := initDivergedPRRepo(t)

	pr := CreatePR(dir, "Add feature", "", CreatePROptions{Base: "main", Head: "feature"})
	if !pr.Success {
		t.Fatalf("CreatePR() failed: %s", pr.Error.Message)
	}
	res := RebaseStack(dir, pr.Data.ID)
	if res.Success || res.Error.Code != "NO_DEPENDENTS" {
		t.Errorf("RebaseStack() = %+v, want NO_DEPENDENTS", res)
	}
}

func TestComparePRVersions(t *testing.T) {
	setupTestDB(t)
	dir, _, _ := initDivergedPRRepo(t)

	pr := CreatePR(dir, "Add feature", "", CreatePROptions{Base: "main", Head: "feature"})
	if !pr.Success {
		t.Fatalf("CreatePR() failed: %s", pr.Error.Message)
	}

	single := GetPRVersions(pr.Data.ID, "")
	if !single.Success || len(single.Data) != 1 {
		t.Fatalf("GetPRVersions() = %+v, want one version", single)
	}
	if res := ComparePRVersions(dir, t.TempDir(), pr.Data.ID, 0, 1); res.Success || res.Error.Code != "INVALID_VERSION" {
		t.Errorf("ComparePRVersions() out of range = %+v, want INVALID_VERSION", res)
	}

	// A second version with new head commits, so the comparison has code to show.
	if synced := SyncPRBranch(dir, pr.Data.ID, "rebase"); !synced.Success {
		t.Fatalf("SyncPRBranch() failed: %s", synced.Error.Message)
	}
	git.ExecGit(dir, []string{"checkout", "feature"})
	commitFile(t, dir, "feature.txt", "one\ntwo\n", "feature two")
	publish(t, dir, "feature")
	git.ExecGit(dir, []string{"checkout", "main"})
	if updated := UpdatePRTips(dir, pr.Data.ID); !updated.Success {
		t.Fatalf("UpdatePRTips() failed: %s", updated.Error.Message)
	}

	versions := GetPRVersions(pr.Data.ID, "")
	if !versions.Success || len(versions.Data) < 2 {
		t.Fatalf("GetPRVersions() = %d versions, want at least 2", len(versions.Data))
	}
	last := len(versions.Data) - 1
	diff := ComparePRVersions(dir, t.TempDir(), pr.Data.ID, 0, last)
	if !diff.Success {
		t.Fatalf("ComparePRVersions() failed: %s", diff.Error.Message)
	}
	if !strings.Contains(diff.Data, "feature two") {
		t.Errorf("range-diff omitted the new commit:\n%s", diff.Data)
	}
	if same := ComparePRVersions(dir, t.TempDir(), pr.Data.ID, last, last); !same.Success || same.Data != "" {
		t.Errorf("ComparePRVersions() of a version with itself = %+v, want an empty diff", same)
	}
}
