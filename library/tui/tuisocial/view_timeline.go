// timeline.go - Main timeline view showing aggregated posts from all sources
package tuisocial

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/extensions/social"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// TimelineView displays the social timeline.
type TimelineView struct {
	cardlist     *tuicore.CardList
	workdir      string
	userEmail    string
	showEmail    bool
	restoreIndex int // cursor position to restore after refresh (-1 = none)
	pag          tuicore.Pagination
}

// Bindings returns keybindings for the timeline view.
func (v *TimelineView) Bindings() []tuicore.Binding {
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "m", Label: "my repo", Contexts: []tuicore.Context{tuicore.Timeline},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				return true, func() tea.Msg {
					return tuicore.NavigateMsg{Location: tuicore.LocMyRepo, Action: tuicore.NavPush}
				}
			}},
		{Key: "n", Label: "new post", Contexts: []tuicore.Context{tuicore.Timeline, tuicore.MyRepository},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				if ctx.OpenEditor == nil {
					return false, nil
				}
				return true, ctx.OpenEditor("post", "")
			}},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.Timeline, tuicore.MyRepository}, Handler: push},
		{Key: "l", Label: "lists", Contexts: []tuicore.Context{tuicore.Timeline, tuicore.MyRepository},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				return true, func() tea.Msg {
					return tuicore.NavigateMsg{Location: tuicore.LocLists, Action: tuicore.NavPush}
				}
			}},
		{Key: "r", Label: "refresh", Contexts: []tuicore.Context{tuicore.Timeline},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				return false, nil // Handled by view
			}},
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.Timeline, tuicore.Search},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				if ctx.Navigate != nil {
					return true, ctx.Navigate(tuicore.Search)
				}
				return true, nil
			}},
	}
}

// NewTimelineView creates a new timeline view.
func NewTimelineView(workdir string, userEmail string, showEmail bool) *TimelineView {
	v := &TimelineView{
		workdir:      workdir,
		userEmail:    userEmail,
		showEmail:    showEmail,
		restoreIndex: -1,
	}
	v.cardlist = tuicore.NewCardList(nil)
	v.cardlist.SetItemResolver(v.resolveItem)
	return v
}

// resolveItem fetches a post by ID via API.
func (v *TimelineView) resolveItem(itemID string) (tuicore.DisplayItem, bool) {
	result := social.GetPosts(v.workdir, "post:"+itemID, nil)
	if result.Success && len(result.Data) > 0 {
		post := result.Data[0]
		post.Display.UserEmail = v.userEmail
		post.Display.ShowEmail = v.showEmail
		return tuicore.NewItem(post.ID, "social", string(post.Type), post.Timestamp, post), true
	}
	return nil, false
}

// SetUserEmail sets the user email for own-post highlighting.
func (v *TimelineView) SetUserEmail(email string) {
	v.userEmail = email
}

// SetSize sets the view dimensions (receives inner content area).
func (v *TimelineView) SetSize(width, height int) {
	v.cardlist.SetSize(width, height-3) // -3 for footer
}

// Activate loads timeline posts when the view becomes active.
func (v *TimelineView) Activate(state *tuicore.State) tea.Cmd {
	if state.DetailSource != nil && state.DetailSource.Path == "/social/timeline" {
		v.restoreIndex = state.DetailSource.Index
	} else {
		v.restoreIndex = -1
	}
	return v.loadPosts()
}

// loadPosts fetches timeline posts from the social API.
func (v *TimelineView) loadPosts() tea.Cmd {
	v.pag.StartLoading()
	workdir := v.workdir
	limit := v.pag.Limit()
	return func() tea.Msg {
		result := social.GetPosts(workdir, "timeline", &social.GetPostsOptions{Limit: limit + 1})
		if !result.Success {
			return TimelineLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		posts, hasMore := tuicore.TrimPage(result.Data, limit)
		total := social.CountTimeline(workdir)
		return TimelineLoadedMsg{Posts: posts, HasMore: hasMore, Total: total}
	}
}

// loadMorePosts fetches the next page of timeline posts.
func (v *TimelineView) loadMorePosts() tea.Cmd {
	v.pag.StartLoading()
	workdir := v.workdir
	cursor := v.pag.Cursor
	return func() tea.Msg {
		result := social.GetPosts(workdir, "timeline", &social.GetPostsOptions{Limit: tuicore.PageSize + 1, Cursor: cursor})
		if !result.Success {
			return TimelineLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message), Append: true}
		}
		posts, hasMore := tuicore.TrimPage(result.Data, tuicore.PageSize)
		return TimelineLoadedMsg{Posts: posts, HasMore: hasMore, Append: true}
	}
}

// LoadMorePosts implements the loadMoreHandler interface for infinite scroll.
func (v *TimelineView) LoadMorePosts() tea.Cmd {
	return v.pag.LoadMore(v.loadMorePosts)
}

// Update handles messages and returns commands.
func (v *TimelineView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg, tea.MouseMsg:
		consumed, activate, link := v.cardlist.Update(msg)
		if link != nil {
			return func() tea.Msg { return tuicore.NavigateMsg{Location: *link, Action: tuicore.NavPush} }
		}
		if activate {
			return v.navigateToSelected()
		}
		if consumed {
			if v.cardlist.NearBottom() && v.pag.CanLoadMore() {
				return tea.Batch(tuicore.ConsumedCmd, v.loadMorePosts())
			}
			return tuicore.ConsumedCmd
		}
		if key, ok := msg.(tea.KeyPressMsg); ok {
			return v.handleKey(key)
		}
	case TimelineLoadedMsg:
		v.handleLoaded(msg)
	}
	return nil
}

// navigateToSelected navigates to the selected item's detail view.
func (v *TimelineView) navigateToSelected() tea.Cmd {
	item, ok := v.cardlist.SelectedItem()
	if !ok {
		return nil
	}
	items := v.cardlist.Items()
	total := v.pag.Total(len(items))
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location:    tuicore.GetNavTarget(item),
			Action:      tuicore.NavPush,
			SourcePath:  "/social/timeline",
			SourceIndex: v.cardlist.Selected(),
			SourceTotal: total,
		}
	}
}

// handleKey processes view-specific keyboard input.
func (v *TimelineView) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "r":
		v.restoreIndex = v.cardlist.Selected()
		v.pag.ResetForRefresh(len(v.cardlist.Items()))
		return v.loadPosts()
	}
	return nil
}

// handleLoaded updates the view with loaded posts.
func (v *TimelineView) handleLoaded(msg TimelineLoadedMsg) {
	if msg.Err != nil {
		v.pag.Loading = false
		return
	}
	cursor := ""
	if len(msg.Posts) > 0 {
		cursor = msg.Posts[len(msg.Posts)-1].Timestamp.Format(time.RFC3339)
	}
	v.pag.Done(msg.HasMore, cursor)
	v.pag.SetTotal(msg.Total)
	items := PostsToItems(msg.Posts, v.userEmail, v.showEmail)
	if msg.Append {
		v.cardlist.AppendItems(items)
	} else {
		v.cardlist.SetItems(items)
		if v.restoreIndex >= 0 {
			v.cardlist.SetSelected(v.restoreIndex)
			v.restoreIndex = -1
		}
	}
}

// Render renders the timeline view to a string.
func (v *TimelineView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	var content string
	if v.pag.Loading && len(v.cardlist.Items()) == 0 {
		content = tuicore.Dim.Render("Loading...")
	} else {
		content = v.cardlist.View()
	}

	footer := tuicore.RenderFooter(state.Registry, tuicore.Timeline, wrapper.ContentWidth(), nil)

	return wrapper.Render(content, footer)
}

// IsInputActive returns false since timeline doesn't have text input.
func (v *TimelineView) IsInputActive() bool {
	return false
}

// UpdateItem updates an item in the list by ID.
func (v *TimelineView) UpdateItem(item tuicore.DisplayItem) {
	items := v.cardlist.Items()
	for i, existing := range items {
		if existing.ItemID() == item.ItemID() {
			items[i] = item
			v.cardlist.SetItems(items)
			return
		}
	}
}

// RemoveItem removes an item from the list by ID.
func (v *TimelineView) RemoveItem(itemID string) {
	items := v.cardlist.Items()
	for i, item := range items {
		if item.ItemID() == itemID {
			v.cardlist.SetItems(append(items[:i], items[i+1:]...))
			return
		}
	}
}

// DisplayItems returns all timeline items.
func (v *TimelineView) DisplayItems() []tuicore.DisplayItem {
	return v.cardlist.Items()
}

// SetDisplayItems replaces all timeline items (extension-agnostic).
func (v *TimelineView) SetDisplayItems(items []tuicore.DisplayItem) {
	v.cardlist.SetItems(items)
	if v.restoreIndex >= 0 {
		v.cardlist.SetSelected(v.restoreIndex)
		v.restoreIndex = -1
	}
}

// SelectedDisplayItem returns the currently selected item (extension-agnostic).
func (v *TimelineView) SelectedDisplayItem() (tuicore.DisplayItem, bool) {
	return v.cardlist.SelectedItem()
}

// HeaderInfo returns position and total for the header display.
func (v *TimelineView) HeaderInfo() (position, total int) {
	items := v.cardlist.Items()
	if len(items) == 0 {
		return 0, 0
	}
	return v.cardlist.Selected() + 1, v.pag.Total(len(items))
}

// GetDisplayItemAt returns the full DisplayItem at the given index.
func (v *TimelineView) GetDisplayItemAt(index int) (tuicore.DisplayItem, bool) {
	items := v.cardlist.Items()
	if index >= 0 && index < len(items) {
		return items[index], true
	}
	return nil, false
}

// GetItemAt returns the post ID at the given index.
func (v *TimelineView) GetItemAt(index int) (string, bool) {
	items := v.cardlist.Items()
	if index >= 0 && index < len(items) {
		return items[index].ItemID(), true
	}
	return "", false
}

// GetItemCount returns the total number of items.
func (v *TimelineView) GetItemCount() int {
	return len(v.cardlist.Items())
}
