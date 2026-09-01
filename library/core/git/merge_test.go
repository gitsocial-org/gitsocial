// merge_test.go - Graph-shape tests for the merge, squash, rebase and range-diff operations
package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAndCommit writes a file and commits it on the current branch.
func writeAndCommit(t *testing.T, dir, name, content, message string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if _, err := ExecGit(dir, []string{"add", name}); err != nil {
		t.Fatalf("git add %s: %v", name, err)
	}
	if _, err := ExecGit(dir, []string{"commit", "-m", message}); err != nil {
		t.Fatalf("git commit %q: %v", message, err)
	}
	return revParse(t, dir, "HEAD")
}

// revParse resolves a revision to a full object id.
func revParse(t *testing.T, dir, rev string) string {
	t.Helper()
	out, err := ExecGit(dir, []string{"rev-parse", rev})
	if err != nil {
		t.Fatalf("rev-parse %s: %v", rev, err)
	}
	return strings.TrimSpace(out.Stdout)
}

// parentsOf returns the parent object ids of a commit, in order.
func parentsOf(t *testing.T, dir, hash string) []string {
	t.Helper()
	out, err := ExecGit(dir, []string{"rev-list", "--parents", "-n", "1", hash})
	if err != nil {
		t.Fatalf("rev-list --parents %s: %v", hash, err)
	}
	fields := strings.Fields(strings.TrimSpace(out.Stdout))
	if len(fields) == 0 {
		t.Fatalf("rev-list --parents %s returned nothing", hash)
	}
	return fields[1:]
}

// subjectsBetween lists commit subjects in from..to, oldest first.
func subjectsBetween(t *testing.T, dir, from, to string) []string {
	t.Helper()
	out, err := ExecGit(dir, []string{"log", "--reverse", "--format=%s", from + ".." + to})
	if err != nil {
		t.Fatalf("log %s..%s: %v", from, to, err)
	}
	trimmed := strings.TrimSpace(out.Stdout)
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// fileAt reads a file's content out of a commit's tree.
func fileAt(t *testing.T, dir, hash, path string) string {
	t.Helper()
	out, err := ExecGit(dir, []string{"show", hash + ":" + path})
	if err != nil {
		return ""
	}
	return out.Stdout
}

// initDivergedRepo builds main and feature branches that have both moved on
// since their merge base, touching different files so no merge conflicts.
func initDivergedRepo(t *testing.T) (dir, mainTip, featureTip, mergeBase string) {
	t.Helper()
	dir = initTestRepo(t)
	mergeBase = writeAndCommit(t, dir, "shared.txt", "base\n", "shared base")

	ExecGit(dir, []string{"checkout", "-b", "feature"})
	writeAndCommit(t, dir, "feature.txt", "one\n", "feature one")
	featureTip = writeAndCommit(t, dir, "feature.txt", "one\ntwo\n", "feature two")

	ExecGit(dir, []string{"checkout", "main"})
	mainTip = writeAndCommit(t, dir, "main.txt", "main\n", "main moves on")
	// Leave HEAD off both branches so the plumbing paths are exercised the way
	// the merge API promises: never touching the user's checkout.
	ExecGit(dir, []string{"checkout", "-b", "bystander"})
	return dir, mainTip, featureTip, mergeBase
}

func TestMergeBranches_fastForwardShape(t *testing.T) {
	dir := initTestRepo(t)
	writeAndCommit(t, dir, "shared.txt", "base\n", "shared base")
	ExecGit(dir, []string{"checkout", "-b", "feature"})
	featureTip := writeAndCommit(t, dir, "feature.txt", "one\n", "feature one")
	ExecGit(dir, []string{"checkout", "-b", "bystander"})

	hash, err := MergeBranches(dir, "main", "feature")
	if err != nil {
		t.Fatalf("MergeBranches() error = %v", err)
	}
	if hash != featureTip {
		t.Errorf("MergeBranches() = %s, want the head tip %s", hash, featureTip)
	}
	if got := revParse(t, dir, "refs/heads/main"); got != featureTip {
		t.Errorf("main = %s, want %s", got, featureTip)
	}
	if parents := parentsOf(t, dir, hash); len(parents) != 1 {
		t.Errorf("fast-forward produced %d parents, want 1 (no merge commit)", len(parents))
	}
}

func TestMergeBranches_threeWayShape(t *testing.T) {
	dir, mainTip, featureTip, _ := initDivergedRepo(t)

	hash, err := MergeBranches(dir, "main", "feature")
	if err != nil {
		t.Fatalf("MergeBranches() error = %v", err)
	}
	parents := parentsOf(t, dir, hash)
	if len(parents) != 2 || parents[0] != mainTip || parents[1] != featureTip {
		t.Errorf("parents = %v, want [%s %s]", parents, mainTip, featureTip)
	}
	if got := revParse(t, dir, "refs/heads/main"); got != hash {
		t.Errorf("main = %s, want the merge commit %s", got, hash)
	}
	if got := revParse(t, dir, "refs/heads/feature"); got != featureTip {
		t.Errorf("feature moved to %s, a merge must not touch the head branch", got)
	}
	if fileAt(t, dir, hash, "feature.txt") == "" || fileAt(t, dir, hash, "main.txt") == "" {
		t.Error("the merge tree should carry both sides' files")
	}
}

func TestSquashMerge(t *testing.T) {
	dir, mainTip, featureTip, _ := initDivergedRepo(t)

	hash, err := SquashMerge(dir, "main", "feature", "Squashed feature")
	if err != nil {
		t.Fatalf("SquashMerge() error = %v", err)
	}
	parents := parentsOf(t, dir, hash)
	if len(parents) != 1 || parents[0] != mainTip {
		t.Errorf("parents = %v, want exactly [%s]: a squash keeps no link to the head branch", parents, mainTip)
	}
	if got := revParse(t, dir, "refs/heads/main"); got != hash {
		t.Errorf("main = %s, want %s", got, hash)
	}
	if got := revParse(t, dir, "refs/heads/feature"); got != featureTip {
		t.Errorf("feature moved to %s, a squash must not touch the head branch", got)
	}
	subjects := subjectsBetween(t, dir, mainTip, hash)
	if len(subjects) != 1 || subjects[0] != "Squashed feature" {
		t.Errorf("subjects = %v, want one squash commit", subjects)
	}
	if strings.TrimSpace(fileAt(t, dir, hash, "feature.txt")) != "one\ntwo" {
		t.Error("the squash commit should carry the head branch's final content")
	}
}

func TestRebaseMerge(t *testing.T) {
	dir, mainTip, featureTip, _ := initDivergedRepo(t)

	hash, err := RebaseMerge(dir, "main", "feature")
	if err != nil {
		t.Fatalf("RebaseMerge() error = %v", err)
	}
	if got := revParse(t, dir, "refs/heads/main"); got != hash {
		t.Errorf("main = %s, want %s", got, hash)
	}
	if got := revParse(t, dir, "refs/heads/feature"); got != featureTip {
		t.Errorf("feature moved to %s, RebaseMerge updates base only", got)
	}
	subjects := subjectsBetween(t, dir, mainTip, hash)
	want := []string{"feature one", "feature two"}
	if len(subjects) != len(want) {
		t.Fatalf("subjects = %v, want %v (linear replay, no merge commit)", subjects, want)
	}
	for i := range want {
		if subjects[i] != want[i] {
			t.Errorf("subject[%d] = %q, want %q", i, subjects[i], want[i])
		}
	}
	for _, rev := range []string{hash, hash + "^"} {
		if parents := parentsOf(t, dir, revParse(t, dir, rev)); len(parents) != 1 {
			t.Errorf("replayed commit %s has %d parents, want 1", rev, len(parents))
		}
	}
	if parents := parentsOf(t, dir, revParse(t, dir, hash+"^")); parents[0] != mainTip {
		t.Errorf("first replayed commit sits on %s, want the old base tip %s", parents[0], mainTip)
	}
	if fileAt(t, dir, hash, "main.txt") == "" {
		t.Error("the rebased tip should still carry the base branch's file")
	}
}

func TestForceMerge_makesAMergeCommitEvenWhenFastForwardable(t *testing.T) {
	dir := initTestRepo(t)
	mainTip := writeAndCommit(t, dir, "shared.txt", "base\n", "shared base")
	ExecGit(dir, []string{"checkout", "-b", "feature"})
	featureTip := writeAndCommit(t, dir, "feature.txt", "one\n", "feature one")
	ExecGit(dir, []string{"checkout", "-b", "bystander"})

	hash, err := ForceMerge(dir, "main", "feature")
	if err != nil {
		t.Fatalf("ForceMerge() error = %v", err)
	}
	parents := parentsOf(t, dir, hash)
	if len(parents) != 2 || parents[0] != mainTip || parents[1] != featureTip {
		t.Errorf("parents = %v, want [%s %s]", parents, mainTip, featureTip)
	}
	if got := revParse(t, dir, "refs/heads/main"); got != hash {
		t.Errorf("main = %s, want the merge commit %s", got, hash)
	}
}

func TestRebaseBranch(t *testing.T) {
	dir, mainTip, featureTip, _ := initDivergedRepo(t)

	hash, err := RebaseBranch(dir, "main", "feature")
	if err != nil {
		t.Fatalf("RebaseBranch() error = %v", err)
	}
	if got := revParse(t, dir, "refs/heads/feature"); got != hash {
		t.Errorf("feature = %s, want the rebased tip %s", got, hash)
	}
	if got := revParse(t, dir, "refs/heads/main"); got != mainTip {
		t.Errorf("main = %s, want it untouched at %s", got, mainTip)
	}
	if hash == featureTip {
		t.Error("rebasing onto a moved base should rewrite the head commits")
	}
	subjects := subjectsBetween(t, dir, mainTip, hash)
	if len(subjects) != 2 || subjects[0] != "feature one" || subjects[1] != "feature two" {
		t.Errorf("subjects = %v, want [feature one feature two]", subjects)
	}
	if behind, err := GetBehindCount(dir, "refs/heads/main", "refs/heads/feature"); err != nil || behind != 0 {
		t.Errorf("GetBehindCount() = %d, %v, want 0 after a rebase onto main", behind, err)
	}
}

func TestRebaseBranch_noCommitsToReplay(t *testing.T) {
	dir := initTestRepo(t)
	writeAndCommit(t, dir, "shared.txt", "base\n", "shared base")
	ExecGit(dir, []string{"branch", "feature"})
	ExecGit(dir, []string{"checkout", "-b", "bystander"})

	if _, err := RebaseBranch(dir, "main", "feature"); err == nil {
		t.Error("RebaseBranch() with nothing to replay should error")
	}
	if _, err := RebaseMerge(dir, "main", "feature"); err == nil {
		t.Error("RebaseMerge() with nothing to replay should error")
	}
}

func TestGetMergeBaseAndBehindCount(t *testing.T) {
	dir, _, _, mergeBase := initDivergedRepo(t)

	got, err := GetMergeBase(dir, "refs/heads/main", "refs/heads/feature")
	if err != nil {
		t.Fatalf("GetMergeBase() error = %v", err)
	}
	if got != mergeBase {
		t.Errorf("GetMergeBase() = %s, want %s", got, mergeBase)
	}
	behind, err := GetBehindCount(dir, "refs/heads/main", "refs/heads/feature")
	if err != nil {
		t.Fatalf("GetBehindCount() error = %v", err)
	}
	if behind != 1 {
		t.Errorf("GetBehindCount() = %d, want 1 (feature misses one base commit)", behind)
	}
}

func TestRangeDiffAndPatchesEqual(t *testing.T) {
	dir, mainTip, featureTip, mergeBase := initDivergedRepo(t)

	t.Run("identical ranges", func(t *testing.T) {
		out, err := RangeDiff(dir, mergeBase, featureTip, mergeBase, featureTip)
		if err != nil {
			t.Fatalf("RangeDiff() error = %v", err)
		}
		if strings.Contains(out, "!") || strings.Contains(out, ">") {
			t.Errorf("RangeDiff() of a range against itself reported changes:\n%s", out)
		}
		equal, err := PatchesEqual(dir, mergeBase, featureTip, mergeBase, featureTip)
		if err != nil {
			t.Fatalf("PatchesEqual() error = %v", err)
		}
		if !equal {
			t.Error("PatchesEqual() = false for a range against itself")
		}
	})

	t.Run("rebased range keeps the same patches", func(t *testing.T) {
		rebased, err := RebaseBranch(dir, "main", "feature")
		if err != nil {
			t.Fatalf("RebaseBranch() error = %v", err)
		}
		equal, err := PatchesEqual(dir, mergeBase, featureTip, mainTip, rebased)
		if err != nil {
			t.Fatalf("PatchesEqual() error = %v", err)
		}
		if !equal {
			out, _ := RangeDiff(dir, mergeBase, featureTip, mainTip, rebased)
			t.Errorf("PatchesEqual() = false after a pure rebase; range-diff:\n%s", out)
		}
	})

	t.Run("changed content differs", func(t *testing.T) {
		ExecGit(dir, []string{"checkout", "-B", "reworked", featureTip})
		reworked := writeAndCommit(t, dir, "feature.txt", "one\ntwo\nthree\n", "feature three")
		ExecGit(dir, []string{"checkout", "bystander"})

		equal, err := PatchesEqual(dir, mergeBase, featureTip, mergeBase, reworked)
		if err != nil {
			t.Fatalf("PatchesEqual() error = %v", err)
		}
		if equal {
			t.Error("PatchesEqual() = true although a commit was added")
		}
		out, err := RangeDiff(dir, mergeBase, featureTip, mergeBase, reworked)
		if err != nil {
			t.Fatalf("RangeDiff() error = %v", err)
		}
		if !strings.Contains(out, "feature three") {
			t.Errorf("RangeDiff() omitted the added commit:\n%s", out)
		}
	})

	t.Run("empty old range synthesizes additions", func(t *testing.T) {
		out, err := RangeDiff(dir, mergeBase, mergeBase, mergeBase, featureTip)
		if err != nil {
			t.Fatalf("RangeDiff() error = %v", err)
		}
		if !strings.Contains(out, ">") || !strings.Contains(out, "feature one") {
			t.Errorf("RangeDiff() from an empty range should list additions:\n%s", out)
		}
		equal, err := PatchesEqual(dir, mergeBase, mergeBase, mergeBase, featureTip)
		if err != nil {
			t.Fatalf("PatchesEqual() error = %v", err)
		}
		if equal {
			t.Error("PatchesEqual() = true comparing an empty range with a non-empty one")
		}
	})

	t.Run("both ranges empty", func(t *testing.T) {
		out, err := RangeDiff(dir, mergeBase, mergeBase, featureTip, featureTip)
		if err != nil {
			t.Fatalf("RangeDiff() error = %v", err)
		}
		if out != "" {
			t.Errorf("RangeDiff() = %q, want empty for two empty ranges", out)
		}
		equal, err := PatchesEqual(dir, mergeBase, mergeBase, featureTip, featureTip)
		if err != nil || !equal {
			t.Errorf("PatchesEqual() = %v, %v, want true for two empty ranges", equal, err)
		}
	})
}
