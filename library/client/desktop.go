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

// NotifyNew sends the messages of new items with their click targets: to the desktop when the setting is on, and to the webhook when it has a URL; a TUI gives its click socket, other callers "".
func NotifyNew(workdir, cacheDir, socket string, items []notifications.Notification) {
	notifyNew(workdir, cacheDir, socket, items, desktop.Send, &http.Client{Timeout: webhookTimeout})
}

// notifyNew sends the messages of new items through send and posts each with httpClient, and logs a failure.
func notifyNew(workdir, cacheDir, socket string, items []notifications.Notification, send func([]notifications.DesktopMessage) error, httpClient *http.Client) {
	if len(items) == 0 {
		return
	}
	all := messages(workdir, cacheDir, items)
	if DesktopEnabled() {
		if err := send(desktopCopy(all, socket)); err != nil {
			log.Debug("desktop notification failed", "error", err)
		}
	}
	if url := webhookURL(); url != "" {
		postWebhook(httpClient, url, all, webhookRetryDelay)
	}
}

// desktopCopy returns the messages for the desktop sender: with a socket, each names it and has no URL, so a click goes to that TUI.
func desktopCopy(all []notifications.DesktopMessage, socket string) []notifications.DesktopMessage {
	if socket == "" {
		return all
	}
	out := make([]notifications.DesktopMessage, 0, len(all))
	for _, m := range all {
		m.Socket, m.URL = socket, ""
		out = append(out, m)
	}
	return out
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
