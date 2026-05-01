// provider_branches.go - Notifications driven by review_branch_observations:
// "head/base advanced" and "head/base deleted" for open PRs in the
// workspace and any registered fork.
//
// These are computed from current state (observed remote tip vs stored PR
// tip), so they self-clear once the PR is updated or closed — no read
// tracking needed. Audience is the PR author and any reviewers.
package review

import (
	"strings"

	"github.com/gitsocial-org/gitsocial/core/notifications"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// getBranchStateNotifications surfaces head-advanced / head-deleted /
// base-advanced / base-deleted notifications for open PRs whose head or
// base branches have drifted from the live remote, or no longer exist on
// the remote. The observation table is keyed by (repo_url, branch) so
// workspace and fork PRs use the same lookup.
func getBranchStateNotifications(workspaceURL, userEmail string, forkURLs []string) []notifications.Notification {
	if workspaceURL == "" {
		return nil
	}
	branch := "gitmsg/review"
	prRes := GetPullRequestsWithForks(workspaceURL, branch, forkURLs, []string{"open"}, "", 0)
	if !prRes.Success {
		return nil
	}
	var result []notifications.Notification
	for _, pr := range prRes.Data {
		if !prStakeholder(pr, userEmail) {
			continue
		}
		notifs := branchStateForPR(workspaceURL, pr)
		result = append(result, notifs...)
	}
	return result
}

// branchStateForPR computes the (zero or more) branch-state notifications
// for a single PR by comparing each side's stored tip to the cached
// observation. Order: deletion before advance; head before base.
func branchStateForPR(workspaceURL string, pr PullRequest) []notifications.Notification {
	headRepo, headBranch := refRepoAndBranch(protocol.ParseRef(pr.Head), workspaceURL)
	baseRepo, baseBranch := refRepoAndBranch(protocol.ParseRef(pr.Base), workspaceURL)
	headObs, _ := GetBranchObservation(headRepo, headBranch)
	baseObs, _ := GetBranchObservation(baseRepo, baseBranch)
	prRef := protocol.CreateRef(protocol.RefTypeCommit, protocol.ParseRef(pr.ID).Value, pr.Repository, pr.Branch)
	if headObs != nil && !headObs.Exists {
		return []notifications.Notification{branchStateNotif(prRef, pr,
			"head-deleted", pr.Head, pr.HeadTip, "", false)}
	}
	if baseObs != nil && !baseObs.Exists {
		return []notifications.Notification{branchStateNotif(prRef, pr,
			"base-deleted", pr.Base, pr.BaseTip, "", false)}
	}
	var result []notifications.Notification
	if headObs != nil && headObs.Tip != "" && headObs.Tip != pr.HeadTip {
		result = append(result, branchStateNotif(prRef, pr,
			"head-advanced", pr.Head, pr.HeadTip, headObs.Tip, true))
	}
	if baseObs != nil && baseObs.Tip != "" && baseObs.Tip != pr.BaseTip {
		result = append(result, branchStateNotif(prRef, pr,
			"base-advanced", pr.Base, pr.BaseTip, baseObs.Tip, true))
	}
	return result
}

// refRepoAndBranch projects a parsed PR ref into the (repo_url, branch)
// pair used as the observation table key, applying the workspace-shorthand
// fallback when the ref's Repository field is empty.
func refRepoAndBranch(parsed protocol.ParsedRef, workspaceURL string) (string, string) {
	if parsed.Type != protocol.RefTypeBranch {
		return "", ""
	}
	repo := parsed.Repository
	if repo == "" {
		repo = workspaceURL
	}
	return repo, parsed.Value
}

// prStakeholder returns true when userEmail authored the PR or appears in
// the comma-separated reviewers list. Mirrors the per-PR filter previously
// embedded in the SQL.
func prStakeholder(pr PullRequest, userEmail string) bool {
	if pr.Author.Email == userEmail {
		return true
	}
	for _, r := range pr.Reviewers {
		if strings.TrimSpace(r) == userEmail {
			return true
		}
	}
	return false
}

// branchStateNotif assembles a notifications.Notification for a single
// branch-state event.
func branchStateNotif(prRef string, pr PullRequest, notifType, branchRef,
	storedTip, observedTip string, includeContent bool) notifications.Notification {
	prHash := protocol.ParseRef(pr.ID).Value
	rn := ReviewNotification{
		ID:         prRef,
		Type:       notifType,
		RepoURL:    pr.Repository,
		Hash:       prHash,
		Branch:     pr.Branch,
		PRSubject:  pr.Subject,
		ActorName:  pr.Author.Name,
		ActorEmail: pr.Author.Email,
		Timestamp:  pr.Timestamp,
	}
	if includeContent {
		rn.Content = branchRef + ": " + storedTip + " → " + observedTip
	} else {
		rn.Content = branchRef
	}
	return notifications.Notification{
		RepoURL:   pr.Repository,
		Hash:      prHash,
		Branch:    pr.Branch,
		Type:      notifType,
		Source:    "review",
		Item:      rn,
		Actor:     notifications.Actor{Name: pr.Author.Name, Email: pr.Author.Email},
		ActorRepo: pr.Repository,
		Timestamp: pr.Timestamp,
		IsRead:    false,
	}
}
