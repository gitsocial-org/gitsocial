// adopt_test.go - Tests for adopting a registered fork's issue (GITMSG.md 1.5)
package pm

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// forkWithIssue registers a fork of the workspace holding one issue filed by its own author.
func forkWithIssue(t *testing.T, workdir string) Issue {
	t.Helper()
	fork := otherAuthorRepo(t, "Forker", "forker@test.com")
	issue := newIssue(t, fork, "Crash on startup", CreateIssueOptions{})
	if err := gitmsg.AddFork(workdir, gitmsg.ResolveRepoURL(fork)); err != nil {
		t.Fatalf("AddFork: %v", err)
	}
	return issue
}

// headerOfLatest returns the GitMsg header of the workspace PM branch tip.
func headerOfLatest(t *testing.T, workdir string) *protocol.Message {
	t.Helper()
	body, err := git.ExecGit(workdir, []string{"log", "-1", "--format=%B", gitmsg.GetExtBranch(workdir, "pm")})
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	msg := protocol.ParseMessage(strings.TrimSpace(body.Stdout))
	if msg == nil {
		t.Fatalf("tip carries no GitMsg header:\n%s", body.Stdout)
	}
	return msg
}

// TestUpdateIssue_AdoptsForkIssue asserts the first change to a fork's issue writes one copy carrying adopts and the author's GitMsg-Ref.
func TestUpdateIssue_AdoptsForkIssue(t *testing.T) {
	workdir := initWorkspace(t)
	original := forkWithIssue(t, workdir)
	closed := StateClosed
	res := UpdateIssue(workdir, original.ID, UpdateIssueOptions{State: &closed})
	if !res.Success {
		t.Fatalf("UpdateIssue: %s", res.Error.Message)
	}
	if res.Data.Repository != gitmsg.ResolveRepoURL(workdir) || res.Data.State != StateClosed {
		t.Errorf("result = %s in %s, want the closed copy in the workspace", res.Data.State, res.Data.Repository)
	}
	msg := headerOfLatest(t, workdir)
	if msg.Header.Fields["adopts"] != original.ID || msg.Header.Fields["edits"] != "" {
		t.Errorf("header adopts=%q edits=%q, want adopts=%q and no edits", msg.Header.Fields["adopts"], msg.Header.Fields["edits"], original.ID)
	}
	if len(msg.References) == 0 || msg.References[0].Ref != original.ID || msg.References[0].Author != "Forker" {
		t.Errorf("references = %+v, want a GitMsg-Ref of the original by Forker", msg.References)
	}
	if res.Data.OriginalAuthor == nil || res.Data.OriginalAuthor.Name != "Forker" || res.Data.Adopts != original.ID {
		t.Errorf("copy original author = %+v adopts = %q, want Forker and %q", res.Data.OriginalAuthor, res.Data.Adopts, original.ID)
	}
	if got, err := GetPMItemByRef(original.ID, gitmsg.ResolveRepoURL(workdir)); err != nil || got.RepoURL != original.Repository {
		t.Errorf("a lookup by the fork ref must still reach the fork original, got %+v, %v", got, err)
	}
	forkItem, err := GetPMItem(original.Repository, protocol.ParseRef(original.ID).Value, original.Branch)
	if err != nil || forkItem.State != string(StateOpen) {
		t.Errorf("the fork's original must stay open, got %+v, %v", forkItem, err)
	}
}

// TestUpdateIssue_EditsAdoptedCopy asserts a second change, by either ref, edits the copy and writes no second copy.
func TestUpdateIssue_EditsAdoptedCopy(t *testing.T) {
	workdir := initWorkspace(t)
	original := forkWithIssue(t, workdir)
	closed, open := StateClosed, StateOpen
	first := UpdateIssue(workdir, original.ID, UpdateIssueOptions{State: &closed})
	if !first.Success {
		t.Fatalf("UpdateIssue: %s", first.Error.Message)
	}
	for _, ref := range []string{original.ID, first.Data.ID} {
		res := UpdateIssue(workdir, ref, UpdateIssueOptions{State: &open})
		if !res.Success {
			t.Fatalf("UpdateIssue(%s): %s", ref, res.Error.Message)
		}
		msg := headerOfLatest(t, workdir)
		if msg.Header.Fields["adopts"] != "" || protocol.ParseRef(msg.Header.Fields["edits"]).Value != protocol.ParseRef(first.Data.ID).Value {
			t.Errorf("change by %s wrote adopts=%q edits=%q, want an edit of the copy %s", ref, msg.Header.Fields["adopts"], msg.Header.Fields["edits"], first.Data.ID)
		}
	}
	copies, err := adoptedCopies(gitmsg.ResolveRepoURL(workdir))
	if err != nil || len(copies) != 1 {
		t.Errorf("adopted copies = %d, %v, want 1", len(copies), err)
	}
}

// TestGetIssuesWithForks_CollapsesAdopted asserts the issue list shows the copy and not the fork original.
func TestGetIssuesWithForks_CollapsesAdopted(t *testing.T) {
	workdir := initWorkspace(t)
	original := forkWithIssue(t, workdir)
	closed := StateClosed
	copied := UpdateIssue(workdir, original.ID, UpdateIssueOptions{State: &closed})
	if !copied.Success {
		t.Fatalf("UpdateIssue: %s", copied.Error.Message)
	}
	list := GetIssuesWithForks(gitmsg.ResolveRepoURL(workdir), gitmsg.GetExtBranch(workdir, "pm"), gitmsg.GetForks(workdir), nil, "", 0)
	if !list.Success {
		t.Fatalf("GetIssuesWithForks: %s", list.Error.Message)
	}
	var sawCopy, sawOriginal bool
	for _, issue := range list.Data {
		sawCopy = sawCopy || issue.ID == copied.Data.ID
		sawOriginal = sawOriginal || issue.ID == original.ID
	}
	if !sawCopy || sawOriginal {
		t.Errorf("list has copy=%v original=%v, want the copy alone", sawCopy, sawOriginal)
	}
}

// TestUpdateIssue_NonForkStaysProposal asserts a change to an unregistered repository's issue stays a cross-repository edit.
func TestUpdateIssue_NonForkStaysProposal(t *testing.T) {
	workdir := initWorkspace(t)
	other := otherAuthorRepo(t, "Other", "other@test.com")
	issue := newIssue(t, other, "Elsewhere", CreateIssueOptions{})
	closed := StateClosed
	if res := UpdateIssue(workdir, issue.ID, UpdateIssueOptions{State: &closed}); !res.Success {
		t.Fatalf("UpdateIssue: %s", res.Error.Message)
	}
	msg := headerOfLatest(t, workdir)
	if msg.Header.Fields["adopts"] != "" || msg.Header.Fields["edits"] != issue.ID {
		t.Errorf("header adopts=%q edits=%q, want a proposal editing %s", msg.Header.Fields["adopts"], msg.Header.Fields["edits"], issue.ID)
	}
}

// TestRetractIssue_AdoptedForkRefStaysProposal asserts retracting a fork original after its adoption writes a proposal and leaves the copy.
func TestRetractIssue_AdoptedForkRefStaysProposal(t *testing.T) {
	workdir := initWorkspace(t)
	original := forkWithIssue(t, workdir)
	closed := StateClosed
	copied := UpdateIssue(workdir, original.ID, UpdateIssueOptions{State: &closed})
	if !copied.Success {
		t.Fatalf("UpdateIssue: %s", copied.Error.Message)
	}
	if res := RetractIssue(workdir, original.ID); !res.Success {
		t.Fatalf("RetractIssue: %s", res.Error.Message)
	}
	if msg := headerOfLatest(t, workdir); msg.Header.Fields["edits"] != original.ID {
		t.Errorf("retraction edits %q, want a proposal to %s", msg.Header.Fields["edits"], original.ID)
	}
	if retracted, err := IsItemRetracted(copied.Data.Repository, protocol.ParseRef(copied.Data.ID).Value, copied.Data.Branch); err != nil || retracted {
		t.Errorf("the adopted copy must survive a retraction of the fork original, retracted=%v err=%v", retracted, err)
	}
}

// TestAdoptIssue asserts adopting writes an unchanged copy once, and refuses a workspace issue and a non-fork issue.
func TestAdoptIssue(t *testing.T) {
	workdir := initWorkspace(t)
	original := forkWithIssue(t, workdir)
	first := AdoptIssue(workdir, original.ID)
	if !first.Success {
		t.Fatalf("AdoptIssue: %s", first.Error.Message)
	}
	if first.Data.Repository != gitmsg.ResolveRepoURL(workdir) || first.Data.State != original.State || first.Data.Subject != original.Subject {
		t.Errorf("copy = %+v, want an unchanged copy in the workspace", first.Data)
	}
	again := AdoptIssue(workdir, original.ID)
	if !again.Success || again.Data.ID != first.Data.ID {
		t.Errorf("a second adoption returned %+v, want the first copy %s", again.Data, first.Data.ID)
	}
	if copies, _ := adoptedCopies(gitmsg.ResolveRepoURL(workdir)); len(copies) != 1 {
		t.Errorf("adopted copies = %d, want 1", len(copies))
	}
	if res := AdoptIssue(workdir, first.Data.ID); res.Success || res.Error.Code != "NOT_FOREIGN" {
		t.Errorf("adopting a workspace issue = %+v, want NOT_FOREIGN", res.Error)
	}
	other := newIssue(t, otherAuthorRepo(t, "Other", "other@test.com"), "Elsewhere", CreateIssueOptions{})
	if res := AdoptIssue(workdir, other.ID); res.Success || res.Error.Code != "NOT_A_FORK" {
		t.Errorf("adopting a non-fork issue = %+v, want NOT_A_FORK", res.Error)
	}
}
