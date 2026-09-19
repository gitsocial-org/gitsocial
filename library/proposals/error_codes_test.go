// error_codes_test.go - Error codes the accept and decline paths return at the Result boundary
package proposals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

// blockDeclinesNamespace puts a regular file where the decline markers' ref directory belongs.
func blockDeclinesNamespace(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, ".git", filepath.FromSlash(strings.TrimSuffix(gitmsg.DeclinesRefPrefix, "/")))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("occupied\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// openIssueProposal files an issue on bob and has alice close it from her own repo, returning the issue and proposal refs.
func openIssueProposal(t *testing.T, bob, alice string) (string, string) {
	t.Helper()
	created := pm.CreateIssue(bob, "Crash on startup", "Segfaults on empty config", pm.CreateIssueOptions{})
	if !created.Success {
		t.Fatalf("CreateIssue: %s", created.Error.Message)
	}
	if res := pm.CloseIssue(alice, created.Data.ID); !res.Success {
		t.Fatalf("CloseIssue: %s", res.Error.Message)
	}
	return created.Data.ID, crossRepoProposal(t, created.Data.ID)
}

// TestApply_lookupFailed asserts LOOKUP_FAILED when the version lookup cannot run.
func TestApply_lookupFailed(t *testing.T) {
	setupCache(t)
	bob := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")
	alice := cloneAs(t, initBareOrigin(t), "alice", "alice@test.com")

	_, proposalRef := openIssueProposal(t, bob, alice)
	cache.Reset()

	if out := applyProposal(bob, proposalRef); out.Success || out.Error.Code != "LOOKUP_FAILED" {
		t.Errorf("applyProposal() over a closed cache = %+v, want LOOKUP_FAILED", out)
	}
	if out := Decline(bob, proposalRef); out.Success || out.Error.Code != "LOOKUP_FAILED" {
		t.Errorf("Decline() over a closed cache = %+v, want LOOKUP_FAILED", out)
	}
}

// TestApply_notAProposal asserts NOT_A_PROPOSAL for a canonical and for a same-repo edit.
func TestApply_notAProposal(t *testing.T) {
	setupCache(t)
	bob := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")

	created := pm.CreateIssue(bob, "Crash", "", pm.CreateIssueOptions{})
	if !created.Success {
		t.Fatalf("CreateIssue: %s", created.Error.Message)
	}
	issueRef := created.Data.ID

	if out := applyProposal(bob, issueRef); out.Success || out.Error.Code != "NOT_A_PROPOSAL" {
		t.Errorf("applyProposal() on a canonical = %+v, want NOT_A_PROPOSAL", out)
	}
	if out := Decline(bob, issueRef); out.Success || out.Error.Code != "NOT_A_PROPOSAL" {
		t.Errorf("Decline() on a canonical = %+v, want NOT_A_PROPOSAL", out)
	}

	// Bob's own close is a same-repo edit, which needs no acceptance.
	if res := pm.CloseIssue(bob, issueRef); !res.Success {
		t.Fatalf("CloseIssue: %s", res.Error.Message)
	}
	latest, err := cache.GetLatestVersion(protocol.NormalizeURL(protocol.ParseRef(issueRef).Repository),
		protocol.ParseRef(issueRef).Value, protocol.ParseRef(issueRef).Branch)
	if err != nil || !latest.HasEdits {
		t.Fatalf("GetLatestVersion: %+v err=%v", latest, err)
	}
	sameRepoEdit := protocol.CreateRef(protocol.RefTypeCommit, latest.Hash, latest.RepoURL, latest.Branch)
	if out := applyProposal(bob, sameRepoEdit); out.Success || out.Error.Code != "NOT_A_PROPOSAL" {
		t.Errorf("applyProposal() on a same-repo edit = %+v, want NOT_A_PROPOSAL", out)
	}
	if out := Decline(bob, sameRepoEdit); out.Success || out.Error.Code != "NOT_A_PROPOSAL" {
		t.Errorf("Decline() on a same-repo edit = %+v, want NOT_A_PROPOSAL", out)
	}
}

// TestApply_canonicalRetracted asserts CANONICAL_RETRACTED when the owner retracted the item.
func TestApply_canonicalRetracted(t *testing.T) {
	setupCache(t)
	bob := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")
	alice := cloneAs(t, initBareOrigin(t), "alice", "alice@test.com")

	issueRef, proposalRef := openIssueProposal(t, bob, alice)
	if res := pm.RetractIssue(bob, issueRef); !res.Success {
		t.Fatalf("RetractIssue: %s", res.Error.Message)
	}
	// RetractIssue only writes the commit; the workspace sync caches it.
	if _, err := fetch.SyncWorkspaceLocal(bob, []fetch.WorkspaceSyncFunc{pm.SyncWorkspaceBatch}); err != nil {
		t.Fatalf("SyncWorkspaceLocal: %v", err)
	}

	out := applyProposal(bob, proposalRef)
	if out.Success || out.Error.Code != "CANONICAL_RETRACTED" {
		t.Errorf("applyProposal() on a retracted canonical = %+v, want CANONICAL_RETRACTED", out)
	}
}

// TestApply_noDelta asserts NO_DELTA when a proposal changes nothing the owner can apply.
func TestApply_noDelta(t *testing.T) {
	setupCache(t)
	bob := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")
	alice := cloneAs(t, initBareOrigin(t), "alice", "alice@test.com")

	t.Run("issue re-stated with its own subject", func(t *testing.T) {
		created := pm.CreateIssue(bob, "Crash", "body", pm.CreateIssueOptions{})
		if !created.Success {
			t.Fatalf("CreateIssue: %s", created.Error.Message)
		}
		same := "Crash"
		if res := pm.UpdateIssue(alice, created.Data.ID, pm.UpdateIssueOptions{Subject: &same}); !res.Success {
			t.Fatalf("UpdateIssue: %s", res.Error.Message)
		}
		out := applyProposal(bob, crossRepoProposal(t, created.Data.ID))
		if out.Success || out.Error.Code != "NO_DELTA" {
			t.Errorf("applyProposal() on an issue proposal with no delta = %+v, want NO_DELTA", out)
		}
	})

	t.Run("pull request re-stated with its own title", func(t *testing.T) {
		created := review.CreatePR(bob, "Old title", "", review.CreatePROptions{Base: "main", Head: "feature", AllowUnpublishedHead: true})
		if !created.Success {
			t.Fatalf("CreatePR: %s", created.Error.Message)
		}
		same := "Old title"
		if res := review.UpdatePR(alice, created.Data.ID, review.UpdatePROptions{Subject: &same}); !res.Success {
			t.Fatalf("UpdatePR: %s", res.Error.Message)
		}
		out := applyProposal(bob, crossRepoProposal(t, created.Data.ID))
		if out.Success || out.Error.Code != "NO_DELTA" {
			t.Errorf("applyProposal() on a PR proposal with no delta = %+v, want NO_DELTA", out)
		}
	})
}

// TestApply_unsupportedType asserts UNSUPPORTED_TYPE for a review proposal that is not a pull request.
func TestApply_unsupportedType(t *testing.T) {
	setupCache(t)
	bob := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")
	alice := cloneAs(t, initBareOrigin(t), "alice", "alice@test.com")

	pr := review.CreatePR(bob, "Fix the parser", "", review.CreatePROptions{Base: "main", Head: "feature", AllowUnpublishedHead: true})
	if !pr.Success {
		t.Fatalf("CreatePR: %s", pr.Error.Message)
	}
	feedback := review.CreateFeedback(bob, "This branch is wrong", review.CreateFeedbackOptions{
		PullRequest: pr.Data.ID,
		ReviewState: review.ReviewStateChangesRequested,
	})
	if !feedback.Success {
		t.Fatalf("CreateFeedback: %s", feedback.Error.Message)
	}

	revised := "This branch reads better now"
	if res := review.UpdateFeedback(alice, feedback.Data.ID, review.UpdateFeedbackOptions{Content: &revised}); !res.Success {
		t.Fatalf("UpdateFeedback: %s", res.Error.Message)
	}

	out := applyProposal(bob, crossRepoProposal(t, feedback.Data.ID))
	if out.Success || out.Error.Code != "UNSUPPORTED_TYPE" {
		t.Errorf("applyProposal() on a feedback proposal = %+v, want UNSUPPORTED_TYPE", out)
	}
}

// TestApply_unsupportedExt asserts UNSUPPORTED_EXT when the proposal edits a social item.
func TestApply_unsupportedExt(t *testing.T) {
	setupCache(t)
	bob := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")
	alice := cloneAs(t, initBareOrigin(t), "alice", "alice@test.com")

	post := social.CreatePost(bob, "Shipping today", nil)
	if !post.Success {
		t.Fatalf("CreatePost: %s", post.Error.Message)
	}
	// social.EditPost refuses a cross-repo edit, so the proposal arrives as a commit synced in, the way a fork's edit does.
	proposal := protocol.FormatMessage("Shipping tomorrow", protocol.Header{
		Ext: "social", V: "0.1.0",
		Fields: map[string]string{"type": "post", "edits": post.Data.ID},
	}, nil)
	if _, err := git.CreateCommitOnBranch(alice, gitmsg.GetExtBranch(alice, "social"), proposal); err != nil {
		t.Fatalf("CreateCommitOnBranch: %v", err)
	}
	if _, err := fetch.SyncWorkspaceLocal(alice, []fetch.WorkspaceSyncFunc{social.SyncWorkspaceBatch}); err != nil {
		t.Fatalf("SyncWorkspaceLocal: %v", err)
	}

	out := applyProposal(bob, crossRepoProposal(t, post.Data.ID))
	if out.Success || out.Error.Code != "UNSUPPORTED_EXT" {
		t.Errorf("applyProposal() on a social proposal = %+v, want UNSUPPORTED_EXT", out)
	}
}

// TestDecline_declineFailed asserts DECLINE_FAILED when the marker ref cannot be written.
func TestDecline_declineFailed(t *testing.T) {
	setupCache(t)
	bob := cloneAs(t, initBareOrigin(t), "bob", "bob@test.com")
	alice := cloneAs(t, initBareOrigin(t), "alice", "alice@test.com")
	testutil.SkipWithoutFilesRefBackend(t, bob)

	issueRef, proposalRef := openIssueProposal(t, bob, alice)
	blockDeclinesNamespace(t, bob)

	out := Decline(bob, proposalRef)
	if out.Success || out.Error.Code != "DECLINE_FAILED" {
		t.Errorf("Decline() with the marker namespace blocked = %+v, want DECLINE_FAILED", out)
	}
	if got := pm.GetIssue(issueRef); !got.Data.HasProposedEdits {
		t.Error("the proposal stopped being pending after the failed decline")
	}
}
