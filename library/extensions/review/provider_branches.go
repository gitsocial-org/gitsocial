// provider_branches.go - The head and base notifications driven by review_branch_observations
package review

import (
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// getBranchStateNotifications reports every open pull request whose branch moved or vanished.
func getBranchStateNotifications(workdir, workspaceURL, userEmail string, forkURLs []string) []notifications.Notification {
	if workspaceURL == "" {
		return nil
	}
	branch := gitmsg.GetExtBranch(workdir, "review")
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

// branchStateForPR compares one pull request's stored tips to their observations.
func branchStateForPR(workspaceURL string, pr PullRequest) []notifications.Notification {
	headRepo, headBranch := refRepoAndBranch(protocol.ParseRef(pr.Head), workspaceURL)
	baseRepo, baseBranch := refRepoAndBranch(protocol.ParseRef(pr.Base), workspaceURL)
	headObs, _ := getBranchObservation(headRepo, headBranch)
	baseObs, _ := getBranchObservation(baseRepo, baseBranch)
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

// refRepoAndBranch projects a parsed ref into the observation table's key.
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

// prStakeholder returns true when userEmail authored the PR or is one of its reviewers.
func prStakeholder(pr PullRequest, userEmail string) bool {
	return pr.Author.Email == userEmail || hasEmail(pr.Reviewers, userEmail)
}

// branchStateNotif assembles one branch state notification.
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
