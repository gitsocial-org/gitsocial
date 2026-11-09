// desktop.go - The desktop messages a thin client sends for the new unread notifications of a fetch.
package client

import (
	"errors"
	"os"
	"strconv"

	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/notifications/desktop"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

const desktopEnv = "GITSOCIAL_DESKTOP_NOTIFICATIONS"

// DesktopEnabled reads the variable first, then notifications.desktop, then the default of on.
func DesktopEnabled() bool {
	if enabled, err := strconv.ParseBool(os.Getenv(desktopEnv)); err == nil {
		return enabled
	}
	value, _ := settings.NewManager().Resolve("notifications.desktop")
	return value != "false"
}

// NotifyNew sends the desktop messages of new items with their click targets; WatchUnread gives items only when the setting is on.
func NotifyNew(workdir, cacheDir string, items []notifications.Notification) {
	notifyNew(workdir, cacheDir, items, desktop.Send)
}

// notifyNew sends the messages of new items through send and logs a failure.
func notifyNew(workdir, cacheDir string, items []notifications.Notification, send func([]notifications.DesktopMessage) error) {
	if len(items) == 0 {
		return
	}
	if err := send(messages(workdir, cacheDir, items)); err != nil {
		log.Debug("desktop notification failed", "error", err)
	}
}

// WatchUnread snapshots the unread set when desktop messages are on and returns a function that gives the items unread since.
func WatchUnread(workdir string) func() []notifications.Notification {
	if !DesktopEnabled() {
		return func() []notifications.Notification { return nil }
	}
	before, beforeErr := notifications.GetAllStrict(workdir, notifications.Filter{UnreadOnly: true})
	return func() []notifications.Notification {
		after, afterErr := notifications.GetAllStrict(workdir, notifications.Filter{UnreadOnly: true})
		if err := errors.Join(beforeErr, afterErr); err != nil {
			log.Debug("unread notifications snapshot failed", "error", err)
			return nil
		}
		return notifications.NewUnread(before, after)
	}
}
