// multiclone_test.go - End-to-end collaboration test: two workdirs share an
// origin bare repo, the test simulates the scenarios from PR-GAPS.md
// (head-advance, branch-deletion). This is the regression guard the rest of
// the workdir-PR fix set is in service of.
package review

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/notifications"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// TestMultiCloneCollab walks through the canonical "two devs share an origin"
// flow that previously masked silent failures.
func TestMultiCloneCollab(t *testing.T) {
	setupTestDB(t)

	origin := initBareOrigin(t)
	alice := cloneAs(t, origin, "alice", "alice@test.com")
	bob := cloneAs(t, origin, "bob", "bob@test.com")

	// Alice creates a feature branch, pushes it, opens a PR.
	if _, err := git.ExecGit(alice, []string{"checkout", "-b", "feature"}); err != nil {
		t.Fatalf("alice checkout: %v", err)
	}
	if _, err := git.CreateCommit(alice, git.CommitOptions{Message: "alice v1", AllowEmpty: true}); err != nil {
		t.Fatalf("alice v1: %v", err)
	}
	v1Tip, _ := git.ReadRef(alice, "feature")
	if _, err := git.ExecGit(alice, []string{"push", "origin", "feature"}); err != nil {
		t.Fatalf("alice push: %v", err)
	}
	if _, err := git.ExecGit(alice, []string{"checkout", "main"}); err != nil {
		t.Fatalf("alice checkout main: %v", err)
	}
	// Sync alice's remote tracking ref so the PR records origin's tip.
	if _, err := git.ExecGit(alice, []string{"fetch", "origin"}); err != nil {
		t.Fatalf("alice fetch: %v", err)
	}

	created := CreatePR(alice, "Add feature", "", CreatePROptions{Base: "main", Head: "feature"})
	if !created.Success {
		t.Fatalf("CreatePR: %s", created.Error.Message)
	}
	if created.Data.HeadTip == "" {
		t.Fatal("PR HeadTip is empty — symmetric tip resolution should have populated it")
	}
	if !strings.HasPrefix(v1Tip, created.Data.HeadTip) && !strings.HasPrefix(created.Data.HeadTip, v1Tip) {
		t.Errorf("PR HeadTip %q does not match origin/feature tip %q", created.Data.HeadTip, v1Tip)
	}

	// Bob pulls the feature branch and pushes new commits.
	if _, err := git.ExecGit(bob, []string{"fetch", "origin"}); err != nil {
		t.Fatalf("bob fetch: %v", err)
	}
	if _, err := git.ExecGit(bob, []string{"checkout", "-b", "feature", "origin/feature"}); err != nil {
		t.Fatalf("bob checkout feature: %v", err)
	}
	if _, err := git.CreateCommit(bob, git.CommitOptions{Message: "bob v2", AllowEmpty: true}); err != nil {
		t.Fatalf("bob v2: %v", err)
	}
	v2Tip, _ := git.ReadRef(bob, "feature")
	if _, err := git.ExecGit(bob, []string{"push", "origin", "feature"}); err != nil {
		t.Fatalf("bob push: %v", err)
	}
	if v1Tip == v2Tip {
		t.Fatal("v2 tip equals v1 tip — bob's push didn't advance feature")
	}

	// Alice fetches and observes the divergence.
	if _, err := git.ExecGit(alice, []string{"fetch", "origin"}); err != nil {
		t.Fatalf("alice fetch after bob push: %v", err)
	}
	if err := RefreshOpenPRBranches(alice); err != nil {
		t.Fatalf("RefreshOpenPRBranches: %v", err)
	}

	prov := &reviewNotificationProvider{}
	notifs, err := prov.GetNotifications(alice, notifications.Filter{})
	if err != nil {
		t.Fatalf("GetNotifications: %v", err)
	}
	if !hasNotifType(notifs, "head-advanced") {
		t.Errorf("head-advanced notification missing; got %v", notifTypes(notifs))
	}

	// Alice runs `pr update`, the PR's stored head-tip catches up, the
	// notification clears.
	upd := UpdatePRTips(alice, created.Data.ID)
	if !upd.Success {
		t.Fatalf("UpdatePRTips: %s", upd.Error.Message)
	}
	if upd.Data.HeadTip == created.Data.HeadTip {
		t.Errorf("UpdatePRTips did not advance HeadTip: still %q", upd.Data.HeadTip)
	}
	if !strings.HasPrefix(v2Tip, upd.Data.HeadTip) && !strings.HasPrefix(upd.Data.HeadTip, v2Tip) {
		t.Errorf("UpdatePRTips HeadTip %q does not match bob's pushed tip %q", upd.Data.HeadTip, v2Tip)
	}
	if err := RefreshOpenPRBranches(alice); err != nil {
		t.Fatalf("RefreshOpenPRBranches after update: %v", err)
	}
	notifs, _ = prov.GetNotifications(alice, notifications.Filter{})
	if hasNotifType(notifs, "head-advanced") {
		t.Errorf("head-advanced still fires after UpdatePRTips: %v", notifTypes(notifs))
	}

	// Branch-deletion path: bob deletes feature from origin.
	if _, err := git.ExecGit(bob, []string{"push", "origin", "--delete", "feature"}); err != nil {
		t.Fatalf("bob delete feature: %v", err)
	}
	// Prune so refs/remotes/origin/feature is removed locally.
	if _, err := git.ExecGit(alice, []string{"fetch", "origin", "--prune"}); err != nil {
		t.Fatalf("alice fetch --prune: %v", err)
	}
	if err := RefreshOpenPRBranches(alice); err != nil {
		t.Fatalf("RefreshOpenPRBranches after delete: %v", err)
	}
	notifs, _ = prov.GetNotifications(alice, notifications.Filter{})
	if !hasNotifType(notifs, "head-deleted") {
		t.Errorf("head-deleted notification missing; got %v", notifTypes(notifs))
	}

	// MergePR should now refuse instead of silently flipping state, since
	// alice's local refs/heads/feature is also gone after she didn't keep a
	// local branch (she only worked on main).
	if _, err := git.ExecGit(alice, []string{"branch", "-D", "feature"}); err != nil {
		// May not exist locally — ignore.
		_ = err
	}
	merge := MergePR(alice, upd.Data.ID, MergeStrategyFF)
	if merge.Success {
		t.Fatal("MergePR should refuse when head branch is gone")
	}
	if merge.Error.Code != "HEAD_NOT_FOUND" {
		t.Errorf("Error.Code = %q, want HEAD_NOT_FOUND", merge.Error.Code)
	}
	pr := GetPR(upd.Data.ID)
	if !pr.Success {
		t.Fatalf("GetPR after failed merge: %s", pr.Error.Message)
	}
	if pr.Data.State == PRStateMerged {
		t.Error("PR was flipped to merged despite missing head branch")
	}
}

func initBareOrigin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := git.ExecGit(dir, []string{"init", "--bare", "--initial-branch=main"}); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	// Seed the bare repo with an initial commit on main so clones land in a
	// sane state. Use a temporary working clone, commit, push, discard.
	seed := t.TempDir()
	if _, err := git.ExecGit(seed, []string{"init", "--initial-branch=main"}); err != nil {
		t.Fatalf("seed init: %v", err)
	}
	if _, err := git.ExecGit(seed, []string{"config", "user.email", "seed@test.com"}); err != nil {
		t.Fatalf("seed email: %v", err)
	}
	if _, err := git.ExecGit(seed, []string{"config", "user.name", "Seed"}); err != nil {
		t.Fatalf("seed name: %v", err)
	}
	if _, err := git.CreateCommit(seed, git.CommitOptions{Message: "seed", AllowEmpty: true}); err != nil {
		t.Fatalf("seed commit: %v", err)
	}
	if _, err := git.ExecGit(seed, []string{"remote", "add", "origin", dir}); err != nil {
		t.Fatalf("seed remote: %v", err)
	}
	if _, err := git.ExecGit(seed, []string{"push", "origin", "main"}); err != nil {
		t.Fatalf("seed push: %v", err)
	}
	return dir
}

func cloneAs(t *testing.T, origin, name, email string) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := git.ExecGit(dir, []string{"clone", origin, "."}); err != nil {
		t.Fatalf("%s clone: %v", name, err)
	}
	if _, err := git.ExecGit(dir, []string{"config", "user.email", email}); err != nil {
		t.Fatalf("%s email: %v", name, err)
	}
	if _, err := git.ExecGit(dir, []string{"config", "user.name", name}); err != nil {
		t.Fatalf("%s name: %v", name, err)
	}
	return dir
}

func hasNotifType(ns []notifications.Notification, typeName string) bool {
	for _, n := range ns {
		if n.Type == typeName {
			return true
		}
	}
	return false
}

// notifTypes is provided by observe_test.go in the same package.
//
// Local helpers above compose with `setupTestDB` from sync_test.go and
// `parseRef` / `protocol.*` for ref handling.
var _ = protocol.ParseRef
