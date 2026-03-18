// notifications.go - Notifications view for comments, reposts, and mentions
package tuicore

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/log"
)

// NotificationsLoadedMsg is sent when notifications are loaded.
type NotificationsLoadedMsg struct {
	Result NotificationsResult
	Err    error
}

// NotificationMarkedReadMsg is sent when a notification is marked as read.
type NotificationMarkedReadMsg struct {
	Index       int
	Err         error
	UnreadCount int
}

// NotificationMarkedUnreadMsg is sent when a notification is marked as unread.
type NotificationMarkedUnreadMsg struct {
	Index       int
	Err         error
	UnreadCount int
}

// NotificationsAllMarkedReadMsg is sent when all notifications are marked as read.
type NotificationsAllMarkedReadMsg struct{}

// NotificationsAllMarkedUnreadMsg is sent when all notifications are marked as unread.
type NotificationsAllMarkedUnreadMsg struct {
	UnreadCount int
}

// NotificationsView displays notifications.
type NotificationsView struct {
	meta            []NotificationMeta
	cardlist        *CardList
	loading         bool
	unreadOnly      bool
	workdir         string
	getFunc         GetNotificationsFunc
	markReadFn      MarkReadFunc
	markUnreadFn    MarkUnreadFunc
	markAllReadFn   MarkAllReadFunc
	markAllUnreadFn MarkAllUnreadFunc
	resolveFunc     ResolveItemFunc
	restoreIndex    int       // cursor position to restore after refresh (-1 = none)
	loadedFetchTime time.Time // LastFetchTime when data was loaded; reload when it changes
}

// NotificationsViewOption configures the notifications view.
type NotificationsViewOption func(*NotificationsView)

// WithBulkMarkFuncs sets bulk mark-all functions for efficient batch operations.
func WithBulkMarkFuncs(readFn MarkAllReadFunc, unreadFn MarkAllUnreadFunc) NotificationsViewOption {
	return func(v *NotificationsView) {
		v.markAllReadFn = readFn
		v.markAllUnreadFn = unreadFn
	}
}

// Bindings returns keybindings for the notifications view.
func (v *NotificationsView) Bindings() []Binding {
	return []Binding{
		{Key: "m", Label: "read", Contexts: []Context{Notifications},
			Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.MarkNotificationRead()
			}},
		{Key: "M", Label: "read all", Contexts: []Context{Notifications},
			Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.MarkAllNotificationsRead()
			}},
		{Key: "u", Label: "unread", Contexts: []Context{Notifications},
			Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.MarkNotificationUnread()
			}},
		{Key: "U", Label: "unread all", Contexts: []Context{Notifications},
			Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.MarkAllNotificationsUnread()
			}},
		{Key: "r", Label: "refresh", Contexts: []Context{Notifications},
			Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
				return false, nil // Handled by view
			}},
		{Key: "F", Label: "filter", Contexts: []Context{Notifications},
			Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.ToggleNotificationFilter()
			}},
	}
}

// NewNotificationsView creates a new notifications view with injected dependencies.
func NewNotificationsView(workdir string, getFn GetNotificationsFunc, markReadFn MarkReadFunc, markUnreadFn MarkUnreadFunc, resolveFn ResolveItemFunc, opts ...NotificationsViewOption) *NotificationsView {
	v := &NotificationsView{
		workdir:      workdir,
		getFunc:      getFn,
		markReadFn:   markReadFn,
		markUnreadFn: markUnreadFn,
		resolveFunc:  resolveFn,
		restoreIndex: -1,
	}
	for _, opt := range opts {
		opt(v)
	}
	v.cardlist = NewCardList(nil)
	if resolveFn != nil {
		v.cardlist.SetItemResolver(func(itemID string) (DisplayItem, bool) {
			return resolveFn(workdir, itemID)
		})
	}
	return v
}

// SetSize sets the view dimensions.
func (v *NotificationsView) SetSize(width, height int) {
	v.cardlist.SetSize(width, height-3) // -3 for footer
}

// Activate loads notifications.
func (v *NotificationsView) Activate(state *State) tea.Cmd {
	if state.DetailSource != nil && state.DetailSource.Path == "/notifications" {
		v.restoreIndex = state.DetailSource.Index
	} else {
		v.restoreIndex = -1
	}
	// Use cached data if we have it and no fetch has occurred since last load.
	// Data only changes on fetch (new items) or mark read/unread (applied locally).
	if len(v.meta) > 0 && v.loadedFetchTime.Equal(state.LastFetchTime) {
		if v.restoreIndex >= 0 {
			v.cardlist.SetSelected(v.restoreIndex)
			v.restoreIndex = -1
		}
		return nil
	}
	v.loading = true
	return v.loadNotifications()
}

// loadNotifications loads notifications using the injected function.
func (v *NotificationsView) loadNotifications() tea.Cmd {
	workdir := v.workdir
	unreadOnly := v.unreadOnly
	getFn := v.getFunc
	return func() tea.Msg {
		if getFn == nil {
			return NotificationsLoadedMsg{Err: nil}
		}
		result, err := getFn(workdir, unreadOnly)
		if err != nil {
			return NotificationsLoadedMsg{Err: err}
		}
		return NotificationsLoadedMsg{Result: result}
	}
}

// Update handles input for the notifications view.
func (v *NotificationsView) Update(msg tea.Msg, state *State) tea.Cmd {
	switch msg.(type) {
	case tea.KeyPressMsg, tea.MouseMsg:
		consumed, activate, link := v.cardlist.Update(msg)
		if link != nil {
			return func() tea.Msg { return NavigateMsg{Location: *link, Action: NavPush} }
		}
		if activate {
			return v.navigateToSelected()
		}
		if consumed {
			return ConsumedCmd
		}
		if key, ok := msg.(tea.KeyPressMsg); ok {
			return v.handleKey(key)
		}
	default:
		switch msg := msg.(type) {
		case NotificationsLoadedMsg:
			v.handleLoaded(msg, state)
		case NotificationMarkedReadMsg:
			v.handleMarkedRead(msg, state)
		case NotificationMarkedUnreadMsg:
			v.handleMarkedUnread(msg, state)
		case NotificationsAllMarkedReadMsg:
			v.handleAllMarkedRead()
		case NotificationsAllMarkedUnreadMsg:
			v.handleAllMarkedUnread()
		}
	}
	return nil
}

// navigateToSelected navigates to the selected notification.
func (v *NotificationsView) navigateToSelected() tea.Cmd {
	idx := v.cardlist.Selected()
	if idx < 0 || idx >= len(v.meta) {
		return nil
	}
	m := v.meta[idx]
	if v.markReadFn != nil && !m.IsRead {
		if err := v.markReadFn(m.RepoURL, m.Hash, m.Branch); err != nil {
			log.Warn("failed to mark notification as read", "error", err)
		}
		v.meta[idx].IsRead = true
		v.cardlist.SetDimmed(idx, true)
	}
	if m.Type == "follow" {
		return func() tea.Msg {
			return NavigateMsg{
				Location: LocRepository(m.ActorRepo, m.Branch),
				Action:   NavPush,
			}
		}
	}
	item, ok := v.cardlist.SelectedItem()
	if !ok {
		return nil
	}
	items := v.cardlist.Items()
	loc := GetNavTarget(item)
	return func() tea.Msg {
		return NavigateMsg{
			Location:    loc,
			Action:      NavPush,
			SourcePath:  "/notifications",
			SourceIndex: v.cardlist.Selected(),
			SourceTotal: len(items),
		}
	}
}

// handleKey processes view-specific keyboard input.
func (v *NotificationsView) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "r":
		return v.loadNotifications()
	}
	return nil
}

// handleLoaded processes the loaded notifications.
func (v *NotificationsView) handleLoaded(msg NotificationsLoadedMsg, state *State) {
	v.loading = false
	if msg.Err != nil {
		return
	}
	v.meta = msg.Result.Meta
	v.cardlist.SetItems(msg.Result.Items)
	v.loadedFetchTime = state.LastFetchTime
	if v.restoreIndex >= 0 {
		v.cardlist.SetSelected(v.restoreIndex)
		v.restoreIndex = -1
	}
}

// markRead marks the selected notification as read.
func (v *NotificationsView) markRead() tea.Cmd {
	if len(v.meta) == 0 || v.markReadFn == nil {
		return nil
	}
	idx := v.cardlist.Selected()
	if idx >= len(v.meta) {
		return nil
	}
	m := v.meta[idx]
	markFn := v.markReadFn
	newCount := v.countUnread()
	if !m.IsRead {
		newCount--
	}
	return func() tea.Msg {
		err := markFn(m.RepoURL, m.Hash, m.Branch)
		return NotificationMarkedReadMsg{Index: idx, Err: err, UnreadCount: newCount}
	}
}

// handleMarkedRead updates state after marking read.
func (v *NotificationsView) handleMarkedRead(msg NotificationMarkedReadMsg, state *State) {
	if msg.Err != nil {
		state.SetMessage("Failed to mark as read: "+msg.Err.Error(), MessageTypeError)
		return
	}
	if msg.Index < len(v.meta) {
		v.meta[msg.Index].IsRead = true
		v.cardlist.SetDimmed(msg.Index, true)
	}
}

// handleMarkedUnread updates state after marking unread.
func (v *NotificationsView) handleMarkedUnread(msg NotificationMarkedUnreadMsg, state *State) {
	if msg.Err != nil {
		state.SetMessage("Failed to mark as unread: "+msg.Err.Error(), MessageTypeError)
		return
	}
	if msg.Index < len(v.meta) {
		v.meta[msg.Index].IsRead = false
		v.cardlist.SetDimmed(msg.Index, false)
	}
}

// handleAllMarkedRead updates state after marking all read.
func (v *NotificationsView) handleAllMarkedRead() {
	for i := range v.meta {
		v.meta[i].IsRead = true
		v.cardlist.SetDimmed(i, true)
	}
}

// handleAllMarkedUnread updates state after marking all unread.
func (v *NotificationsView) handleAllMarkedUnread() {
	for i := range v.meta {
		v.meta[i].IsRead = false
		v.cardlist.SetDimmed(i, false)
	}
}

// countUnread returns the number of unread notifications.
func (v *NotificationsView) countUnread() int {
	count := 0
	for _, m := range v.meta {
		if !m.IsRead {
			count++
		}
	}
	return count
}

// markAllRead marks all notifications as read.
func (v *NotificationsView) markAllRead() tea.Cmd {
	if v.markAllReadFn != nil {
		workdir := v.workdir
		bulkFn := v.markAllReadFn
		return func() tea.Msg {
			if err := bulkFn(workdir); err != nil {
				log.Warn("failed to mark all notifications as read", "error", err)
			}
			return NotificationsAllMarkedReadMsg{}
		}
	}
	meta := v.meta
	markFn := v.markReadFn
	if markFn == nil {
		return nil
	}
	return func() tea.Msg {
		for _, m := range meta {
			if !m.IsRead {
				if err := markFn(m.RepoURL, m.Hash, m.Branch); err != nil {
					log.Warn("failed to mark notification as read", "repo", m.RepoURL, "error", err)
				}
			}
		}
		return NotificationsAllMarkedReadMsg{}
	}
}

// markUnread marks the selected notification as unread.
func (v *NotificationsView) markUnread() tea.Cmd {
	if len(v.meta) == 0 || v.markUnreadFn == nil {
		return nil
	}
	idx := v.cardlist.Selected()
	if idx < 0 || idx >= len(v.meta) {
		return nil
	}
	m := v.meta[idx]
	markFn := v.markUnreadFn
	newCount := v.countUnread()
	if m.IsRead {
		newCount++
	}
	return func() tea.Msg {
		err := markFn(m.RepoURL, m.Hash, m.Branch)
		return NotificationMarkedUnreadMsg{Index: idx, Err: err, UnreadCount: newCount}
	}
}

// markAllUnread marks all notifications as unread.
func (v *NotificationsView) markAllUnread() tea.Cmd {
	total := len(v.meta)
	if v.markAllUnreadFn != nil {
		workdir := v.workdir
		bulkFn := v.markAllUnreadFn
		return func() tea.Msg {
			if err := bulkFn(workdir); err != nil {
				log.Warn("failed to mark all notifications as unread", "error", err)
			}
			return NotificationsAllMarkedUnreadMsg{UnreadCount: total}
		}
	}
	meta := v.meta
	markFn := v.markUnreadFn
	if markFn == nil {
		return nil
	}
	return func() tea.Msg {
		for _, m := range meta {
			if m.IsRead {
				if err := markFn(m.RepoURL, m.Hash, m.Branch); err != nil {
					log.Warn("failed to mark notification as unread", "repo", m.RepoURL, "error", err)
				}
			}
		}
		return NotificationsAllMarkedUnreadMsg{UnreadCount: total}
	}
}

// MarkNotificationRead marks the selected notification as read.
func (v *NotificationsView) MarkNotificationRead() tea.Cmd {
	return v.markRead()
}

// MarkAllNotificationsRead marks all notifications as read.
func (v *NotificationsView) MarkAllNotificationsRead() tea.Cmd {
	return v.markAllRead()
}

// MarkNotificationUnread marks the selected notification as unread.
func (v *NotificationsView) MarkNotificationUnread() tea.Cmd {
	return v.markUnread()
}

// MarkAllNotificationsUnread marks all notifications as unread.
func (v *NotificationsView) MarkAllNotificationsUnread() tea.Cmd {
	return v.markAllUnread()
}

// ToggleNotificationFilter toggles between all and unread notifications.
func (v *NotificationsView) ToggleNotificationFilter() tea.Cmd {
	v.unreadOnly = !v.unreadOnly
	return v.loadNotifications()
}

// Render renders the notifications view.
func (v *NotificationsView) Render(state *State) string {
	wrapper := NewViewWrapper(state)

	var content string
	if v.loading {
		content = Dim.Render("Loading...")
	} else if len(v.cardlist.Items()) == 0 {
		content = Dim.Render("No notifications")
	} else {
		content = v.cardlist.View()
	}

	footer := RenderFooter(state.Registry, Notifications, wrapper.ContentWidth(), nil)
	return wrapper.Render(content, footer)
}

// IsInputActive returns false since notifications view doesn't handle text input.
func (v *NotificationsView) IsInputActive() bool {
	return false
}

// HeaderInfo returns position and total for the header.
func (v *NotificationsView) HeaderInfo() (position, total int) {
	items := v.cardlist.Items()
	if len(items) == 0 {
		return 0, 0
	}
	return v.cardlist.Selected() + 1, len(items)
}

// GetItemAt returns the post ID at the given index.
func (v *NotificationsView) GetItemAt(index int) (string, bool) {
	items := v.cardlist.Items()
	if index >= 0 && index < len(items) {
		return items[index].ItemID(), true
	}
	return "", false
}

// GetItemCount returns the total number of items.
func (v *NotificationsView) GetItemCount() int {
	return len(v.cardlist.Items())
}

// GetDisplayItemAt returns the full DisplayItem at the given index.
func (v *NotificationsView) GetDisplayItemAt(index int) (DisplayItem, bool) {
	items := v.cardlist.Items()
	if index >= 0 && index < len(items) {
		return items[index], true
	}
	return nil, false
}
