// desktop_text.go - The text of the desktop messages of the events that a delivery claimed.
package notifications

import (
	"fmt"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// desktopTitle is the title of each desktop message.
const desktopTitle = "GitSocial"

// desktopMessageCap is the most new items that get one message each; more get one summary.
const desktopMessageCap = 3

// DesktopMessage is one message of a fetch, and the JSON document of the webhook: the text, the click target and the item it names; a summary names no item.
type DesktopMessage struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"url"`
	Type   string `json:"type,omitempty"`
	Repo   string `json:"repo,omitempty"`
	Hash   string `json:"hash,omitempty"`
	Socket string `json:"-"`
	Ref    string `json:"-"`
}

// DesktopMessages returns one message per new item, or one summary over the cap; itemURL gives the click target of an item and summaryURL that of the summary.
func DesktopMessages(items []Notification, itemURL func(Notification) string, summaryURL string) []DesktopMessage {
	if len(items) > desktopMessageCap {
		return []DesktopMessage{{Title: desktopTitle, Body: fmt.Sprintf("%d new notifications", len(items)), URL: summaryURL}}
	}
	messages := make([]DesktopMessage, 0, len(items))
	for _, n := range items {
		messages = append(messages, DesktopMessage{Title: desktopTitle, Body: desktopBody(n), URL: itemURL(n), Type: n.Type, Repo: n.RepoURL, Hash: n.Hash, Ref: desktopRef(n)})
	}
	return messages
}

// desktopRef returns the commit ref of the item of a notification, or "" for a follow.
func desktopRef(n Notification) string {
	if n.Type == "follow" {
		return ""
	}
	return protocol.CreateRef(protocol.RefTypeCommit, n.Hash, n.RepoURL, n.Branch)
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
	case "activity":
		return "has new activity"
	}
	return notificationType
}
