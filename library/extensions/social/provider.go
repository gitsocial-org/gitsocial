// provider.go - Social notification provider for the core notifications system
package social

import (
	"github.com/gitsocial-org/gitsocial/core/notifications"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

type notificationProvider struct{}

func init() {
	notifications.RegisterProvider("social", &notificationProvider{})
}

// GetNotifications returns social notifications converted to core Notification type.
func (p *notificationProvider) GetNotifications(workdir string, filter notifications.Filter) ([]notifications.Notification, error) {
	sf := NotificationFilter{
		UnreadOnly: filter.UnreadOnly,
		Limit:      filter.Limit,
	}
	items, err := GetNotifications(workdir, sf)
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
		result = append(result, cn)
	}
	return result, nil
}

// GetUnreadCount returns the social unread notification count.
func (p *notificationProvider) GetUnreadCount(workdir string) (int, error) {
	return GetUnreadCount(workdir)
}
