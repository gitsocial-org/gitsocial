// publish_test.go - Tests for the code branches a push publishes
package review

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// TestCodeBranchesToPush_openHeadsOnly asserts an open pull request head with unpushed commits is published and a closed one is not.
func TestCodeBranchesToPush_openHeadsOnly(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	if _, err := git.ExecGit(dir, []string{"checkout", "-b", "open-head"}); err != nil {
		t.Fatalf("checkout open-head: %v", err)
	}
	commitFile(t, dir, "open.txt", "one\n", "open head work")
	if _, err := git.ExecGit(dir, []string{"checkout", "main"}); err != nil {
		t.Fatalf("checkout main: %v", err)
	}
	if _, err := git.ExecGit(dir, []string{"checkout", "-b", "closed-head"}); err != nil {
		t.Fatalf("checkout closed-head: %v", err)
	}
	commitFile(t, dir, "closed.txt", "one\n", "closed head work")
	if _, err := git.ExecGit(dir, []string{"checkout", "main"}); err != nil {
		t.Fatalf("checkout main: %v", err)
	}

	open := CreatePR(dir, "Open work", "", CreatePROptions{Base: "main", Head: "open-head"})
	if !open.Success {
		t.Fatalf("CreatePR(open) failed: %s", open.Error.Message)
	}
	closed := CreatePR(dir, "Closed work", "", CreatePROptions{Base: "main", Head: "closed-head"})
	if !closed.Success {
		t.Fatalf("CreatePR(closed) failed: %s", closed.Error.Message)
	}
	if res := ClosePR(dir, closed.Data.ID); !res.Success {
		t.Fatalf("ClosePR() failed: %s", res.Error.Message)
	}

	branches, err := CodeBranchesToPush(dir, "origin")
	if err != nil {
		t.Fatalf("CodeBranchesToPush() error = %v", err)
	}
	if branches["open-head"] != 1 {
		t.Errorf("open-head = %d unpushed commits, want 1; push set = %v", branches["open-head"], branches)
	}
	if _, ok := branches["closed-head"]; ok {
		t.Errorf("closed-head is in the push set %v, want it left out", branches)
	}
}
