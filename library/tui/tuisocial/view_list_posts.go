// list_posts.go - Posts view for displaying timeline from a specific list
package tuisocial

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/extensions/social"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// ListPostsView displays posts from a specific list.
type ListPostsView struct {
	list              social.List
	cardlist          *tuicore.CardList
	externalListOwner string
	workdir           string
	userEmail         string
	showEmail         bool
	restoreIndex      int // cursor position to restore after refresh (-1 = none)
	pag               tuicore.Pagination
}

// Bindings returns keybindings for the list posts view.
func (v *ListPostsView) Bindings() []tuicore.Binding {
	return []tuicore.Binding{
		{Key: "m", Label: "more", Contexts: []tuicore.Context{tuicore.ListPosts},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil || !ctx.Panel.IsExternalList() {
					return false, nil
				}
				return true, ctx.Panel.LoadMorePosts()
			}},
		{Key: "r", Label: "repositories", Contexts: []tuicore.Context{tuicore.ListPosts},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.ToggleListView()
			}},
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.ListPosts},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.SearchInList()
			}},
	}
}

// NewListPostsView creates a new list posts view.
func NewListPostsView(workdir string) *ListPostsView {
	v := &ListPostsView{
		workdir:      workdir,
		restoreIndex: -1,
	}
	v.cardlist = tuicore.NewCardList(nil)
	v.cardlist.SetItemResolver(v.resolveItem)
	return v
}

// resolveItem fetches a post by ID via API.
func (v *ListPostsView) resolveItem(itemID string) (tuicore.DisplayItem, bool) {
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
func (v *ListPostsView) SetUserEmail(email string) {
	v.userEmail = email
}

// SetShowEmail sets whether to show emails on cards.
func (v *ListPostsView) SetShowEmail(show bool) {
	v.showEmail = show
}

// SetSize sets the view dimensions (receives inner content area).
func (v *ListPostsView) SetSize(width, height int) {
	v.cardlist.SetSize(width, height-3) // -3 for footer
}

// Activate loads list posts when the view becomes active.
func (v *ListPostsView) Activate(state *tuicore.State) tea.Cmd {
	loc := state.Router.Location()
	listID := loc.Param("listID")
	owner := loc.Param("owner")

	// Restore cursor position when returning from detail view
	if state.DetailSource != nil && state.DetailSource.Path == "/social/list" {
		v.restoreIndex = state.DetailSource.Index
	} else {
		v.restoreIndex = -1
	}

	v.externalListOwner = owner
	v.list = social.List{ID: listID, Name: listID}
	v.pag.StartLoading()
	v.cardlist.SetItems(nil)

	if owner != "" {
		return v.loadExternalListPosts(owner, listID)
	}
	return v.loadListPosts(listID)
}

// loadListPosts fetches posts for a local list.
func (v *ListPostsView) loadListPosts(listID string) tea.Cmd {
	workdir := v.workdir
	limit := v.pag.Limit()
	return func() tea.Msg {
		var list *social.List
		listResult := social.GetList(workdir, listID)
		if listResult.Success {
			list = listResult.Data
		}
		result := social.GetPosts(workdir, "list:"+listID, &social.GetPostsOptions{Limit: limit + 1})
		if !result.Success {
			return ListPostsLoadedMsg{ListID: listID, Err: fmt.Errorf("%s", result.Error.Message)}
		}
		posts, hasMore := tuicore.TrimPage(result.Data, limit)
		total := social.CountListPosts(listID)
		return ListPostsLoadedMsg{ListID: listID, List: list, Posts: posts, HasMore: hasMore, Total: total}
	}
}

// loadMoreListPosts fetches the next page of list posts.
func (v *ListPostsView) loadMoreListPosts() tea.Cmd {
	v.pag.StartLoading()
	workdir := v.workdir
	listID := v.list.ID
	cursor := v.pag.Cursor
	return func() tea.Msg {
		result := social.GetPosts(workdir, "list:"+listID, &social.GetPostsOptions{Limit: tuicore.PageSize + 1, Cursor: cursor})
		if !result.Success {
			return ListPostsLoadedMsg{ListID: listID, Err: fmt.Errorf("%s", result.Error.Message)}
		}
		posts, hasMore := tuicore.TrimPage(result.Data, tuicore.PageSize)
		return ListPostsLoadedMsg{ListID: listID, Posts: posts, HasMore: hasMore, Append: true}
	}
}

// LoadMorePosts implements the loadMoreHandler interface for infinite scroll.
func (v *ListPostsView) LoadMorePosts() tea.Cmd {
	return v.pag.LoadMore(v.loadMoreListPosts)
}

// loadExternalListPosts fetches posts for an external list.
func (v *ListPostsView) loadExternalListPosts(owner, listID string) tea.Cmd {
	workdir := v.workdir
	return func() tea.Msg {
		result := social.GetPosts(workdir, "list:"+owner+"#list:"+listID, nil)
		if !result.Success {
			return ListPostsLoadedMsg{ListID: listID, Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return ListPostsLoadedMsg{ListID: listID, Posts: result.Data}
	}
}

// Update handles messages and returns commands.
func (v *ListPostsView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg.(type) {
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
				return tea.Batch(tuicore.ConsumedCmd, v.loadMoreListPosts())
			}
			return tuicore.ConsumedCmd
		}
		if key, ok := msg.(tea.KeyPressMsg); ok {
			return v.handleKey(key)
		}
	default:
		if msg, ok := msg.(ListPostsLoadedMsg); ok {
			v.handleLoaded(msg)
		}
	}
	return nil
}

// navigateToSelected navigates to the selected item's detail view.
func (v *ListPostsView) navigateToSelected() tea.Cmd {
	item, ok := v.cardlist.SelectedItem()
	if !ok {
		return nil
	}
	items := v.cardlist.Items()
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location:    tuicore.GetNavTarget(item),
			Action:      tuicore.NavPush,
			SourcePath:  "/social/list",
			SourceIndex: v.cardlist.Selected(),
			SourceTotal: v.pag.Total(len(items)),
		}
	}
}

// handleKey processes view-specific keyboard input.
func (v *ListPostsView) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "r":
		if v.externalListOwner == "" {
			return func() tea.Msg {
				return tuicore.NavigateMsg{
					Location: tuicore.LocListRepos(v.list.ID),
					Action:   tuicore.NavPush,
				}
			}
		}
		return func() tea.Msg {
			return tuicore.NavigateMsg{
				Location: tuicore.LocExternalListRepos(v.externalListOwner, v.list.ID),
				Action:   tuicore.NavPush,
			}
		}
	case "/":
		query := "list:" + v.list.ID
		return func() tea.Msg {
			return tuicore.NavigateMsg{
				Location: tuicore.LocSearchQuery(query),
				Action:   tuicore.NavPush,
			}
		}
	}
	return nil
}

// handleLoaded processes the loaded posts data.
func (v *ListPostsView) handleLoaded(msg ListPostsLoadedMsg) {
	if msg.Err != nil {
		v.pag.Loading = false
		return
	}
	if msg.List != nil {
		v.list = *msg.List
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

// Render renders the list posts view to a string.
func (v *ListPostsView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)

	var content string
	if v.pag.Loading {
		content = tuicore.Dim.Render("Loading...")
	} else if len(v.cardlist.Items()) == 0 {
		content = tuicore.Dim.Render("No posts in this list")
	} else {
		content = v.cardlist.View()
	}

	var footer string
	if v.externalListOwner == "" {
		// Local list - hide "m:more" key
		footer = tuicore.RenderFooter(state.Registry, tuicore.ListPosts, wrapper.ContentWidth(), map[string]bool{"m": true})
	} else {
		footer = tuicore.RenderFooter(state.Registry, tuicore.ListPosts, wrapper.ContentWidth(), nil)
	}
	return wrapper.Render(content, footer)
}

// IsInputActive returns false since list posts view has no text input.
func (v *ListPostsView) IsInputActive() bool {
	return false
}

// IsExternalList returns true if viewing an external list.
func (v *ListPostsView) IsExternalList() bool {
	return v.externalListOwner != ""
}

// Title returns the list name for the header.
func (v *ListPostsView) Title() string {
	return "☷  " + v.list.Name
}

// HeaderInfo returns position and total for the header.
func (v *ListPostsView) HeaderInfo() (position, total int) {
	items := v.cardlist.Items()
	if len(items) == 0 {
		return 0, 0
	}
	return v.cardlist.Selected() + 1, v.pag.Total(len(items))
}

// ToggleListView switches to repos view.
func (v *ListPostsView) ToggleListView() tea.Cmd {
	if v.externalListOwner == "" {
		return func() tea.Msg {
			return tuicore.NavigateMsg{
				Location: tuicore.LocListRepos(v.list.ID),
				Action:   tuicore.NavPush,
			}
		}
	}
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocExternalListRepos(v.externalListOwner, v.list.ID),
			Action:   tuicore.NavPush,
		}
	}
}

// GetDisplayItemAt returns the full DisplayItem at the given index.
func (v *ListPostsView) GetDisplayItemAt(index int) (tuicore.DisplayItem, bool) {
	items := v.cardlist.Items()
	if index >= 0 && index < len(items) {
		return items[index], true
	}
	return nil, false
}

// GetItemAt returns the post ID at the given index.
func (v *ListPostsView) GetItemAt(index int) (string, bool) {
	items := v.cardlist.Items()
	if index >= 0 && index < len(items) {
		return items[index].ItemID(), true
	}
	return "", false
}

// GetItemCount returns the total number of items.
func (v *ListPostsView) GetItemCount() int {
	return len(v.cardlist.Items())
}

// DisplayItems returns all list items.
func (v *ListPostsView) DisplayItems() []tuicore.DisplayItem {
	return v.cardlist.Items()
}

// SetDisplayItems replaces all list items.
func (v *ListPostsView) SetDisplayItems(items []tuicore.DisplayItem) {
	v.cardlist.SetItems(items)
	if v.restoreIndex >= 0 {
		v.cardlist.SetSelected(v.restoreIndex)
		v.restoreIndex = -1
	}
}

// SelectedDisplayItem returns the currently selected item.
func (v *ListPostsView) SelectedDisplayItem() (tuicore.DisplayItem, bool) {
	return v.cardlist.SelectedItem()
}

// SearchInList opens search with the list scope prefilled.
func (v *ListPostsView) SearchInList() tea.Cmd {
	query := "list:" + v.list.ID
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocSearchQuery(query),
			Action:   tuicore.NavPush,
		}
	}
}
