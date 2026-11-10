// util_click.go - Opens the item of a clicked desktop message and marks its notification read.
package tuisocial

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/library/tui/tuiviews"
)

// clickMaxAge is the age after which a click changes nothing in the TUI.
const clickMaxAge = 2 * time.Second

// clickResolvedMsg carries the location of a clicked item and the repository and hash of the notification it marked read, if any.
type clickResolvedMsg struct {
	location tuicore.Location
	repoURL  string
	hash     string
}

// handleClick opens the item of a click, unless the click is old or an input or a choice dialog is open.
func handleClick(msg tuicore.ClickMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	state := ctx.Host().State()
	sh, ok := ctx.Host().(socialHost)
	if !ok || time.Since(msg.At) > clickMaxAge || sh.IsInputActive() || state.ChoicePrompt != "" {
		return true, nil
	}
	if msg.Ref == "" {
		return true, navigateCmd(tuicore.LocNotifications)
	}
	showEmail := state.ShowEmailOnCards
	getFn := makeGetNotificationsFunc(state.UserEmail, func() bool { return showEmail })
	workdir, ref := ctx.Workdir(), msg.Ref
	return true, func() tea.Msg { return resolveClick(workdir, ref, getFn) }
}

// resolveClick finds the notification of a ref, whatever its branch, marks it read and returns its location; a ref with no notification opens as a ref.
func resolveClick(workdir, ref string, getFn tuicore.GetNotificationsFunc) tea.Msg {
	parsed := protocol.ParseRef(ref)
	result, err := getFn(workdir, false)
	if err != nil {
		log.Warn("load notifications for a click", "error", err)
		return clickResolvedMsg{location: tuicore.LocRef(ref)}
	}
	for i, meta := range result.Meta {
		if meta.Type == "follow" || parsed.Value == "" || protocol.NormalizeURL(meta.RepoURL) != parsed.Repository || !strings.HasPrefix(strings.ToLower(meta.Hash), parsed.Value) {
			continue
		}
		if !meta.IsRead {
			if err := notifications.MarkAsRead(meta.RepoURL, meta.Hash, meta.Branch); err != nil {
				log.Warn("mark a clicked notification read", "error", err)
			}
		}
		return clickResolvedMsg{location: tuiviews.NotificationTarget(meta, result.Items[i]), repoURL: meta.RepoURL, hash: meta.Hash}
	}
	return clickResolvedMsg{location: tuicore.LocRef(ref)}
}

// handleClickResolved marks the row of a clicked notification read in the feed, reloads the unread count and opens the location.
func handleClickResolved(msg clickResolvedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.hash == "" {
		return true, navigateCmd(msg.location)
	}
	if sh, ok := ctx.Host().(socialHost); ok {
		if feed, ok := sh.ViewAt("/notifications").(*tuiviews.NotificationsView); ok {
			feed.MarkReadByHash(msg.repoURL, msg.hash)
		}
	}
	return true, tea.Batch(ctx.LoadUnreadCount(), navigateCmd(msg.location))
}

// navigateCmd opens a location with no source list, so Back returns to the view before it.
func navigateCmd(loc tuicore.Location) tea.Cmd {
	return func() tea.Msg { return tuicore.NavigateMsg{Location: loc, Action: tuicore.NavPush} }
}
