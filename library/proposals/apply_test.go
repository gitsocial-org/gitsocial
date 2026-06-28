// apply_test.go - Two-repo (distinct repo_url) accept round-trip: an item's
// canonical owner accepts a cross-repo proposal by authoring a same-repo mirror
// edit that wins resolution under the gating.
package proposals

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/extensions/release"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
)

func TestApplyPM_crossRepoClose(t *testing.T) {
	setupCache(t)
	bob := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")
	alice := cloneAs(t, initBareOrigin(t), "alice", "alice@test.com")

	bobURL := gitmsg.ResolveRepoURL(bob)
	if bobURL == gitmsg.ResolveRepoURL(alice) {
		t.Fatal("upstream and fork must have distinct repo_urls")
	}

	// Bob files an issue — its canonical lives on Bob's repo.
	created := pm.CreateIssue(bob, "Crash on startup", "Segfaults on empty config", pm.CreateIssueOptions{})
	if !created.Success {
		t.Fatalf("CreateIssue: %s", created.Error.Message)
	}
	issueRef := created.Data.ID

	// Alice closes it from her own repo: a cross-repo proposal, inert under gating.
	if res := pm.CloseIssue(alice, issueRef); !res.Success {
		t.Fatalf("CloseIssue: %s", res.Error.Message)
	}
	if got := pm.GetIssue(issueRef); !got.Success || got.Data.State != pm.StateOpen {
		t.Fatalf("pre-accept: want open (proposal inert under gating), got success=%v state=%q", got.Success, got.Data.State)
	}

	// Bob accepts Alice's proposal: authors a same-repo mirror edit.
	proposalRef := crossRepoProposal(t, created.Data.ID)
	out := applyProposal(bob, proposalRef)
	if !out.Success {
		t.Fatalf("applyProposal: %s (%s)", out.Error.Message, out.Error.Code)
	}
	if out.Data.Ext != "pm" {
		t.Errorf("Outcome.Ext = %q, want pm", out.Data.Ext)
	}

	// Bob's issue now resolves closed via his same-repo mirror.
	if got := pm.GetIssue(issueRef); !got.Success || got.Data.State != pm.StateClosed {
		t.Fatalf("post-accept: want closed, got success=%v state=%q", got.Success, got.Data.State)
	}
}

func TestApply_notOwner(t *testing.T) {
	setupCache(t)
	bob := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")
	alice := cloneAs(t, initBareOrigin(t), "alice", "alice@test.com")

	created := pm.CreateIssue(bob, "Bug", "", pm.CreateIssueOptions{})
	if !created.Success {
		t.Fatalf("CreateIssue: %s", created.Error.Message)
	}
	if res := pm.CloseIssue(alice, created.Data.ID); !res.Success {
		t.Fatalf("CloseIssue: %s", res.Error.Message)
	}
	proposalRef := crossRepoProposal(t, created.Data.ID)

	// Alice does not own the canonical (Bob does), so she cannot accept it.
	if out := applyProposal(alice, proposalRef); out.Success || out.Error.Code != "NOT_OWNER" {
		t.Errorf("applyProposal by non-owner: want NOT_OWNER, got success=%v code=%q", out.Success, out.Error.Code)
	}
}

// TestApplyPM_carryForward proves S3: accept applies only the proposal's delta,
// so the owner's concurrent edit (made after the proposal) survives.
func TestApplyPM_carryForward(t *testing.T) {
	setupCache(t)
	bob := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")
	alice := cloneAs(t, initBareOrigin(t), "alice", "alice@test.com")

	created := pm.CreateIssue(bob, "Bug", "", pm.CreateIssueOptions{})
	if !created.Success {
		t.Fatalf("CreateIssue: %s", created.Error.Message)
	}
	issueRef := created.Data.ID

	// Alice proposes a close based on the original (no assignee).
	if res := pm.CloseIssue(alice, issueRef); !res.Success {
		t.Fatalf("CloseIssue: %s", res.Error.Message)
	}
	proposalRef := crossRepoProposal(t, issueRef)

	// Bob concurrently assigns the issue (a same-repo edit after the proposal).
	assignees := []string{"bob"}
	if res := pm.UpdateIssue(bob, issueRef, pm.UpdateIssueOptions{Assignees: &assignees}); !res.Success {
		t.Fatalf("assign: %s", res.Error.Message)
	}

	// Git commit timestamps are second-resolution; ensure the accept mirror
	// sorts after Bob's concurrent edit so resolution picks the mirror (which
	// carries both changes), as it would in real, non-same-second usage.
	time.Sleep(1100 * time.Millisecond)

	// Bob accepts: only the state delta should apply; his assignee must survive.
	if out := applyProposal(bob, proposalRef); !out.Success {
		t.Fatalf("applyProposal: %s (%s)", out.Error.Message, out.Error.Code)
	}
	got := pm.GetIssue(issueRef)
	if !got.Success || got.Data.State != pm.StateClosed {
		t.Fatalf("want closed, got success=%v state=%q", got.Success, got.Data.State)
	}
	if len(got.Data.Assignees) != 1 || got.Data.Assignees[0] != "bob" {
		t.Errorf("concurrent assignee clobbered: got %v, want [bob]", got.Data.Assignees)
	}
}

// TestApplyRelease covers the release dispatch (a non-pm extension).
func TestApplyRelease(t *testing.T) {
	setupCache(t)
	bob := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")
	alice := cloneAs(t, initBareOrigin(t), "alice", "alice@test.com")

	created := release.CreateRelease(bob, "Release 1.0", "notes", release.CreateReleaseOptions{Tag: "v1.0.0", Version: "1.0.0"})
	if !created.Success {
		t.Fatalf("CreateRelease: %s", created.Error.Message)
	}
	relRef := created.Data.ID

	newVersion := "1.0.1"
	if res := release.EditRelease(alice, relRef, release.EditReleaseOptions{Version: &newVersion}); !res.Success {
		t.Fatalf("EditRelease: %s", res.Error.Message)
	}
	if got := release.GetSingleRelease(relRef); !got.Success || got.Data.Version != "1.0.0" {
		t.Fatalf("pre-accept: want version 1.0.0 (proposal inert), got success=%v version=%q", got.Success, got.Data.Version)
	}

	proposalRef := crossRepoProposal(t, relRef)
	if out := applyProposal(bob, proposalRef); !out.Success || out.Data.Ext != "release" {
		t.Fatalf("applyProposal: success=%v ext=%q err=%s", out.Success, out.Data.Ext, out.Error.Message)
	}
	if got := release.GetSingleRelease(relRef); !got.Success || got.Data.Version != "1.0.1" {
		t.Fatalf("post-accept: want version 1.0.1, got success=%v version=%q", got.Success, got.Data.Version)
	}
}

// TestApplyReview_contentEdit covers a non-owner PR content (retitle) proposal.
func TestApplyReview_contentEdit(t *testing.T) {
	setupCache(t)
	bob := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")
	alice := cloneAs(t, initBareOrigin(t), "alice", "alice@test.com")

	created := review.CreatePR(bob, "Old title", "", review.CreatePROptions{Base: "main", Head: "feature", AllowUnpublishedHead: true})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}
	prRef := created.Data.ID

	newTitle := "New title"
	if res := review.UpdatePR(alice, prRef, review.UpdatePROptions{Subject: &newTitle}); !res.Success {
		t.Fatalf("UpdatePR: %s", res.Error.Message)
	}
	if got := review.GetPR(prRef); !got.Success || got.Data.Subject != "Old title" {
		t.Fatalf("pre-accept: want \"Old title\" (proposal inert), got success=%v subject=%q", got.Success, got.Data.Subject)
	}

	proposalRef := crossRepoProposal(t, prRef)
	if out := applyProposal(bob, proposalRef); !out.Success || out.Data.Ext != "review" {
		t.Fatalf("applyProposal: success=%v ext=%q err=%s (%s)", out.Success, out.Data.Ext, out.Error.Message, out.Error.Code)
	}
	if got := review.GetPR(prRef); !got.Success || got.Data.Subject != "New title" {
		t.Fatalf("post-accept: want \"New title\", got success=%v subject=%q", got.Success, got.Data.Subject)
	}
}

// TestApply_attribution proves the mirror records provenance (Phase 2.5): a
// accepts= header naming the proposal, plus a GitMsg-Ref snapshot preserving the
// proposer's identity (its type is the proposal's real type, not "accept").
func TestApply_attribution(t *testing.T) {
	setupCache(t)
	bob := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")
	alice := cloneAs(t, initBareOrigin(t), "alice", "alice@test.com")

	created := pm.CreateIssue(bob, "Crash", "", pm.CreateIssueOptions{})
	if !created.Success {
		t.Fatalf("CreateIssue: %s", created.Error.Message)
	}
	issueRef := created.Data.ID

	if res := pm.CloseIssue(alice, issueRef); !res.Success {
		t.Fatalf("CloseIssue: %s", res.Error.Message)
	}
	proposalRef := crossRepoProposal(t, issueRef)
	proposal := protocol.ParseRef(proposalRef)

	out := applyProposal(bob, proposalRef)
	if !out.Success {
		t.Fatalf("applyProposal: %s (%s)", out.Error.Message, out.Error.Code)
	}

	mirror, err := cache.GetCommit(out.Data.MirrorRepoURL, out.Data.MirrorHash, out.Data.MirrorBranch)
	if err != nil {
		t.Fatalf("get mirror commit: %v", err)
	}
	msg := protocol.ParseMessage(mirror.Message)
	if msg == nil {
		t.Fatal("mirror message is not a GitMsg")
	}
	// The mirror's header carries accepts=<proposal>, the searchable projection.
	if acc := protocol.ParseRef(msg.Header.Fields["accepts"]); acc.Value != proposal.Value {
		t.Errorf("accepts= header = %q, want proposal %s#%s", msg.Header.Fields["accepts"], proposal.Repository, proposal.Value)
	}
	// A snapshot GitMsg-Ref names the proposal and preserves the proposer's
	// identity; its type is the proposal's real type (issue), never "accept".
	var attr *protocol.Ref
	for i := range msg.References {
		if r := protocol.ParseRef(msg.References[i].Ref); r.Value == proposal.Value {
			attr = &msg.References[i]
			break
		}
	}
	if attr == nil {
		t.Fatalf("mirror missing proposal snapshot ref; message:\n%s", mirror.Message)
	}
	if attr.Fields["type"] == "accept" {
		t.Error("snapshot ref type must be the proposal's real type, not \"accept\"")
	}
	if attr.Author != "alice" || attr.Email != "alice@test.com" {
		t.Errorf("attribution = %q <%s>, want proposer alice <alice@test.com>", attr.Author, attr.Email)
	}
}

// crossRepoProposal returns the ref of the cross-repo edit proposing a change to
// the canonical named by issueRef.
func crossRepoProposal(t *testing.T, issueRef string) string {
	t.Helper()
	c := protocol.ParseRef(issueRef)
	coords, err := cache.QueryLocked(func(db *sql.DB) ([3]string, error) {
		var e [3]string
		err := db.QueryRow(`SELECT edit_repo_url, edit_hash, edit_branch FROM core_commits_version
			WHERE canonical_repo_url = ? AND canonical_hash = ? AND canonical_branch = ?
			  AND edit_repo_url != canonical_repo_url LIMIT 1`,
			protocol.NormalizeURL(c.Repository), c.Value, c.Branch).Scan(&e[0], &e[1], &e[2])
		return e, err
	})
	if err != nil {
		t.Fatalf("find cross-repo proposal: %v", err)
	}
	return protocol.CreateRef(protocol.RefTypeCommit, coords[1], coords[0], coords[2])
}

func setupCache(t *testing.T) {
	t.Helper()
	cache.Reset()
	if err := cache.Open(t.TempDir()); err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(cache.Reset)
}

func initBareOrigin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := git.ExecGit(dir, []string{"init", "--bare", "--initial-branch=main"}); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	seed := t.TempDir()
	mustGit(t, seed, "init", "--initial-branch=main")
	mustGit(t, seed, "config", "user.email", "seed@test.com")
	mustGit(t, seed, "config", "user.name", "Seed")
	if _, err := git.CreateCommit(seed, git.CommitOptions{Message: "seed", AllowEmpty: true}); err != nil {
		t.Fatalf("seed commit: %v", err)
	}
	mustGit(t, seed, "remote", "add", "origin", dir)
	mustGit(t, seed, "push", "origin", "main")
	return dir
}

func cloneAs(t *testing.T, origin, name, email string) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "clone", origin, ".")
	mustGit(t, dir, "config", "user.email", email)
	mustGit(t, dir, "config", "user.name", name)
	return dir
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := git.ExecGit(dir, args); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}
