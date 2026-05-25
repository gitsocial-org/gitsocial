// util_register.go - Memo extension TUI registration: views, card renderer, nav target
package tuimemo

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/identity"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/memo"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

func init() {
	tuicore.RegisterViewMeta(tuicore.ViewMeta{
		Path:      "/memo/list",
		Context:   tuicore.MemoList,
		Title:     "Memos",
		Icon:      "☞",
		NavItemID: "memo",
		Component: "CardList",
	})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{
		Path:      "/memo/project",
		Context:   tuicore.MemoList,
		Title:     "Project Memos",
		Icon:      "☞",
		NavItemID: "memo.project",
		Component: "CardList",
	})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{
		Path:      "/memo/inherited",
		Context:   tuicore.MemoInheritedList,
		Title:     "Inherited Memos",
		Icon:      "☞",
		NavItemID: "memo.inherited",
		Component: "CardList",
	})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{
		Path:      "/memo/personal",
		Context:   tuicore.MemoList,
		Title:     "Personal Memos",
		Icon:      "☞",
		NavItemID: "memo.personal",
		Component: "CardList",
	})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{
		Path:      "/memo/session",
		Context:   tuicore.MemoSessions,
		Title:     "Sessions",
		Icon:      "☞",
		NavItemID: "memo.session",
	})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{
		Path:      "/memo/session/items",
		Context:   tuicore.MemoList,
		Title:     "Session Memos",
		Icon:      "☞",
		NavItemID: "memo.session",
		Component: "CardList",
	})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{
		Path:      "/memo/inherits",
		Context:   tuicore.MemoInherits,
		Title:     "Inherited Sources",
		Icon:      "☞",
		NavItemID: "memo.inherited",
	})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{
		Path:      "/memo/detail",
		Context:   tuicore.MemoDetail,
		Title:     "Memo Detail",
		Icon:      "☞",
		NavItemID: "memo",
		Component: "SectionList",
	})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{
		Path:      "/memo/history",
		Context:   tuicore.MemoHistory,
		Title:     "Memo History",
		Icon:      "☞",
		NavItemID: "memo",
		Component: "VersionPicker",
	})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{
		Path:      "/memo/history/diff",
		Context:   tuicore.MemoHistoryDiff,
		Title:     "Memo Diff",
		Icon:      "☞",
		NavItemID: "memo",
	})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{
		Path:      "/memo/edit",
		Context:   tuicore.MemoDetail,
		Title:     "Edit Memo",
		Icon:      "☞",
		NavItemID: "memo",
	})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{
		Path:      "/memo/new",
		Context:   tuicore.MemoDetail,
		Title:     "New Memo",
		Icon:      "☞",
		NavItemID: "memo",
	})
	tuicore.RegisterMessageHandler(handleMemoMessages)
	tuicore.RegisterNavTarget(
		tuicore.ItemType{Extension: "memo", Type: "memo"},
		func(id string) tuicore.Location { return tuicore.LocMemoDetail(id) },
	)
	tuicore.RegisterCardRenderer(
		tuicore.ItemType{Extension: "memo", Type: "memo"},
		memoCardRenderer,
	)
	tuicore.RegisterDimmedChecker(
		tuicore.ItemType{Extension: "memo", Type: "memo"},
		memoDimmedChecker,
	)
}

// Register adds memo views to the host.
func Register(host tuicore.ViewHost) {
	state := host.State()
	host.AddView("/memo/list", NewMemosView(state.Workdir))
	host.AddView("/memo/project", NewProjectMemosView(state.Workdir))
	host.AddView("/memo/inherited", NewInheritedMemosView(state.Workdir))
	host.AddView("/memo/personal", NewPersonalMemosView(state.Workdir))
	host.AddView("/memo/session", NewSessionsView(state.Workdir))
	host.AddView("/memo/session/items", NewSessionItemsView(state.Workdir))
	host.AddView("/memo/inherits", NewInheritsView(state.Workdir))
	host.AddView("/memo/detail", NewMemoDetailView(state.Workdir))
	host.AddView("/memo/history", NewMemoHistoryView(state.Workdir))
	host.AddView("/memo/history/diff", NewMemoHistoryDiffView(state.Workdir))
	host.AddView("/memo/edit", NewMemoFormView(state.Workdir))
	host.AddView("/memo/new", NewMemoCreateFormView(state.Workdir))
}

// memoItemData wraps a memo with display context for card rendering.
type memoItemData struct {
	Memo         memo.Memo
	ShowEmail    bool
	UserEmail    string
	WorkspaceURL string
}

// memoCardRenderer renders a memo as a Card.
func memoCardRenderer(data any, _ tuicore.ItemResolver) tuicore.Card {
	switch d := data.(type) {
	case memoItemData:
		return memoToCard(d)
	case memo.Memo:
		return memoToCard(memoItemData{Memo: d})
	}
	return tuicore.Card{Header: tuicore.CardHeader{Title: "Invalid memo"}}
}

// memoDimmedChecker dims retracted or stale memos.
func memoDimmedChecker(data any) bool {
	switch d := data.(type) {
	case memoItemData:
		return d.Memo.IsRetracted
	case memo.Memo:
		return d.IsRetracted
	}
	return false
}

// memoToCard converts a memo into the standard Card display unit.
func memoToCard(d memoItemData) tuicore.Card {
	m := d.Memo
	authorName := m.Author.Name
	if d.ShowEmail && m.Author.Email != "" {
		authorName += " <" + m.Author.Email + ">"
	}
	if authorName == "" {
		authorName = "(unknown)"
	}

	subtitle := []tuicore.HeaderPart{
		{Text: authorName},
	}
	if !m.Timestamp.IsZero() {
		subtitle = append(subtitle, tuicore.HeaderPart{Text: tuicore.FormatTime(m.Timestamp)})
	}
	if m.Tier != "" {
		subtitle = append(subtitle, tuicore.HeaderPart{Text: "[" + string(m.Tier) + "]"})
	}
	for _, l := range m.Labels {
		subtitle = append(subtitle, tuicore.HeaderPart{Text: l})
	}
	isWorkspace := d.WorkspaceURL != "" && m.Repository == d.WorkspaceURL
	if ref := tuicore.BuildRef(m.ID, m.Repository, m.Branch, isWorkspace); ref != "" {
		var refLink *tuicore.Location
		if !isWorkspace && m.Repository != "" && !strings.HasPrefix(m.Repository, "local:") {
			loc := tuicore.LocRepository(m.Repository, m.Branch)
			refLink = &loc
		}
		subtitle = append(subtitle, tuicore.HeaderPart{Text: ref, Link: refLink})
	}

	badge := ""
	switch {
	case m.IsRetracted:
		badge = "retracted"
	case isMemoExpired(m.Labels):
		badge = "expired"
	}

	card := tuicore.Card{
		Header: tuicore.CardHeader{
			Icon:        "☞",
			Title:       m.Subject,
			Subtitle:    subtitle,
			Badge:       badge,
			IsEdited:    m.IsEdited,
			IsRetracted: m.IsRetracted,
			IsStale:     m.IsStale,
		},
		Content:      tuicore.CardContent{Text: m.Body},
		ContentLinks: tuicore.ExtractContentLinks(m.Body, m.Repository, ""),
	}
	if d.UserEmail != "" && strings.EqualFold(m.Author.Email, d.UserEmail) {
		card.Header.IsMe = true
	}
	if d.WorkspaceURL != "" && m.Repository == d.WorkspaceURL {
		card.Header.IsOwnRepo = true
	}
	if m.Repository != "" && m.Author.Email != "" && !strings.HasPrefix(m.Repository, "local:") {
		hash := protocol.ParseRef(m.ID).Value
		card.Header.IsVerified = identity.IsVerifiedCommit(m.Repository, hash, m.Author.Email)
	}
	return card
}

// isMemoExpired reports whether any expires/<date> label is in the past.
// Accepts YYYY-MM-DD and RFC3339, matching memo.validateLabels.
func isMemoExpired(labels []string) bool {
	now := time.Now()
	for _, l := range labels {
		v, ok := strings.CutPrefix(l, "expires/")
		if !ok {
			continue
		}
		if t, err := time.Parse("2006-01-02", v); err == nil {
			if t.Before(now) {
				return true
			}
			continue
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			if t.Before(now) {
				return true
			}
		}
	}
	return false
}

// handleMemoMessages dispatches memo extension messages (form submit results).
func handleMemoMessages(msg tea.Msg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	switch m := msg.(type) {
	case MemoEditedMsg:
		return handleMemoEdited(m, ctx)
	case MemoCreatedMsg:
		return handleMemoCreated(m, ctx)
	}
	return false, nil
}

// handleMemoEdited handles the result of a memo edit form submission.
// On success, navigate back to the memo's detail view (NavReplace so /memo/edit
// is removed from the back stack) and surface a brief status message.
func handleMemoEdited(msg MemoEditedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		return true, nil
	}
	statusCmd := ctx.Host().SetMessageWithTimeout(
		fmt.Sprintf("Updated: %s", msg.Memo.Subject),
		tuicore.MessageTypeSuccess,
		5*time.Second,
	)
	return true, tea.Batch(statusCmd, func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocMemoDetail(msg.MemoID),
			Action:   tuicore.NavReplace,
		}
	})
}

// handleMemoCreated handles the result of a memo create form submission.
// On success, replace /memo/new with the new memo's detail view and surface
// a brief status message; on error, keep the form mounted and show the error.
func handleMemoCreated(msg MemoCreatedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		return true, nil
	}
	statusCmd := ctx.Host().SetMessageWithTimeout(
		fmt.Sprintf("Created: %s", msg.Memo.Subject),
		tuicore.MessageTypeSuccess,
		5*time.Second,
	)
	return true, tea.Batch(statusCmd, func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocMemoDetail(msg.Memo.ID),
			Action:   tuicore.NavReplace,
		}
	})
}
