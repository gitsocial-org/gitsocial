// view_list_posts.go - Posts view for displaying timeline from a specific list
package tuisocial

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// listPostsView displays posts from a specific list.
type listPostsView struct {
	list              social.List
	cardlist          *tuicore.CardList
	externalListOwner string
	workdir           string
	userEmail         string
	showEmail         bool
	restoreID         string // item ID to reselect after reload ("" = none)
	pag               tuicore.Pagination
}

// Bindings returns keybindings for the list posts view.
func (v *listPostsView) Bindings() []tuicore.Binding {
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

// newListPostsView creates a new list posts view.
func newListPostsView(workdir string) *listPostsView {
	v := &listPostsView{
		workdir: workdir,
	}
	v.cardlist = tuicore.NewCardList(nil)
	v.cardlist.SetItemResolver(v.resolveItem)
	return v
}

// resolveItem fetches a post by ID via API.
func (v *listPostsView) resolveItem(itemID string) (tuicore.DisplayItem, bool) {
	result := social.GetPosts(v.workdir, "post:"+itemID, nil)
	if result.Success && len(result.Data) > 0 {
		post := result.Data[0]
		post.Display.UserEmail = v.userEmail
		post.Display.ShowEmail = v.showEmail
		post.Display.Workdir = v.workdir
		return tuicore.NewItem(post.ID, "social", string(post.Type), post.Timestamp, post), true
	}
	return nil, false
}

// setUserEmail sets the user email for own-post highlighting.
func (v *listPostsView) setUserEmail(email string) {
	v.userEmail = email
}

// setShowEmail sets whether to show emails on cards.
func (v *listPostsView) setShowEmail(show bool) {
	v.showEmail = show
}

// SetSize sets the view dimensions (receives inner content area).
func (v *listPostsView) SetSize(width, height int) {
	v.cardlist.SetSize(width, height-3) // -3 for footer
}

// Activate loads list posts when the view becomes active.
func (v *listPostsView) Activate(state *tuicore.State) tea.Cmd {
	loc := state.Router.Location()
	listID := loc.Param("listID")
	owner := loc.Param("owner")

	// Restore cursor position when returning from detail view (by ID). Read the
	// id before SetItems(nil) below clears the list.
	v.restoreID = ""
	if state.DetailSource != nil && state.DetailSource.Path == "/social/list" {
		if id, ok := v.GetItemAt(state.DetailSource.Index); ok {
			v.restoreID = id
		}
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

// loadListPosts fetches posts for a local list. The total count is loaded
// asynchronously so the page render isn't blocked on COUNT(*).
func (v *listPostsView) loadListPosts(listID string) tea.Cmd {
	workdir := v.workdir
	limit := v.pag.Limit()
	pageCmd := func() tea.Msg {
		var list *social.List
		listResult := social.GetList(workdir, listID)
		if listResult.Success {
			list = listResult.Data
		}
		result := social.GetPosts(workdir, "list:"+listID, &social.GetPostsOptions{Limit: limit + 1})
		if !result.Success {
			return listPostsLoadedMsg{ListID: listID, Err: fmt.Errorf("%s", result.Error.Text())}
		}
		posts, hasMore := tuicore.TrimPage(result.Data, limit)
		return listPostsLoadedMsg{ListID: listID, List: list, Posts: posts, HasMore: hasMore}
	}
	countCmd := func() tea.Msg {
		return listPostsCountLoadedMsg{ListID: listID, Total: social.CountListPosts(listID)}
	}
	return tea.Batch(pageCmd, countCmd)
}

// loadMoreListPosts fetches the next page of list posts.
func (v *listPostsView) loadMoreListPosts() tea.Cmd {
	v.pag.StartLoading()
	workdir := v.workdir
	listID := v.list.ID
	cursor := v.pag.Cursor
	return func() tea.Msg {
		result := social.GetPosts(workdir, "list:"+listID, &social.GetPostsOptions{Limit: tuicore.PageSize + 1, Cursor: cursor})
		if !result.Success {
			return listPostsLoadedMsg{ListID: listID, Err: fmt.Errorf("%s", result.Error.Text())}
		}
		posts, hasMore := tuicore.TrimPage(result.Data, tuicore.PageSize)
		return listPostsLoadedMsg{ListID: listID, Posts: posts, HasMore: hasMore, Append: true}
	}
}

// LoadMorePosts implements the loadMoreHandler interface for infinite scroll.
func (v *listPostsView) LoadMorePosts() tea.Cmd {
	return v.pag.LoadMore(v.loadMoreListPosts)
}

// loadExternalListPosts fetches posts for an external list.
func (v *listPostsView) loadExternalListPosts(owner, listID string) tea.Cmd {
	workdir := v.workdir
	return func() tea.Msg {
		result := social.GetPosts(workdir, "list:"+owner+"#list:"+listID, nil)
		if !result.Success {
			return listPostsLoadedMsg{ListID: listID, Err: fmt.Errorf("%s", result.Error.Text())}
		}
		return listPostsLoadedMsg{ListID: listID, Posts: result.Data}
	}
}

// Update handles messages and returns commands.
func (v *listPostsView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
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
		switch msg := msg.(type) {
		case listPostsLoadedMsg:
			v.handleLoaded(msg, state)
		case listPostsCountLoadedMsg:
			if msg.ListID == v.list.ID {
				v.pag.SetTotal(msg.Total)
			}
		}
	}
	return nil
}

// navigateToSelected navigates to the selected item's detail view.
func (v *listPostsView) navigateToSelected() tea.Cmd {
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

// Refresh reloads the list's posts in place, preserving the focused row by ID.
func (v *listPostsView) Refresh(_ *tuicore.State) tea.Cmd {
	if id, ok := v.cardlist.SelectedID(); ok {
		v.restoreID = id
	}
	if v.externalListOwner != "" {
		return v.loadExternalListPosts(v.externalListOwner, v.list.ID)
	}
	return v.loadListPosts(v.list.ID)
}

// handleKey processes view-specific keyboard input.
func (v *listPostsView) handleKey(msg tea.KeyPressMsg) tea.Cmd {
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
func (v *listPostsView) handleLoaded(msg listPostsLoadedMsg, state *tuicore.State) {
	if msg.Err != nil {
		v.pag.Loading = false
		state.SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
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
	items := postsToItems(msg.Posts, v.userEmail, v.showEmail, v.workdir)
	if msg.Append {
		v.cardlist.AppendItems(items)
	} else {
		v.cardlist.SetItems(items)
		if v.restoreID != "" {
			v.cardlist.SelectByID(v.restoreID)
			v.restoreID = ""
		}
	}
}

// Render renders the list posts view to a string.
func (v *listPostsView) Render(state *tuicore.State) string {
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
		footer = tuicore.RenderFooter(state.Registry, tuicore.ListPosts, map[string]bool{"m": true})
	} else {
		footer = tuicore.RenderFooter(state.Registry, tuicore.ListPosts, nil)
	}
	return wrapper.Render(content, footer)
}

// IsInputActive returns false since list posts view has no text input.
func (v *listPostsView) IsInputActive() bool {
	return false
}

// IsExternalList returns true if viewing an external list.
func (v *listPostsView) IsExternalList() bool {
	return v.externalListOwner != ""
}

// Title returns the list name for the header, appending the owner when viewing
// another repo's list.
func (v *listPostsView) Title() string {
	if v.externalListOwner != "" {
		return "☷  " + v.list.Name + " · " + protocol.GetDisplayName(v.externalListOwner)
	}
	return "☷  " + v.list.Name
}

// HeaderInfo returns position and total for the header.
func (v *listPostsView) HeaderInfo() (position int, total string) {
	items := v.cardlist.Items()
	if len(items) == 0 {
		return 0, ""
	}
	return v.cardlist.Selected() + 1, v.pag.TotalDisplay(len(items))
}

// ToggleListView switches to repos view.
func (v *listPostsView) ToggleListView() tea.Cmd {
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
func (v *listPostsView) GetDisplayItemAt(index int) (tuicore.DisplayItem, bool) {
	items := v.cardlist.Items()
	if index >= 0 && index < len(items) {
		return items[index], true
	}
	return nil, false
}

// GetItemAt returns the post ID at the given index.
func (v *listPostsView) GetItemAt(index int) (string, bool) {
	items := v.cardlist.Items()
	if index >= 0 && index < len(items) {
		return items[index].ItemID(), true
	}
	return "", false
}

// GetItemCount returns the total number of items.
func (v *listPostsView) GetItemCount() int {
	return len(v.cardlist.Items())
}

// DisplayItems returns all list items.
func (v *listPostsView) DisplayItems() []tuicore.DisplayItem {
	return v.cardlist.Items()
}

// SetDisplayItems replaces all list items.
func (v *listPostsView) SetDisplayItems(items []tuicore.DisplayItem) {
	v.cardlist.SetItems(items)
	if v.restoreID != "" {
		v.cardlist.SelectByID(v.restoreID)
		v.restoreID = ""
	}
}

// SelectedDisplayItem returns the currently selected item.
func (v *listPostsView) SelectedDisplayItem() (tuicore.DisplayItem, bool) {
	return v.cardlist.SelectedItem()
}

// SearchInList opens search with the list scope prefilled.
func (v *listPostsView) SearchInList() tea.Cmd {
	query := "list:" + v.list.ID
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocSearchQuery(query),
			Action:   tuicore.NavPush,
		}
	}
}
