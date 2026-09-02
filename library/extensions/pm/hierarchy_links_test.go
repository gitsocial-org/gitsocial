// hierarchy_links_test.go - Tests for parent/root derivation, cycle detection, and blocked-by resolution
package pm

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
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

// issueHeader returns the GitMsg header line of the commit behind an issue ref.
func issueHeader(t *testing.T, workdir, issueRef string) string {
	t.Helper()
	out, err := git.ExecGit(workdir, []string{"log", "-1", "--format=%B", refHash(t, issueRef)})
	if err != nil {
		t.Fatalf("read commit %s: %v", refHash(t, issueRef), err)
	}
	for _, line := range strings.Split(out.Stdout, "\n") {
		if strings.HasPrefix(line, "GitMsg:") {
			return line
		}
	}
	t.Fatalf("no GitMsg header in commit %s", refHash(t, issueRef))
	return ""
}

// branchTipHeader returns the GitMsg header line of the newest commit on the PM
// branch, which is where an edit lands.
func branchTipHeader(t *testing.T, workdir string) string {
	t.Helper()
	out, err := git.ExecGit(workdir, []string{"log", "-1", "--format=%B", gitmsg.GetExtBranch(workdir, "pm")})
	if err != nil {
		t.Fatalf("read branch tip: %v", err)
	}
	for _, line := range strings.Split(out.Stdout, "\n") {
		if strings.HasPrefix(line, "GitMsg:") {
			return line
		}
	}
	t.Fatal("no GitMsg header on the branch tip")
	return ""
}

// TestCreateIssueDerivesHierarchy pins the spec form for a caller that names
// only a parent: root alone at the first level (GITPM.md §1.7, where parent and
// root would be the same commit), both fields below it. A caller that skipped
// the derivation used to write parent with no root, which the next level down
// then read as "my parent is top-level" and mis-rooted.
func TestCreateIssueDerivesHierarchy(t *testing.T) {
	setupTestDB(t)
	workdir := cloneFixture(t)

	top := newIssue(t, workdir, "Top", CreateIssueOptions{})
	child := newIssue(t, workdir, "Child", CreateIssueOptions{Parent: top.ID})
	header := issueHeader(t, workdir, child.ID)
	if strings.Contains(header, "parent=") {
		t.Errorf("header = %q, want no parent field for a direct child", header)
	}
	if !strings.Contains(header, "root=\"#commit:"+refHash(t, top.ID)) {
		t.Errorf("header = %q, want root referencing the top-level issue", header)
	}

	grandchild := newIssue(t, workdir, "Grandchild", CreateIssueOptions{Parent: child.ID})
	header = issueHeader(t, workdir, grandchild.ID)
	if !strings.Contains(header, "parent=\"#commit:"+refHash(t, child.ID)) {
		t.Errorf("header = %q, want parent referencing the immediate parent", header)
	}
	if !strings.Contains(header, "root=\"#commit:"+refHash(t, top.ID)) {
		t.Errorf("header = %q, want root referencing the top-level ancestor, not the parent", header)
	}
}

// TestCreateIssueKeepsExplicitRoot leaves a caller that supplies both fields
// alone: explicit wins over derivation.
func TestCreateIssueKeepsExplicitRoot(t *testing.T) {
	setupTestDB(t)
	workdir := cloneFixture(t)
	repoURL := gitmsg.ResolveRepoURL(workdir)

	top := newIssue(t, workdir, "Top", CreateIssueOptions{})
	child := newIssue(t, workdir, "Child", CreateIssueOptions{Parent: top.ID})
	parentRef, rootRef, err := DeriveHierarchy(child.ID, repoURL, "")
	if err != nil {
		t.Fatalf("DeriveHierarchy: %v", err)
	}
	grandchild := newIssue(t, workdir, "Grandchild", CreateIssueOptions{Parent: parentRef, Root: rootRef})
	header := issueHeader(t, workdir, grandchild.ID)
	if !strings.Contains(header, "root=\"#commit:"+refHash(t, top.ID)) {
		t.Errorf("header = %q, want the caller's explicit root preserved", header)
	}
}

// TestUpdateIssueRederivesHierarchy covers a move: naming a new parent without
// a root must re-derive the root rather than strand the one already on the
// issue, and clearing the parent must clear the root with it.
func TestUpdateIssueRederivesHierarchy(t *testing.T) {
	setupTestDB(t)
	workdir := cloneFixture(t)

	topA := newIssue(t, workdir, "Top A", CreateIssueOptions{})
	topB := newIssue(t, workdir, "Top B", CreateIssueOptions{})
	child := newIssue(t, workdir, "Child", CreateIssueOptions{Parent: topA.ID})

	// UpdateIssue returns the canonical issue, so the edit's own header is on
	// the branch tip rather than on the returned ID.
	moved := topB.ID
	res := UpdateIssue(workdir, child.ID, UpdateIssueOptions{Parent: &moved})
	if !res.Success {
		t.Fatalf("UpdateIssue(move) failed: %s", res.Error.Message)
	}
	header := branchTipHeader(t, workdir)
	if !strings.Contains(header, "root=\"#commit:"+refHash(t, topB.ID)) {
		t.Errorf("header = %q, want root re-derived to the new parent", header)
	}
	if strings.Contains(header, refHash(t, topA.ID)) {
		t.Errorf("header = %q, still references the old parent", header)
	}

	cleared := ""
	res = UpdateIssue(workdir, res.Data.ID, UpdateIssueOptions{Parent: &cleared})
	if !res.Success {
		t.Fatalf("UpdateIssue(clear) failed: %s", res.Error.Message)
	}
	header = branchTipHeader(t, workdir)
	if strings.Contains(header, "root=") || strings.Contains(header, "parent=") {
		t.Errorf("header = %q, want both hierarchy fields cleared", header)
	}
}

// TestCreateIssueRejectsUnknownParent surfaces the derivation error instead of
// writing a dangling ref.
func TestCreateIssueRejectsUnknownParent(t *testing.T) {
	setupTestDB(t)
	workdir := cloneFixture(t)

	res := CreateIssue(workdir, "Orphan", "", CreateIssueOptions{Parent: "#commit:deadbeefcafe@gitmsg/pm"})
	if res.Success {
		t.Fatal("CreateIssue with an unknown parent succeeded, want INVALID_PARENT")
	}
	if res.Error.Code != "INVALID_PARENT" {
		t.Errorf("code = %q, want INVALID_PARENT", res.Error.Code)
	}
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
