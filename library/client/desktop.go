// desktop.go - The desktop messages and the webhook posts a thin client sends for the new unread notifications of a fetch.
package client

import (
	"errors"
	"net/http"
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

// webhookURL reads notifications.webhook; an empty value turns the webhook off.
func webhookURL() string {
	value, _ := settings.NewManager().Resolve("notifications.webhook")
	return value
}

// NotifyNew sends the messages of new items with their click targets: to the desktop when the setting is on, and to the webhook when it has a URL.
func NotifyNew(workdir, cacheDir string, items []notifications.Notification) {
	notifyNew(workdir, cacheDir, items, desktop.Send, &http.Client{Timeout: webhookTimeout})
}

// notifyNew sends the messages of new items through send and posts each with httpClient, and logs a failure.
func notifyNew(workdir, cacheDir string, items []notifications.Notification, send func([]notifications.DesktopMessage) error, httpClient *http.Client) {
	if len(items) == 0 {
		return
	}
	all := messages(workdir, cacheDir, items)
	if DesktopEnabled() {
		if err := send(all); err != nil {
			log.Debug("desktop notification failed", "error", err)
		}
	}
	if url := webhookURL(); url != "" {
		postWebhook(httpClient, url, all, webhookRetryDelay)
	}
}

// WatchUnread snapshots the unread set when desktop messages or the webhook are on and returns a function that gives the items unread since.
func WatchUnread(workdir string) func() []notifications.Notification {
	if !DesktopEnabled() && webhookURL() == "" {
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
