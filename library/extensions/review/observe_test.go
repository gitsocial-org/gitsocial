// observe_test.go - Tests for branch observations and the head/base
// notifications driven by them.
package review

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/notifications"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

func TestRefreshOpenPRBranches_DetectsAdvance(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	// Create a real `feature` commit so we can advance origin/feature past it.
	if _, err := git.CreateCommitOnBranch(dir, "feature", "v1"); err != nil {
		t.Fatalf("CreateCommitOnBranch v1: %v", err)
	}
	v1Tip, err := git.ReadRef(dir, "feature")
	if err != nil {
		t.Fatalf("ReadRef v1: %v", err)
	}

	// Plant origin/feature at the same tip as the local branch (matching
	// the post-push state — observation should report no advance yet).
	if err := git.WriteRef(dir, "refs/remotes/origin/feature", v1Tip); err != nil {
		t.Fatalf("WriteRef origin/feature v1: %v", err)
	}

	created := CreatePR(dir, "subj", "", CreatePROptions{Base: "main", Head: "feature"})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}

	// Advance origin/feature past the stored head-tip (simulates a teammate
	// pushing new commits while we hold an open PR).
	v2Hash, err := git.CreateCommitTree(dir, "v2 commit\n", v1Tip)
	if err != nil {
		t.Fatalf("CreateCommitTree v2: %v", err)
	}
	if err := git.WriteRef(dir, "refs/remotes/origin/feature", v2Hash); err != nil {
		t.Fatalf("WriteRef origin/feature v2: %v", err)
	}

	if err := RefreshOpenPRBranches(dir); err != nil {
		t.Fatalf("RefreshOpenPRBranches: %v", err)
	}

	obs, err := GetBranchObservation(reviewTestRepoURL, "feature")
	if err != nil {
		t.Fatalf("GetBranchObservation: %v", err)
	}
	if !obs.Exists {
		t.Errorf("Exists = false; expected true")
	}
	if obs.Tip == created.Data.HeadTip {
		t.Errorf("observation Tip %q matches stored — expected divergence", obs.Tip)
	}
}

func TestRefreshOpenPRBranches_DetectsDeletion(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)
	if _, err := git.CreateCommitOnBranch(dir, "doomed", "v1"); err != nil {
		t.Fatalf("CreateCommitOnBranch: %v", err)
	}
	tip, err := git.ReadRef(dir, "doomed")
	if err != nil {
		t.Fatalf("ReadRef: %v", err)
	}
	// Stage origin/doomed so CreatePR's tip resolution succeeds, then
	// delete it to simulate a remote branch removal.
	if err := git.WriteRef(dir, "refs/remotes/origin/doomed", tip); err != nil {
		t.Fatalf("WriteRef origin/doomed: %v", err)
	}
	created := CreatePR(dir, "subj", "", CreatePROptions{Base: "main", Head: "doomed"})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}
	if _, err := git.ExecGit(dir, []string{"update-ref", "-d", "refs/remotes/origin/doomed"}); err != nil {
		t.Fatalf("update-ref -d origin/doomed: %v", err)
	}

	if err := RefreshOpenPRBranches(dir); err != nil {
		t.Fatalf("RefreshOpenPRBranches: %v", err)
	}

	obs, err := GetBranchObservation(reviewTestRepoURL, "doomed")
	if err != nil {
		t.Fatalf("GetBranchObservation: %v", err)
	}
	if obs.Exists {
		t.Errorf("Exists = true; expected false (origin/doomed was deleted)")
	}
}

func TestNotifications_HeadAdvancedFiresForAuthor(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)
	if _, err := git.CreateCommitOnBranch(dir, "feature-adv", "v1"); err != nil {
		t.Fatalf("CreateCommitOnBranch: %v", err)
	}
	v1Tip, _ := git.ReadRef(dir, "feature-adv")
	if err := git.WriteRef(dir, "refs/remotes/origin/feature-adv", v1Tip); err != nil {
		t.Fatalf("WriteRef: %v", err)
	}
	created := CreatePR(dir, "subj", "", CreatePROptions{Base: "main", Head: "feature-adv"})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}

	// Advance the remote and refresh observations.
	v2Hash, err := git.CreateCommitTree(dir, "v2\n", v1Tip)
	if err != nil {
		t.Fatalf("CreateCommitTree: %v", err)
	}
	if err := git.WriteRef(dir, "refs/remotes/origin/feature-adv", v2Hash); err != nil {
		t.Fatalf("WriteRef v2: %v", err)
	}
	if err := RefreshOpenPRBranches(dir); err != nil {
		t.Fatalf("RefreshOpenPRBranches: %v", err)
	}

	prov := &reviewNotificationProvider{}
	notifs, err := prov.GetNotifications(dir, notifications.Filter{})
	if err != nil {
		t.Fatalf("GetNotifications: %v", err)
	}
	found := false
	for _, n := range notifs {
		if n.Type == "head-advanced" && n.Hash == protocol.ParseRef(created.Data.ID).Value {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("head-advanced notification not found; got types: %v", notifTypes(notifs))
	}
}

func TestNotifications_HeadAdvancedClearsAfterUpdate(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)
	if _, err := git.CreateCommitOnBranch(dir, "feature-clear", "v1"); err != nil {
		t.Fatalf("CreateCommitOnBranch: %v", err)
	}
	v1Tip, _ := git.ReadRef(dir, "feature-clear")
	if err := git.WriteRef(dir, "refs/remotes/origin/feature-clear", v1Tip); err != nil {
		t.Fatalf("WriteRef: %v", err)
	}
	created := CreatePR(dir, "subj", "", CreatePROptions{Base: "main", Head: "feature-clear"})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}
	v2Hash, err := git.CreateCommitTree(dir, "v2\n", v1Tip)
	if err != nil {
		t.Fatalf("CreateCommitTree: %v", err)
	}
	if err := git.WriteRef(dir, "refs/remotes/origin/feature-clear", v2Hash); err != nil {
		t.Fatalf("WriteRef v2: %v", err)
	}
	if err := RefreshOpenPRBranches(dir); err != nil {
		t.Fatalf("RefreshOpenPRBranches: %v", err)
	}

	// Update PR tips so the stored head-tip catches up to origin.
	upd := UpdatePRTips(dir, created.Data.ID)
	if !upd.Success {
		t.Fatalf("UpdatePRTips: %s", upd.Error.Message)
	}

	prov := &reviewNotificationProvider{}
	notifs, err := prov.GetNotifications(dir, notifications.Filter{})
	if err != nil {
		t.Fatalf("GetNotifications: %v", err)
	}
	for _, n := range notifs {
		if n.Type == "head-advanced" {
			t.Errorf("head-advanced still present after UpdatePRTips: %+v", n)
		}
	}
}

func notifTypes(ns []notifications.Notification) []string {
	types := make([]string, len(ns))
	for i, n := range ns {
		types[i] = n.Type
	}
	return types
}
