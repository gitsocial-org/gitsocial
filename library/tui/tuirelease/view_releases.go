// view_releases.go - Release list view
package tuirelease

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/extensions/release"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// ReleasesView displays a list of releases.
type ReleasesView struct {
	workdir      string
	workspaceURL string
	userEmail    string
	width        int
	height       int
	loaded       bool
	showEmail    bool
	allReleases  []release.Release
	cardList     *tuicore.CardList
	searchActive bool
	searchInput  textinput.Model
	searchQuery  string
	pag          tuicore.Pagination
	restoreIndex int // cursor position to restore after refresh (-1 = none)
}

// NewReleasesView creates a new releases view.
func NewReleasesView(workdir string) *ReleasesView {
	searchInput := textinput.New()
	searchInput.Placeholder = "Filter releases..."
	searchInput.CharLimit = 100
	searchInput.Prompt = "/ "
	tuicore.StyleTextInput(&searchInput, tuicore.Title, tuicore.Title, tuicore.Dim)
	return &ReleasesView{
		workdir:      workdir,
		userEmail:    git.GetUserEmail(workdir),
		cardList:     tuicore.NewCardList(nil),
		searchInput:  searchInput,
		restoreIndex: -1,
	}
}

// SetSize sets the view dimensions.
func (v *ReleasesView) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.cardList.SetSize(w, h-2)
}

// Activate loads the releases.
func (v *ReleasesView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	v.searchActive = false
	v.searchQuery = ""
	v.searchInput.SetValue("")
	v.cardList.SetCardOptions(tuicore.CardOptions{
		MaxLines:  2,
		ShowStats: false,
		Separator: true,
	})
	v.pag.Reset()
	return v.loadReleases()
}

// Deactivate is called when the view is hidden.
func (v *ReleasesView) Deactivate() {}

// navigateToSelected navigates to the selected release's detail view.
func (v *ReleasesView) navigateToSelected() tea.Cmd {
	item, ok := v.cardList.SelectedItem()
	if !ok {
		return nil
	}
	releaseID := item.ItemID()
	items := v.cardList.Items()
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location:    tuicore.LocReleaseDetail(releaseID),
			Action:      tuicore.NavPush,
			SourcePath:  "/release/list",
			SourceIndex: v.cardList.Selected(),
			SourceTotal: v.pag.Total(len(items)),
		}
	}
}

// GetItemAt returns the item ID at the given index.
func (v *ReleasesView) GetItemAt(index int) (string, bool) {
	items := v.cardList.Items()
	if index >= 0 && index < len(items) {
		return items[index].ItemID(), true
	}
	return "", false
}

// GetItemCount returns the total number of items.
func (v *ReleasesView) GetItemCount() int {
	return len(v.cardList.Items())
}

// Update handles messages.
func (v *ReleasesView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case releasesLoadedMsg:
		v.pag.Loading = false
		v.loaded = true
		if msg.err != nil {
			return nil
		}
		v.pag.HasMore = msg.hasMore
		v.pag.SetTotal(msg.total)
		if msg.append {
			v.allReleases = append(v.allReleases, msg.releases...)
			newItems := make([]tuicore.DisplayItem, len(msg.releases))
			for i, rel := range msg.releases {
				newItems[i] = tuicore.NewItem(rel.ID, "release", "release", rel.Timestamp, releaseItemData{
					Release:      rel,
					ShowEmail:    v.showEmail,
					UserEmail:    v.userEmail,
					WorkspaceURL: v.workspaceURL,
				})
			}
			v.cardList.AppendItems(newItems)
		} else {
			v.allReleases = msg.releases
			v.applyFilter()
			if v.restoreIndex >= 0 {
				v.cardList.SetSelected(v.restoreIndex)
				v.restoreIndex = -1
			}
		}
		if len(msg.releases) > 0 {
			v.pag.Cursor = msg.releases[len(msg.releases)-1].Timestamp.Format(time.RFC3339)
		}
		return nil
	case tea.KeyPressMsg, tea.MouseMsg:
		if key, ok := msg.(tea.KeyPressMsg); ok {
			if v.searchActive {
				return v.handleSearchKey(key)
			}
		} else if v.searchActive {
			return nil
		}
		consumed, activate, link := v.cardList.Update(msg)
		if link != nil {
			return func() tea.Msg { return tuicore.NavigateMsg{Location: *link, Action: tuicore.NavPush} }
		}
		if activate {
			return v.navigateToSelected()
		}
		if consumed {
			if v.cardList.NearBottom() && v.pag.CanLoadMore() {
				return tea.Batch(tuicore.ConsumedCmd, v.loadMoreReleases())
			}
			return tuicore.ConsumedCmd
		}
		if key, ok := msg.(tea.KeyPressMsg); ok {
			return v.handleKey(key)
		}
	default:
		if v.searchActive {
			var cmd tea.Cmd
			v.searchInput, cmd = v.searchInput.Update(msg)
			return cmd
		}
	}
	return nil
}

// IsInputActive returns true when search input is active.
func (v *ReleasesView) IsInputActive() bool {
	return v.searchActive
}

func (v *ReleasesView) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "N":
		return func() tea.Msg {
			return tuicore.NavigateMsg{
				Location: tuicore.LocReleaseNew,
				Action:   tuicore.NavPush,
			}
		}
	case "r":
		v.restoreIndex = v.cardList.Selected()
		v.pag.ResetForRefresh(len(v.cardList.Items()))
		return v.loadReleases()
	case "/":
		v.searchActive = true
		v.searchInput.SetValue(v.searchQuery)
		return v.searchInput.Focus()
	}
	return nil
}

func (v *ReleasesView) handleSearchKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		v.searchActive = false
		v.searchQuery = ""
		v.searchInput.SetValue("")
		v.searchInput.Blur()
		v.applyFilter()
		return nil
	case "enter":
		v.searchQuery = v.searchInput.Value()
		v.searchActive = false
		v.searchInput.Blur()
		v.applyFilter()
		return nil
	}
	var cmd tea.Cmd
	v.searchInput, cmd = v.searchInput.Update(msg)
	v.searchQuery = v.searchInput.Value()
	v.applyFilter()
	return cmd
}

// applyFilter filters releases by search query and updates the card list.
func (v *ReleasesView) applyFilter() {
	filtered := v.allReleases
	if v.searchQuery != "" {
		pattern := tuicore.CompileSearchPattern(v.searchQuery)
		var matched []release.Release
		for _, rel := range filtered {
			if pattern != nil && (pattern.MatchString(rel.Subject) || pattern.MatchString(rel.Version) || pattern.MatchString(rel.Tag)) {
				matched = append(matched, rel)
			}
		}
		filtered = matched
	}
	v.cardList.SetCardOptions(tuicore.CardOptions{
		MaxLines:      2,
		ShowStats:     false,
		Separator:     true,
		HighlightText: v.searchQuery,
	})
	items := make([]tuicore.DisplayItem, len(filtered))
	for i, rel := range filtered {
		items[i] = tuicore.NewItem(rel.ID, "release", "release", rel.Timestamp, releaseItemData{
			Release:      rel,
			ShowEmail:    v.showEmail,
			UserEmail:    v.userEmail,
			WorkspaceURL: v.workspaceURL,
		})
	}
	v.cardList.SetItems(items)
}

// Render renders the view.
func (v *ReleasesView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)

	var content string
	if !v.loaded {
		content = "Loading releases..."
	} else if len(v.cardList.Items()) == 0 {
		content = tuicore.Dim.Render("  No releases found")
	} else {
		v.cardList.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
		content = v.cardList.View()
	}

	var footer string
	if v.searchActive {
		v.searchInput.SetWidth(wrapper.ContentWidth() - 5)
		footer = v.searchInput.View()
	} else {
		footer = tuicore.RenderFooter(state.Registry, tuicore.ReleaseList, wrapper.ContentWidth(), nil)
	}
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *ReleasesView) Title() string {
	title := "⏏  Releases"
	if v.searchQuery != "" {
		title += fmt.Sprintf(" · /%s", v.searchQuery)
	}
	items := v.cardList.Items()
	if len(items) > 0 {
		return fmt.Sprintf("%s · %d/%s", title, v.cardList.Selected()+1, v.pag.TotalDisplay(len(items)))
	}
	return title
}

// HeaderInfo returns position info for the title.
func (v *ReleasesView) HeaderInfo() (position int, total string) {
	items := v.cardList.Items()
	if len(items) == 0 {
		return 0, ""
	}
	return v.cardList.Selected() + 1, v.pag.TotalDisplay(len(items))
}

// Bindings returns keybindings for this view.
func (v *ReleasesView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	lfsPush := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartLFSPush == nil {
			return false, nil
		}
		return true, ctx.StartLFSPush()
	}
	return []tuicore.Binding{
		{Key: "N", Label: "new", Contexts: []tuicore.Context{tuicore.ReleaseList}, Handler: noop},
		{Key: "r", Label: "refresh", Contexts: []tuicore.Context{tuicore.ReleaseList}, Handler: noop},
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.ReleaseList}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.ReleaseList}, Handler: push},
		{Key: "L", Label: "push lfs", Contexts: []tuicore.Context{tuicore.ReleaseList}, Handler: lfsPush},
	}
}

type releasesLoadedMsg struct {
	releases []release.Release
	hasMore  bool
	append   bool
	total    int
	err      error
}

func (v *ReleasesView) loadReleases() tea.Cmd {
	v.pag.StartLoading()
	workdir := v.workdir
	limit := v.pag.Limit()
	return func() tea.Msg {
		repoURL := gitmsg.ResolveRepoURL(workdir)
		v.workspaceURL = repoURL
		branch := gitmsg.GetExtBranch(workdir, "release")

		res := release.GetReleases(repoURL, branch, "", limit+1)
		if !res.Success {
			return releasesLoadedMsg{err: fmt.Errorf("%s", res.Error.Message)}
		}
		releases, hasMore := tuicore.TrimPage(res.Data, limit)
		total, _ := release.CountReleases(repoURL, branch)
		return releasesLoadedMsg{releases: releases, hasMore: hasMore, total: total}
	}
}

func (v *ReleasesView) loadMoreReleases() tea.Cmd {
	v.pag.StartLoading()
	workdir := v.workdir
	cursor := v.pag.Cursor
	return func() tea.Msg {
		repoURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "release")

		res := release.GetReleases(repoURL, branch, cursor, tuicore.PageSize+1)
		if !res.Success {
			return releasesLoadedMsg{err: fmt.Errorf("%s", res.Error.Message)}
		}
		releases, hasMore := tuicore.TrimPage(res.Data, tuicore.PageSize)
		return releasesLoadedMsg{releases: releases, hasMore: hasMore, append: true}
	}
}

// LoadMorePosts implements the loadMoreHandler interface for infinite scroll.
func (v *ReleasesView) LoadMorePosts() tea.Cmd {
	return v.pag.LoadMore(v.loadMoreReleases)
}
