// desktop.go - The delivery of the unread events that no process sent: the desktop messages and the webhook posts of a thin client.
package client

import (
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

// Deliver claims the unread events that no process sent and sends them: to the desktop when onDesktop is true and the setting is on, and to the webhook when it has a URL; it returns the unread count. A TUI gives its click socket, other callers "".
func Deliver(workdir, cacheDir, socket string, onDesktop bool) int {
	return deliver(workdir, cacheDir, socket, onDesktop, desktop.Send, &http.Client{Timeout: webhookTimeout})
}

// deliver sends the claimed events through send and posts each with httpClient, and logs a failure; with both channels off it claims nothing.
func deliver(workdir, cacheDir, socket string, onDesktop bool, send func([]notifications.DesktopMessage) error, httpClient *http.Client) int {
	unread, _ := notifications.GetAll(workdir, notifications.Filter{UnreadOnly: true}) // GetAll returns no error
	url := webhookURL()
	if len(unread) == 0 || (!DesktopEnabled() && url == "") {
		return len(unread)
	}
	// The claim is before the send: a send before it lets two processes send one event.
	claimed, err := notifications.Claim(unread)
	if err != nil {
		log.Debug("notification claim failed", "error", err)
	}
	sendClaimed(workdir, cacheDir, socket, onDesktop, claimed, url, send, httpClient)
	return len(unread)
}

// sendClaimed sends the messages of the claimed events with their click targets to the desktop and to the webhook.
func sendClaimed(workdir, cacheDir, socket string, onDesktop bool, claimed []notifications.Notification, url string, send func([]notifications.DesktopMessage) error, httpClient *http.Client) {
	if len(claimed) == 0 {
		return
	}
	all := messages(workdir, cacheDir, claimed)
	if onDesktop && DesktopEnabled() {
		if err := send(desktopCopy(all, socket)); err != nil {
			log.Debug("desktop notification failed", "error", err)
		}
	}
	if url != "" {
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
