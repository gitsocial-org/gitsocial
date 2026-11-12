// view_notifications_test.go - The notifications view reports a failed load
package tuiviews

import (
	"errors"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestNotificationsLoadFailureSurfaces checks that a failed notifications load reaches the status bar.
func TestNotificationsLoadFailureSurfaces(t *testing.T) {
	view := NewNotificationsView(t.TempDir(), nil, nil, nil, nil)
	view.loading = true
	state := &tuicore.State{}
	view.Update(NotificationsLoadedMsg{Err: errors.New("get notifications: cache closed")}, state)
	if state.Message != "get notifications: cache closed" {
		t.Errorf("Message = %q, want the error text", state.Message)
	}
	if state.MessageType != tuicore.MessageTypeError {
		t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
	}
	if view.loading {
		t.Error("a failed load left the view loading; the panel would keep saying Loading")
	}
}

// TestNotificationsConditionKeepsItsState checks that a click, "read all" and the mark keys leave a condition row unread.
func TestNotificationsConditionKeepsItsState(t *testing.T) {
	marked := 0
	mark := func(string, string) error { marked++; return nil }
	view := NewNotificationsView(t.TempDir(), nil, mark, mark, nil)
	repo, hash := "https://example.com/owner/repo", "abc123456789"
	now := time.Now()
	view.Update(NotificationsLoadedMsg{Result: tuicore.NotificationsResult{
		Items: []tuicore.DisplayItem{
			tuicore.NewItem("condition", "review", "head-advanced", now, nil),
			tuicore.NewItem("event", "review", "review-requested", now, nil),
		},
		Meta: []tuicore.NotificationMeta{
			{RepoURL: repo, Hash: hash, Type: "head-advanced", Condition: true},
			{RepoURL: repo, Hash: hash, Type: "review-requested"},
		},
	}}, &tuicore.State{})
	if cmd := view.MarkNotificationRead(); cmd != nil {
		t.Error("the read key on a condition row returned a command")
	}
	if cmd := view.MarkNotificationUnread(); cmd != nil {
		t.Error("the unread key on a condition row returned a command")
	}
	view.MarkReadByHash(repo, hash)
	if view.meta[0].IsRead || !view.meta[1].IsRead {
		t.Errorf("after a click: condition read = %v, event read = %v, want false and true", view.meta[0].IsRead, view.meta[1].IsRead)
	}
	view.meta[1].IsRead = false
	view.Update(NotificationsAllMarkedReadMsg{}, &tuicore.State{})
	if view.meta[0].IsRead || !view.meta[1].IsRead || view.countUnread() != 1 {
		t.Errorf("after read all: condition read = %v, event read = %v, unread = %d, want false, true and 1", view.meta[0].IsRead, view.meta[1].IsRead, view.countUnread())
	}
	if marked != 0 {
		t.Errorf("the mark function ran %d times, want 0", marked)
	}
}
