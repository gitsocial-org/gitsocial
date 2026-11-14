// provider.go - Social notification provider for the core notifications system
package social

import (
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

type notificationProvider struct{}

// init registers the social notification provider.
func init() {
	notifications.RegisterProvider("social", &notificationProvider{})
}

// GetNotifications returns social notifications converted to core Notification type.
func (p *notificationProvider) GetNotifications(workdir string, filter notifications.Filter) ([]notifications.Notification, error) {
	sf := notificationFilter{
		UnreadOnly: filter.UnreadOnly,
		Limit:      filter.Limit,
	}
	items, err := getNotifications(workdir, sf)
	if err != nil {
		return nil, err
	}
	result := make([]notifications.Notification, 0, len(items))
	for _, n := range items {
		cn := notifications.Notification{
			Type:      string(n.Type),
			Source:    "social",
			Actor:     notifications.Actor{Name: n.Actor.Name, Email: n.Actor.Email},
			ActorRepo: n.ActorRepo,
			Timestamp: n.Timestamp,
			IsRead:    n.IsRead,
			Item:      n,
		}
		if n.Type == NotificationTypeFollow {
			cn.RepoURL = n.ActorRepo
			cn.Hash = "follow"
			cn.Branch = ""
		} else if n.Item != nil {
			cn.RepoURL = n.Item.Repository
			cn.Hash = protocol.ParseRef(n.Item.ID).Value
			cn.Branch = n.Item.Branch
		}
		result = append(result, withText(cn, n))
	}
	return result, nil
}

// withText returns the notification with the subject, the excerpt, the phrase and the glyph of its type; a follow has the repository as its actor.
func withText(cn notifications.Notification, n Notification) notifications.Notification {
	if n.Type == NotificationTypeFollow {
		cn.Actor = notifications.Actor{Name: protocol.GetFullDisplayName(n.ActorRepo)}
		cn.Glyph = protocol.TypeGlyph("list", "")
		return cn
	}
	if n.Item == nil {
		return cn
	}
	content := notifications.FirstLine(n.Item.Content)
	switch n.Type {
	case NotificationTypeActivity:
		cn.Glyph = activityGlyph(*n.Item)
		kind := activityKind(*n.Item)
		cn.Phrase, cn.Subject = notifications.NewItemPhrase(kind), content
		if n.Item.Action != "" {
			cn.Phrase, cn.Subject = notifications.ActionPhrase(n.Item.Action, kind), n.Item.ActionSubject
			if kind == "feedback" {
				cn.Excerpt = content
			}
		}
	case notificationTypeRepost:
		cn.Glyph, cn.Subject = protocol.TypeGlyph(string(n.Type), ""), n.OriginalSubject
	default:
		cn.Glyph, cn.Subject, cn.Excerpt = protocol.TypeGlyph(string(n.Type), ""), n.OriginalSubject, content
	}
	return cn
}
