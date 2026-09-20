// util_register_test.go - PM message handlers hand a failed save back to the form
package tuipm

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// stubContext is the AppContext the message handlers run against in tests; it records the status message.
type stubContext struct {
	state   *tuicore.State
	message string
	msgType tuicore.MessageType
}

func (c *stubContext) Workdir() string                       { return "" }
func (c *stubContext) CacheDir() string                      { return "" }
func (c *stubContext) IsFetching() bool                      { return false }
func (c *stubContext) SetFetching(bool)                      {}
func (c *stubContext) IsPushing() bool                       { return false }
func (c *stubContext) SetPushing(bool)                       {}
func (c *stubContext) Router() *tuicore.Router               { return nil }
func (c *stubContext) Host() tuicore.HostContext             { return c }
func (c *stubContext) Nav() tuicore.NavContext               { return nil }
func (c *stubContext) LoadLists() tea.Cmd                    { return nil }
func (c *stubContext) LoadUnreadCount() tea.Cmd              { return nil }
func (c *stubContext) LoadUnpushedCount() tea.Cmd            { return nil }
func (c *stubContext) RefreshTimeline() tea.Cmd              { return nil }
func (c *stubContext) RefreshCacheSize() tea.Cmd             { return nil }
func (c *stubContext) FetchRepo(string) tea.Cmd              { return nil }
func (c *stubContext) SetFetchStatus(time.Time, int)         {}
func (c *stubContext) SetFetchingInfo(int, int)              {}
func (c *stubContext) SetSaving(bool)                        {}
func (c *stubContext) SetRetracting(bool)                    {}
func (c *stubContext) Update(tea.Msg) tea.Cmd                { return nil }
func (c *stubContext) ActivateView() tea.Cmd                 { return nil }
func (c *stubContext) RefreshView() tea.Cmd                  { return nil }
func (c *stubContext) GetSourceItem(int) (string, int, bool) { return "", 0, false }
func (c *stubContext) UpdateSourceIndex(int, int)            {}
func (c *stubContext) State() *tuicore.State                 { return c.state }

func (c *stubContext) SetMessage(msg string, msgType tuicore.MessageType) {
	c.message, c.msgType = msg, msgType
}

func (c *stubContext) SetMessageWithTimeout(msg string, msgType tuicore.MessageType, _ time.Duration) tea.Cmd {
	c.SetMessage(msg, msgType)
	return nil
}

// TestFailedSaveReachesTheForm checks that a failed create or edit is passed on, so the form clears its submitting state and the user can retry.
func TestFailedSaveReachesTheForm(t *testing.T) {
	err := errors.New("commit failed: ref is locked")
	tests := []struct {
		name string
		msg  tea.Msg
	}{
		{"issue created", issueCreatedMsg{Err: err}},
		{"issue updated", issueUpdatedMsg{Err: err}},
		{"milestone created", milestoneCreatedMsg{Err: err}},
		{"milestone updated", milestoneUpdatedMsg{Err: err}},
		{"sprint created", sprintCreatedMsg{Err: err}},
		{"sprint updated", sprintUpdatedMsg{Err: err}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &stubContext{state: &tuicore.State{}}
			handled, cmd := handlePMMessages(tt.msg, ctx)
			if handled {
				t.Error("the handler claimed the failure; the form never sees it and stays stuck submitting")
			}
			if cmd != nil {
				t.Error("a failed save returned a command; nothing should run on the error path")
			}
			if ctx.message != err.Error() || ctx.msgType != tuicore.MessageTypeError {
				t.Errorf("message = %q (%v), want the error text", ctx.message, ctx.msgType)
			}
		})
	}
}
