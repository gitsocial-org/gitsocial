// hierarchy_links_test.go - Tests for parent/root derivation, cycle detection, and blocked-by resolution
package pm

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
)

// newIssue creates an issue and fails the test if creation did not succeed.
func newIssue(t *testing.T, workdir, subject string, opts CreateIssueOptions) Issue {
	t.Helper()
	res := CreateIssue(workdir, subject, "", opts)
	if !res.Success {
		t.Fatalf("CreateIssue(%q) failed: %s", subject, res.Error.Message)
	}
	return res.Data
}

func TestDeriveHierarchy(t *testing.T) {
	setupTestDB(t)
	workdir := cloneFixture(t)
	repoURL := gitmsg.ResolveRepoURL(workdir)

	// Mirror the real callers (CLI, TUI): derive the parent/root header refs
	// first, then create the issue carrying them.
	parent := newIssue(t, workdir, "Parent", CreateIssueOptions{})
	childParent, childRoot, err := DeriveHierarchy(parent.ID, repoURL, "")
	if err != nil {
		t.Fatalf("DeriveHierarchy(parent) error = %v", err)
	}
	if childParent != "" {
		t.Errorf("parent = %q, want empty: a direct child of a top-level issue carries only root", childParent)
	}
	if !strings.Contains(childRoot, refHash(t, parent.ID)) {
		t.Errorf("root = %q, want a ref to the top-level issue", childRoot)
	}
	child := newIssue(t, workdir, "Child", CreateIssueOptions{Parent: childParent, Root: childRoot})

	grandParent, grandRoot, err := DeriveHierarchy(child.ID, repoURL, "")
	if err != nil {
		t.Fatalf("DeriveHierarchy(child) error = %v", err)
	}
	if !strings.Contains(grandParent, refHash(t, child.ID)) {
		t.Errorf("parent = %q, want a ref to the child issue", grandParent)
	}
	if !strings.Contains(grandRoot, refHash(t, parent.ID)) {
		t.Errorf("root = %q, want the top-level issue, not the immediate parent", grandRoot)
	}
	grandchild := newIssue(t, workdir, "Grandchild", CreateIssueOptions{Parent: grandParent, Root: grandRoot})

	t.Run("no parent", func(t *testing.T) {
		gotParent, gotRoot, err := DeriveHierarchy("", repoURL, "")
		if err != nil || gotParent != "" || gotRoot != "" {
			t.Errorf("DeriveHierarchy(\"\") = %q, %q, %v, want empty", gotParent, gotRoot, err)
		}
	})

	t.Run("unknown parent", func(t *testing.T) {
		if _, _, err := DeriveHierarchy("#commit:ffffffffffff", repoURL, ""); err == nil {
			t.Error("DeriveHierarchy() with an unknown parent should error")
		}
	})

	t.Run("self parent is refused", func(t *testing.T) {
		_, _, err := DeriveHierarchy(parent.ID, repoURL, parent.ID)
		if err == nil || !strings.Contains(err.Error(), "own parent") {
			t.Errorf("DeriveHierarchy() error = %v, want an own-parent refusal", err)
		}
	})

	t.Run("cycle is refused", func(t *testing.T) {
		// Making a descendant the parent of its own ancestor would close a loop.
		if _, _, err := DeriveHierarchy(grandchild.ID, repoURL, parent.ID); err == nil || !strings.Contains(err.Error(), "cycle") {
			t.Errorf("DeriveHierarchy(grandchild) error = %v, want a cycle refusal", err)
		}
		if _, _, err := DeriveHierarchy(child.ID, repoURL, parent.ID); err == nil || !strings.Contains(err.Error(), "cycle") {
			t.Errorf("DeriveHierarchy(child) error = %v, want a cycle refusal", err)
		}
		if _, _, err := DeriveHierarchy(grandchild.ID, repoURL, child.ID); err == nil || !strings.Contains(err.Error(), "cycle") {
			t.Errorf("DeriveHierarchy(grandchild, self=child) error = %v, want a cycle refusal", err)
		}
	})

	t.Run("reparenting outside the ancestry is allowed", func(t *testing.T) {
		other := newIssue(t, workdir, "Other top-level", CreateIssueOptions{})
		if _, _, err := DeriveHierarchy(other.ID, repoURL, grandchild.ID); err != nil {
			t.Errorf("DeriveHierarchy() error = %v, want it allowed", err)
		}
	})
}

// refHash returns the commit hash inside an issue ref.
func refHash(t *testing.T, issueRef string) string {
	t.Helper()
	hash := issueRef
	if i := strings.Index(hash, "#commit:"); i >= 0 {
		hash = hash[i+len("#commit:"):]
	}
	if i := strings.Index(hash, "@"); i >= 0 {
		hash = hash[:i]
	}
	if hash == "" {
		t.Fatalf("could not extract a hash from %q", issueRef)
	}
	return hash
}

func TestIsBlocked(t *testing.T) {
	setupTestDB(t)
	workdir := cloneFixture(t)

	blocker := newIssue(t, workdir, "Blocker", CreateIssueOptions{})
	blocked := newIssue(t, workdir, "Blocked", CreateIssueOptions{BlockedBy: []string{blocker.ID}})
	free := newIssue(t, workdir, "Unblocked", CreateIssueOptions{})

	if IsBlocked(free.ID) {
		t.Error("IsBlocked() = true for an issue with no links")
	}
	if !IsBlocked(blocked.ID) {
		t.Error("IsBlocked() = false while its blocker is open")
	}

	res := GetBlockedBy(blocked.ID)
	if !res.Success || len(res.Data) != 1 || res.Data[0].Subject != "Blocker" {
		t.Fatalf("GetBlockedBy() = %+v, want the one blocker", res)
	}

	closed := StateClosed
	if upd := UpdateIssue(workdir, blocker.ID, UpdateIssueOptions{State: &closed}); !upd.Success {
		t.Fatalf("UpdateIssue() failed: %s", upd.Error.Message)
	}
	if IsBlocked(blocked.ID) {
		t.Error("IsBlocked() = true although the only blocker is closed")
	}
}

func TestIsBlocked_reverseLink(t *testing.T) {
	setupTestDB(t)
	workdir := cloneFixture(t)

	target := newIssue(t, workdir, "Target", CreateIssueOptions{})
	blocker := newIssue(t, workdir, "Blocker", CreateIssueOptions{Blocks: []string{target.ID}})

	if !IsBlocked(target.ID) {
		t.Error("IsBlocked() = false although another open issue declares it blocks this one")
	}
	res := GetBlocking(blocker.ID)
	if !res.Success || len(res.Data) != 1 || res.Data[0].Subject != "Target" {
		t.Errorf("GetBlocking() = %+v, want the blocked issue", res)
	}
}

func TestIsBlocked_invalidRef(t *testing.T) {
	setupTestDB(t)
	_ = cloneFixture(t)

	if IsBlocked("") {
		t.Error("IsBlocked(\"\") = true, want false for an unparseable ref")
	}
	if IsBlocked("#commit:ffffffffffff") {
		t.Error("IsBlocked() = true for an issue that does not exist")
	}
}
