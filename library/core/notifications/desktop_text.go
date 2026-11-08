// desktop_text.go - The new unread notifications of a fetch and the text of their desktop messages.
package notifications

import (
	"fmt"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// DesktopTitle is the title of each desktop message.
const DesktopTitle = "GitSocial"

// desktopMessageCap is the most new items that get one message each; more get one summary.
const desktopMessageCap = 3

// NewUnread returns the items of after whose (repo, hash) is not in before, each key once.
func NewUnread(before, after []Notification) []Notification {
	seen := make(map[string]bool, len(before)+len(after))
	for _, n := range before {
		seen[notificationKey(n)] = true
	}
	var fresh []Notification
	for _, n := range after {
		key := notificationKey(n)
		if seen[key] {
			continue
		}
		seen[key] = true
		fresh = append(fresh, n)
	}
	return fresh
}

// notificationKey joins the repository and the hash; the branch is not in it because the live row moves between branches.
func notificationKey(n Notification) string {
	return n.RepoURL + "\x00" + n.Hash
}

// DesktopBodies returns one message body per new item, or one summary over the cap.
func DesktopBodies(items []Notification) []string {
	if len(items) > desktopMessageCap {
		return []string{fmt.Sprintf("%d new notifications", len(items))}
	}
	bodies := make([]string, 0, len(items))
	for _, n := range items {
		bodies = append(bodies, desktopBody(n))
	}
	return bodies
}

// desktopBody returns the actor and the verb of the type, or a sentence for a type with no actor, then the short repository name.
func desktopBody(n Notification) string {
	text := desktopSentence(n.Type)
	if text == "" {
		text = desktopVerb(n.Type)
		if n.Actor.Name != "" {
			text = n.Actor.Name + " " + text
		}
	}
	if n.RepoURL != "" {
		text += " · " + protocol.GetFullDisplayName(n.RepoURL)
	}
	return text
}

// desktopSentence returns the full text of a type that has no actor, or "".
func desktopSentence(notificationType string) string {
	switch notificationType {
	case "head-advanced":
		return "The head branch of a pull request advanced"
	case "base-advanced":
		return "The base branch of a pull request advanced"
	case "head-deleted":
		return "The head branch of a pull request is not on its remote"
	case "base-deleted":
		return "The base branch of a pull request is not on its remote"
	case "branch-diverged":
		return "A local gitmsg branch diverged from origin"
	}
	return ""
}

// desktopVerb returns the phrase that follows the actor for a notification type, or the type name.
func desktopVerb(notificationType string) string {
	switch notificationType {
	case "mention":
		return "mentioned you"
	case "reference":
		return "referenced your item"
	case "edit":
		return "edited your item"
	case "comment":
		return "commented"
	case "repost":
		return "reposted your post"
	case "quote":
		return "quoted your post"
	case "follow":
		return "followed you"
	case "issue-assigned":
		return "assigned you an issue"
	case "issue-closed":
		return "closed your issue"
	case "issue-reopened":
		return "reopened your issue"
	case "fork-issue":
		return "opened an issue on a fork"
	case "fork-pr":
		return "opened a pull request"
	case "review-requested":
		return "requested your review"
	case "feedback":
		return "gave feedback on a pull request"
	case "approved":
		return "approved a pull request"
	case "changes-requested":
		return "requested changes"
	case "pr-merged":
		return "merged your pull request"
	case "pr-closed":
		return "closed your pull request"
	case "pr-ready":
		return "marked a pull request ready"
	case "new-release":
		return "published a release"
	case "memo-comment":
		return "commented on your memo"
	case "inherited-policy":
		return "added a critical memo"
	}
	return notificationType
}
