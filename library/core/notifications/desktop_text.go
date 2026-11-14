// desktop_text.go - The title and the body of the message of a notification, and the summary of a delivery.
package notifications

import (
	"fmt"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/text"
)

// The limits of the parts of a message, in code points.
const (
	actorLimit   = 30
	phraseLimit  = 60
	lineLimit    = 100
	contextLimit = 40
)

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
		return []DesktopMessage{summaryMessage(items, summaryURL)}
	}
	messages := make([]DesktopMessage, 0, len(items))
	for _, n := range items {
		title, body := MessageText(n)
		messages = append(messages, DesktopMessage{Title: title, Body: body, URL: itemURL(n), Type: n.Type, Repo: n.RepoURL, Hash: n.Hash, Ref: desktopRef(n)})
	}
	return messages
}

// summaryMessage returns the count of the items as the title, and the titles of the first three and the count of the rest as the body.
func summaryMessage(items []Notification, url string) DesktopMessage {
	lines := make([]string, 0, desktopMessageCap+1)
	for _, n := range items[:desktopMessageCap] {
		title, _ := MessageText(n)
		lines = append(lines, title)
	}
	lines = append(lines, fmt.Sprintf("and %d more", len(items)-desktopMessageCap))
	return DesktopMessage{Title: fmt.Sprintf("%d new notifications", len(items)), Body: strings.Join(lines, "\n"), URL: url}
}

// desktopRef returns the commit ref of the item of a notification, or "" for a follow.
func desktopRef(n Notification) string {
	if n.Type == "follow" {
		return ""
	}
	return protocol.CreateRef(protocol.RefTypeCommit, n.Hash, n.RepoURL, n.Branch)
}

// MessageText returns the title and the body of a notification; it is the only place that makes remote text safe for a message. notificationText in gs-render.js is its copy.
func MessageText(n Notification) (title, body string) {
	title = text.SafeLine(n.Phrase, phraseLimit)
	if title == "" {
		title = typeWords(n.Type)
	}
	if _, sentence := typeSentences[n.Type]; !sentence {
		if actor := text.SafeLine(n.Actor.Name, actorLimit); actor != "" {
			title = actor + " " + title
		}
	}
	if n.Glyph != "" {
		title = n.Glyph + " " + title
	}
	if n.Type == "follow" {
		return title, ""
	}
	repo := ""
	if n.RepoURL != "" {
		repo = text.SafeLine(protocol.GetFullDisplayName(n.RepoURL), phraseLimit)
	}
	excerpt := text.SafeLine(n.Excerpt, lineLimit)
	if excerpt == "" {
		return title, joinPresent("\n", text.SafeLine(n.Subject, lineLimit), repo)
	}
	return title, joinPresent("\n", excerpt, joinPresent(" · ", text.SafeLine(n.Subject, contextLimit), repo))
}

// joinPresent joins the parts that are not empty with sep.
func joinPresent(sep string, parts ...string) string {
	present := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			present = append(present, p)
		}
	}
	return strings.Join(present, sep)
}

// typeWords returns the sentence of a type with no actor, the verb that follows the actor, or the type name.
func typeWords(notificationType string) string {
	if sentence, ok := typeSentences[notificationType]; ok {
		return sentence
	}
	if verb, ok := typeVerbs[notificationType]; ok {
		return verb
	}
	return notificationType
}

// typeSentences is the full title of each type that names no actor.
var typeSentences = map[string]string{
	"issue-assigned":   "An issue is assigned to you",
	"review-requested": "Your review is requested",
	"head-advanced":    "The head branch of a pull request advanced",
	"base-advanced":    "The base branch of a pull request advanced",
	"head-deleted":     "The head branch of a pull request is not on its remote",
	"base-deleted":     "The base branch of a pull request is not on its remote",
	"branch-diverged":  "A branch diverged from its remote",
}

// typeVerbs is the phrase that follows the actor for each other type.
var typeVerbs = map[string]string{
	"mention":           "mentioned you",
	"reference":         "referenced your item",
	"edit":              "edited your item",
	"comment":           "commented",
	"repost":            "reposted your post",
	"quote":             "quoted your post",
	"follow":            "added your repository to a list",
	"issue-closed":      "closed an issue of yours",
	"issue-reopened":    "reopened an issue of yours",
	"fork-issue":        "opened an issue on a fork",
	"fork-pr":           "opened a pull request",
	"feedback":          "commented on a pull request",
	"approved":          "approved a pull request",
	"changes-requested": "requested changes",
	"pr-merged":         "merged your pull request",
	"pr-closed":         "closed your pull request",
	"pr-ready":          "marked a pull request ready",
	"new-release":       "published a release",
	"memo-comment":      "commented on your memo",
	"inherited-policy":  "added a critical memo",
	"activity":          "added an item",
}

// newItemPhrases is the phrase of an activity event for a new item, by the kind of the item.
var newItemPhrases = map[string]string{
	"post":         "posted",
	"comment":      "commented",
	"repost":       "reposted",
	"quote":        "quoted",
	"issue":        "opened an issue",
	"pull-request": "opened a pull request",
	"feedback":     "commented on a pull request",
	"milestone":    "added a milestone",
	"sprint":       "added a sprint",
	"release":      "published a release",
	"memo":         "added a memo",
	"commit":       "committed",
}

// kindNouns names the kind of an item that a timeline action acts on; a review acts on a pull request.
var kindNouns = map[string]string{
	"issue":        "an issue",
	"pull-request": "a pull request",
	"feedback":     "a pull request",
	"milestone":    "a milestone",
	"sprint":       "a sprint",
}

// NewItemPhrase returns the phrase of an activity event for a new item of a kind; NEW_ITEM_PHRASES in gs-render.js is its copy.
func NewItemPhrase(kind string) string {
	if phrase, ok := newItemPhrases[kind]; ok {
		return phrase
	}
	return typeVerbs["activity"]
}

// ActionPhrase returns the phrase of an activity event for a timeline action on an item of a kind.
func ActionPhrase(action, kind string) string {
	noun := kindNouns[kind]
	if noun == "" {
		noun = "an item"
	}
	switch action {
	case "ready":
		return "marked " + noun + " ready"
	case "changes-requested":
		return "requested changes on " + noun
	case "review-requested":
		return "requested a review"
	}
	return action + " " + noun
}

// FirstLine returns the first line of the content of a commit message, without its GitMsg header.
func FirstLine(message string) string {
	subject, _ := protocol.SplitSubjectBody(protocol.ExtractCleanContent(message))
	return subject
}

// commitGlyph returns the glyph of the item of a commit: its header type in its state, a post for a gitmsg branch with no header, and none for a code commit.
func commitGlyph(message, branch string) string {
	msg := protocol.ParseMessage(message)
	if msg == nil {
		if strings.HasPrefix(branch, gitmsgBranchPrefix) {
			return protocol.TypeGlyph("post", "")
		}
		return ""
	}
	itemType := msg.Header.Fields["type"]
	if itemType == "" {
		itemType = defaultItemType(msg.Header.Ext)
	}
	return protocol.TypeGlyph(itemType, msg.Header.Fields["state"])
}

// defaultItemType returns the item type of a header of an extension that names none.
func defaultItemType(ext string) string {
	switch ext {
	case "pm":
		return "issue"
	case "review":
		return "pull-request"
	case "release", "memo":
		return ext
	}
	return "post"
}
