// view_memos.go - Memo list view backed by the shared CardList component
package tuimemo

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
	"github.com/gitsocial-org/gitsocial/library/extensions/memo"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// MemosView displays memos using the shared CardList. The variant determines
// the filter applied (all merged tiers, project only, inherited only, etc.).
type MemosView struct {
	workdir      string
	workspaceURL string
	userEmail    string
	width        int
	height       int
	loaded       bool
	showEmail    bool
	cardList     *tuicore.CardList
	variant      memosVariant
	sessionID    string // populated from route params for variantSessionItems
	restoreIndex int    // cursor position to restore after Activate's data load (-1 = none)
}

type memosVariant int

const (
	variantAll memosVariant = iota
	variantProject
	variantInherited
	variantPersonal
	variantSessionItems // memos for a specific session id (read from route params)
)

// NewMemosView creates the default merged memos view.
func NewMemosView(workdir string) *MemosView { return newMemosViewVariant(workdir, variantAll) }

// NewProjectMemosView creates a memos view scoped to the workspace's project tier.
func NewProjectMemosView(workdir string) *MemosView {
	return newMemosViewVariant(workdir, variantProject)
}

// NewInheritedMemosView creates a memos view scoped to inherited binding sources.
func NewInheritedMemosView(workdir string) *MemosView {
	return newMemosViewVariant(workdir, variantInherited)
}

// NewPersonalMemosView creates a memos view scoped to the personal tier.
func NewPersonalMemosView(workdir string) *MemosView {
	return newMemosViewVariant(workdir, variantPersonal)
}

// NewSessionItemsView creates a memos view scoped to a specific session id
// (read from the route's `sessionID` param).
func NewSessionItemsView(workdir string) *MemosView {
	return newMemosViewVariant(workdir, variantSessionItems)
}

func newMemosViewVariant(workdir string, v memosVariant) *MemosView {
	return &MemosView{
		workdir:      workdir,
		userEmail:    git.GetUserEmail(workdir),
		cardList:     tuicore.NewCardList(nil),
		variant:      v,
		restoreIndex: -1,
	}
}

// SetSize updates the view dimensions.
func (v *MemosView) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.cardList.SetSize(w, h-2)
}

// Activate loads memos when the view becomes active. The current cursor is
// captured into restoreIndex so the load handler can put it back after the
// items are replaced (preserves selection across detail-back navigation).
// For variantSessionItems we additionally detect when the session id has
// changed since the last visit; in that case the previous cursor belongs to
// a different session and is discarded so the new session starts at row 0.
func (v *MemosView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	v.cardList.SetCardOptions(tuicore.CardOptions{
		MaxLines:  2,
		ShowStats: false,
		Separator: true,
	})
	preserve := true
	if v.variant == variantSessionItems {
		newID := state.Router.Location().Params["sessionID"]
		if newID != v.sessionID {
			preserve = false
			v.cardList.SetSelected(0)
		}
		v.sessionID = newID
	}
	if preserve {
		if cur := v.cardList.Selected(); cur > 0 {
			v.restoreIndex = cur
		}
	} else {
		v.restoreIndex = -1
	}
	return v.loadMemos()
}

// Refresh reloads in place, preserving cursor.
func (v *MemosView) Refresh(state *tuicore.State) tea.Cmd {
	return v.loadMemos()
}

// Update handles input and delegates list operations to CardList.
func (v *MemosView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch m := msg.(type) {
	case memosLoadedMsg:
		v.loaded = true
		if m.err != nil {
			return nil
		}
		v.workspaceURL = m.workspaceURL
		items := make([]tuicore.DisplayItem, len(m.memos))
		for i, mm := range m.memos {
			items[i] = tuicore.NewItem(mm.ID, "memo", "memo", mm.Timestamp, memoItemData{
				Memo:         mm,
				ShowEmail:    v.showEmail,
				UserEmail:    v.userEmail,
				WorkspaceURL: m.workspaceURL,
			})
		}
		v.cardList.SetItems(items)
		if v.restoreIndex >= 0 {
			if v.restoreIndex < len(items) {
				v.cardList.SetSelected(v.restoreIndex)
			}
			v.restoreIndex = -1
		}
		return nil
	case memoSyncMsg:
		if m.isErr {
			state.SetMessage(m.summary, tuicore.MessageTypeError)
		} else {
			state.SetMessage(m.summary, tuicore.MessageTypeSuccess)
		}
		return v.loadMemos()
	case tea.KeyPressMsg, tea.MouseMsg:
		consumed, activate, link := v.cardList.Update(msg)
		if link != nil {
			return func() tea.Msg { return tuicore.NavigateMsg{Location: *link, Action: tuicore.NavPush} }
		}
		if activate {
			return v.navigateToSelected()
		}
		if consumed {
			return tuicore.ConsumedCmd
		}
		if key, ok := msg.(tea.KeyPressMsg); ok {
			return v.handleKey(key, state)
		}
	}
	return nil
}

// IsInputActive reports whether the view is consuming text input (always false).
func (v *MemosView) IsInputActive() bool { return false }

// Render renders the view through the standard wrapper + footer.
func (v *MemosView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	var content string
	if !v.loaded {
		content = "Loading memos..."
	} else if len(v.cardList.Items()) == 0 {
		content = tuicore.Dim.Render("  No memos found")
	} else {
		v.cardList.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
		content = v.cardList.View()
	}
	footer := tuicore.RenderFooter(state.Registry, v.bindingContext(), nil)
	return wrapper.Render(content, footer)
}

// bindingContext returns the keybinding context this variant lives in. The
// inherited and personal lists have their own contexts so their view-specific
// bindings (`m:manage`, `p:push`) stay scoped and don't leak into the other
// lists, which share the generic MemoList context.
func (v *MemosView) bindingContext() tuicore.Context {
	switch v.variant {
	case variantInherited:
		return tuicore.MemoInheritedList
	case variantPersonal:
		return tuicore.MemoPersonalList
	}
	return tuicore.MemoList
}

// Bindings returns the view's keybindings.
func (v *MemosView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	ctx := v.bindingContext()
	var bindings []tuicore.Binding
	if v.variant != variantInherited {
		bindings = append(bindings, tuicore.Binding{
			Key: "n", Label: "new", Contexts: []tuicore.Context{ctx}, Handler: noop,
		})
	}
	if v.variant == variantInherited {
		bindings = append(bindings, tuicore.Binding{
			Key: "m", Label: "manage", Contexts: []tuicore.Context{ctx}, Handler: noop,
		})
	}
	if v.variant == variantPersonal {
		bindings = append(bindings,
			tuicore.Binding{Key: "p", Label: "push", Contexts: []tuicore.Context{ctx}, Handler: noop},
			tuicore.Binding{Key: "f", Label: "fetch", Contexts: []tuicore.Context{ctx}, Handler: noop},
		)
	}
	bindings = append(bindings, tuicore.Binding{
		Key: "r", Label: "refresh", Contexts: []tuicore.Context{ctx}, Handler: noop,
	})
	return bindings
}

// GetItemAt returns the memo ID at the given list index (used by detail nav).
func (v *MemosView) GetItemAt(index int) (string, bool) {
	items := v.cardList.Items()
	if index >= 0 && index < len(items) {
		return items[index].ItemID(), true
	}
	return "", false
}

// GetItemCount returns the total number of items in the list.
func (v *MemosView) GetItemCount() int {
	return len(v.cardList.Items())
}

// SetSourceCursor moves the list cursor to the given index. Called by the
// host when the detail view navigates left/right so esc-back returns to the
// currently-displayed memo's row.
func (v *MemosView) SetSourceCursor(index int) {
	if index < 0 {
		return
	}
	if index < len(v.cardList.Items()) {
		v.cardList.SetSelected(index)
	}
}

// Title returns the view's title with current selection info.
func (v *MemosView) Title() string {
	title := "☞  Memos"
	switch v.variant {
	case variantProject:
		title = "☞  Project Memos"
	case variantInherited:
		title = "☞  Inherited Memos"
	case variantPersonal:
		title = "☞  Personal Memos"
		if path, err := settings.PersonalRepoPath(); err == nil && path != "" {
			title += " · " + tildePath(path)
		}
	case variantSessionItems:
		if v.sessionID != "" {
			title = "☞  Session · " + v.sessionID
			if path, err := memo.SessionRepoPath(v.sessionID); err == nil && path != "" {
				title += " · " + tildePath(path)
			}
		} else {
			title = "☞  Session"
		}
	}
	items := v.cardList.Items()
	if len(items) > 0 {
		return fmt.Sprintf("%s · %d/%d", title, v.cardList.Selected()+1, len(items))
	}
	return title
}

// tildePath collapses a leading $HOME to "~" so list titles stay compact.
func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

// HeaderInfo returns position info for the title bar.
func (v *MemosView) HeaderInfo() (int, string) {
	items := v.cardList.Items()
	if len(items) == 0 {
		return 0, ""
	}
	return v.cardList.Selected() + 1, fmt.Sprintf("%d", len(items))
}

func (v *MemosView) navigateToSelected() tea.Cmd {
	item, ok := v.cardList.SelectedItem()
	if !ok {
		return nil
	}
	id := item.ItemID()
	sourcePath := v.sourcePath()
	idx := v.cardList.Selected()
	total := len(v.cardList.Items())
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location:    tuicore.LocMemoDetail(id),
			Action:      tuicore.NavPush,
			SourcePath:  sourcePath,
			SourceIndex: idx,
			SourceTotal: total,
		}
	}
}

// sourcePath returns the route path that hosts this variant's view, so the
// detail view's left/right navigation can find the correct source list.
func (v *MemosView) sourcePath() string {
	switch v.variant {
	case variantProject:
		return "/memo/project"
	case variantInherited:
		return "/memo/inherited"
	case variantPersonal:
		return "/memo/personal"
	case variantSessionItems:
		return "/memo/session/items"
	}
	return "/memo/list"
}

func (v *MemosView) handleKey(msg tea.KeyPressMsg, state *tuicore.State) tea.Cmd {
	switch msg.String() {
	case "r":
		return v.loadMemos()
	case "n":
		if v.variant == variantInherited {
			return nil
		}
		tier := v.defaultCreateTier()
		return func() tea.Msg {
			return tuicore.NavigateMsg{Location: tuicore.LocMemoNew(tier), Action: tuicore.NavPush}
		}
	case "m":
		if v.variant == variantInherited {
			return func() tea.Msg {
				return tuicore.NavigateMsg{Location: tuicore.LocMemoInherits, Action: tuicore.NavPush}
			}
		}
	case "p":
		if v.variant == variantPersonal {
			return v.syncTier(state, "push")
		}
	case "f":
		if v.variant == variantPersonal {
			return v.syncTier(state, "fetch")
		}
	}
	return nil
}

// syncTier pushes or fetches the personal-tier bare repo to its remote. Runs
// async (network) and reports via memoSyncMsg; a fetch reloads the list so
// pulled memos appear. The missing-remote case surfaces as the result error.
func (v *MemosView) syncTier(state *tuicore.State, action string) tea.Cmd {
	if action == "push" {
		state.SetMessage("Pushing personal memos…", tuicore.MessageTypeSuccess)
		return func() tea.Msg {
			res := memo.PushPersonal()
			if !res.Success {
				return memoSyncMsg{summary: "Push failed: " + res.Error.Message, isErr: true}
			}
			return memoSyncMsg{summary: "Pushed personal memos"}
		}
	}
	state.SetMessage("Fetching personal memos…", tuicore.MessageTypeSuccess)
	return func() tea.Msg {
		res := memo.FetchPersonal()
		if !res.Success {
			return memoSyncMsg{summary: "Fetch failed: " + res.Error.Message, isErr: true}
		}
		return memoSyncMsg{summary: "Fetched personal memos"}
	}
}

// defaultCreateTier maps the current list variant to the tier the new-memo
// form should preselect; empty means use the form's own default (session).
func (v *MemosView) defaultCreateTier() string {
	switch v.variant {
	case variantProject:
		return string(memo.TierProject)
	case variantPersonal:
		return string(memo.TierPersonal)
	case variantSessionItems:
		return string(memo.TierSession)
	}
	return ""
}

type memosLoadedMsg struct {
	memos        []memo.Memo
	workspaceURL string
	err          error
}

// memoSyncMsg carries the result of an async personal-tier push/fetch.
type memoSyncMsg struct {
	summary string
	isErr   bool
}

func (v *MemosView) loadMemos() tea.Cmd {
	workdir := v.workdir
	variant := v.variant
	return func() tea.Msg {
		_ = memo.SyncAllTierReposToCache(workdir)
		opts := memo.ListOptions{IncludeSessions: "all"}
		switch variant {
		case variantProject:
			opts.Tier = memo.TierProject
		case variantInherited:
			opts.Tier = memo.TierInherited
		case variantPersonal:
			opts.Tier = memo.TierPersonal
		case variantSessionItems:
			opts.Tier = memo.TierSession
			opts.IncludeSessions = v.sessionID
		}
		res := memo.ListMemos(workdir, opts)
		if !res.Success {
			return memosLoadedMsg{err: fmt.Errorf("%s", res.Error.Message)}
		}
		return memosLoadedMsg{
			memos:        res.Data,
			workspaceURL: gitmsg.ResolveRepoURL(workdir),
		}
	}
}
