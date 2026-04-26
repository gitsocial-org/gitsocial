// repository.go - Repository view showing posts from a single repo
package tuisocial

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/social"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// RepositoryView displays posts from a specific repository.
type RepositoryView struct {
	name         string
	url          string
	branch       string
	isWorkspace  bool
	originURL    string
	lastFetch    time.Time
	cardlist     *tuicore.CardList
	workdir      string
	userEmail    string
	showEmail    bool
	cacheDir     string
	restoreIndex int // cursor position to restore after refresh (-1 = none)

	// Unfollowed repo fetch state
	isFetching    bool
	fetchingLabel string
	fetchedMonths []string

	// Follow status detection
	allLists    []social.List
	followerSet map[string]bool

	pag tuicore.Pagination
}

// Bindings returns keybindings for the repository view.
func (v *RepositoryView) Bindings() []tuicore.Binding {
	return []tuicore.Binding{
		{Key: "l", Label: "lists", Contexts: []tuicore.Context{tuicore.Repository},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.OpenRepoLists()
			}},
		{Key: "a", Label: "add to my lists", Contexts: []tuicore.Context{tuicore.Repository},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.FollowRepository()
			}},
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.Repository, tuicore.MyRepository},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.SearchInRepository()
			}},
		{Key: "[", Label: "older", Contexts: []tuicore.Context{tuicore.Repository},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				return false, nil // Handled by view
			}},
		{Key: "]", Label: "newer", Contexts: []tuicore.Context{tuicore.Repository},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				return false, nil // Handled by view
			}},
		{Key: "r", Label: "refresh", Contexts: []tuicore.Context{tuicore.Repository, tuicore.MyRepository},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				return false, nil // Handled by view
			}},
		{Key: "%", Label: "analytics", Contexts: []tuicore.Context{tuicore.Repository},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				repoURL := v.url
				if repoURL == "" {
					return false, nil
				}
				return true, func() tea.Msg {
					return tuicore.NavigateMsg{
						Location: tuicore.LocAnalyticsRepo(repoURL),
						Action:   tuicore.NavPush,
					}
				}
			}},
	}
}

// NewRepositoryView creates a new repository view.
func NewRepositoryView(workdir string) *RepositoryView {
	v := &RepositoryView{
		workdir:      workdir,
		restoreIndex: -1,
	}
	v.cardlist = tuicore.NewCardList(nil)
	v.cardlist.SetItemResolver(v.resolveItem)
	return v
}

// resolveItem fetches a post by ID via API.
func (v *RepositoryView) resolveItem(itemID string) (tuicore.DisplayItem, bool) {
	result := social.GetPosts(v.workdir, "post:"+itemID, nil)
	if result.Success && len(result.Data) > 0 {
		post := result.Data[0]
		post.Display.UserEmail = v.userEmail
		return tuicore.NewItem(post.ID, "social", string(post.Type), post.Timestamp, post), true
	}
	return nil, false
}

// SetUserEmail sets the user email for own-post highlighting.
func (v *RepositoryView) SetUserEmail(email string) {
	v.userEmail = email
}

// SetSize sets the view dimensions (receives inner content area).
func (v *RepositoryView) SetSize(width, height int) {
	v.cardlist.SetSize(width, height-3) // -3 for footer
}

// Activate loads repository posts when the view becomes active.
func (v *RepositoryView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	loc := state.Router.Location()
	url := loc.Param("url")
	branch := loc.Param("branch")
	v.cacheDir = state.CacheDir
	v.workdir = state.Workdir

	// Restore cursor position when returning from detail view
	if state.DetailSource != nil && state.DetailSource.Path == "/social/repository" {
		v.restoreIndex = state.DetailSource.Index
	} else {
		v.restoreIndex = -1
	}

	// Check if URL matches workspace origin
	originURL := git.GetOriginURL(state.Workdir)
	isOriginURL := url != "" && protocol.NormalizeURL(url) == protocol.NormalizeURL(originURL)

	if url == "" || loc.Is("/social/repository/my") || isOriginURL {
		// My repository
		v.isWorkspace = true
		v.url = originURL
		v.branch = gitmsg.GetExtBranch(state.Workdir, "social")
		v.originURL = v.url
		v.name = "My Repository"
		v.fetchedMonths = nil
		return v.loadPosts()
	}

	// Remote repository
	v.isWorkspace = false
	v.url = url
	v.branch = branch
	if v.branch == "" {
		v.branch = "main"
	}
	v.name = protocol.GetDisplayName(url)

	// Get last fetch time from cache
	if repo, err := cache.GetRepository(url); err == nil && repo.LastFetch.Valid {
		v.lastFetch, _ = time.Parse(time.RFC3339, repo.LastFetch.String)
	}

	// Load fetched months for this repo
	if months, err := cache.GetFetchedMonths(url); err == nil {
		v.fetchedMonths = months
	}

	// Load all lists for follow status detection
	if listsResult := social.GetLists(state.Workdir); listsResult.Success {
		v.allLists = listsResult.Data
	}

	// Load follower set for mutual follow detection
	workspaceURL := gitmsg.ResolveRepoURL(state.Workdir)
	var followerErr error
	v.followerSet, followerErr = social.GetFollowerSet(workspaceURL)
	if followerErr != nil {
		log.Debug("failed to get follower set", "error", followerErr)
	}

	// Check if unfollowed and has no cached data - auto-fetch
	isFollowed := cache.IsRepositoryInAnyList(url, state.Workdir)
	hasRanges := cache.HasFetchRanges(url)
	if !isFollowed && !hasRanges {
		return v.fetchInitialMonths(state)
	}

	return v.loadPosts()
}

// loadPosts fetches posts for the current repository. The total count is
// loaded asynchronously so the page render isn't blocked on a multi-second
// COUNT(*) over huge repos.
func (v *RepositoryView) loadPosts() tea.Cmd {
	v.pag.StartLoading()
	v.pag.Cursor = ""
	v.pag.HasMore = false
	workdir := v.workdir
	limit := v.pag.Limit()
	scope := "repository:workspace"
	if !v.isWorkspace {
		scope = "repository:" + v.url
		if v.branch != "" {
			scope += "@" + v.branch
		}
	}
	isWs := v.isWorkspace
	repoURL := v.url
	repoBranch := v.branch
	pageCmd := func() tea.Msg {
		result := social.GetPosts(workdir, scope, &social.GetPostsOptions{Limit: limit + 1})
		if !result.Success {
			return RepositoryLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		posts, hasMore := tuicore.TrimPage(result.Data, limit)
		return RepositoryLoadedMsg{Posts: posts, HasMore: hasMore}
	}
	countCmd := func() tea.Msg {
		return RepositoryCountLoadedMsg{Total: social.CountRepository(workdir, repoURL, repoBranch, isWs)}
	}
	return tea.Batch(pageCmd, countCmd)
}

// loadMorePosts fetches the next page of repository posts.
func (v *RepositoryView) loadMorePosts() tea.Cmd {
	v.pag.StartLoading()
	workdir := v.workdir
	cursor := v.pag.Cursor
	scope := "repository:workspace"
	if !v.isWorkspace {
		scope = "repository:" + v.url
		if v.branch != "" {
			scope += "@" + v.branch
		}
	}
	return func() tea.Msg {
		result := social.GetPosts(workdir, scope, &social.GetPostsOptions{Limit: tuicore.PageSize + 1, Cursor: cursor})
		if !result.Success {
			return RepositoryLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		posts, hasMore := tuicore.TrimPage(result.Data, tuicore.PageSize)
		return RepositoryLoadedMsg{Posts: posts, HasMore: hasMore, Append: true}
	}
}

// LoadMorePosts implements the loadMoreHandler interface for infinite scroll.
func (v *RepositoryView) LoadMorePosts() tea.Cmd {
	return v.pag.LoadMore(v.loadMorePosts)
}

// fetchInitialMonths fetches the initial months for unfollowed repos.
func (v *RepositoryView) fetchInitialMonths(state *tuicore.State) tea.Cmd {
	v.isFetching = true
	v.fetchingLabel = "Fetching posts..."
	cacheDir := state.CacheDir
	workdir := state.Workdir
	url := v.url
	branch := v.branch
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	return func() tea.Msg {
		months := social.InitialFetchMonths()
		var totalPosts int
		var fetchedMonths []string
		for _, m := range months {
			result := social.FetchRepositoryRange(cacheDir, url, branch, m.Start, m.End, workspaceURL)
			if result.Success {
				totalPosts += result.Data.Posts
				fetchedMonths = append(fetchedMonths, social.YearMonthFromRange(m))
			}
		}
		// Also fetch the repo's lists for exploration
		social.CacheExternalRepoLists(cacheDir, url, branch)
		return RepositoryFetchedMsg{Posts: totalPosts, Months: fetchedMonths}
	}
}

// fetchOlderMonth fetches posts from the previous month.
func (v *RepositoryView) fetchOlderMonth(state *tuicore.State) tea.Cmd {
	if v.isWorkspace || v.isFetching || len(v.fetchedMonths) == 0 {
		return nil
	}
	oldest := v.fetchedMonths[len(v.fetchedMonths)-1]
	m := social.PreviousMonthRange(oldest)
	v.isFetching = true
	v.fetchingLabel = "Fetching " + social.FormatMonthDisplay(social.YearMonthFromRange(m)) + "..."
	cacheDir := state.CacheDir
	workdir := state.Workdir
	url := v.url
	branch := v.branch
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	return func() tea.Msg {
		result := social.FetchRepositoryRange(cacheDir, url, branch, m.Start, m.End, workspaceURL)
		if !result.Success {
			return RepositoryFetchedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return RepositoryFetchedMsg{Posts: result.Data.Posts, Months: []string{social.YearMonthFromRange(m)}}
	}
}

// fetchNewerMonth fetches posts from the next month.
func (v *RepositoryView) fetchNewerMonth(state *tuicore.State) tea.Cmd {
	if v.isWorkspace || v.isFetching || len(v.fetchedMonths) == 0 {
		return nil
	}
	newest := v.fetchedMonths[0]
	currentMonth := social.CurrentYearMonth()
	if newest >= currentMonth {
		return nil // Already at current month
	}
	m := social.NextMonthRange(newest)
	ym := social.YearMonthFromRange(m)
	if ym > currentMonth {
		return nil // Don't fetch future months
	}
	// Check if already fetched
	for _, fetched := range v.fetchedMonths {
		if fetched == ym {
			return nil
		}
	}
	v.isFetching = true
	v.fetchingLabel = "Fetching " + social.FormatMonthDisplay(ym) + "..."
	cacheDir := state.CacheDir
	workdir := state.Workdir
	url := v.url
	branch := v.branch
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	return func() tea.Msg {
		result := social.FetchRepositoryRange(cacheDir, url, branch, m.Start, m.End, workspaceURL)
		if !result.Success {
			return RepositoryFetchedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return RepositoryFetchedMsg{Posts: result.Data.Posts, Months: []string{ym}}
	}
}

// Update handles messages and returns commands.
func (v *RepositoryView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg.(type) {
	case tea.KeyPressMsg, tea.MouseMsg:
		if v.isFetching {
			return nil
		}
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
			return v.handleKey(key, state)
		}
	default:
		switch msg := msg.(type) {
		case RepositoryLoadedMsg:
			v.handleLoaded(msg)
		case RepositoryCountLoadedMsg:
			v.pag.SetTotal(msg.Total)
		case RepositoryFetchedMsg:
			return v.handleFetched(msg)
		}
	}
	return nil
}

// handleFetched processes fetch completion and reloads posts.
func (v *RepositoryView) handleFetched(msg RepositoryFetchedMsg) tea.Cmd {
	v.isFetching = false
	v.fetchingLabel = ""
	if msg.Err != nil {
		return nil
	}
	// Merge new months into fetchedMonths (keep sorted desc)
	v.fetchedMonths = mergeMonths(v.fetchedMonths, msg.Months)
	return v.loadPosts()
}

// mergeMonths merges and sorts month strings in descending order.
func mergeMonths(existing, new []string) []string {
	seen := make(map[string]bool)
	for _, m := range existing {
		seen[m] = true
	}
	for _, m := range new {
		if !seen[m] {
			existing = append(existing, m)
			seen[m] = true
		}
	}
	// Sort descending
	for i := 0; i < len(existing)-1; i++ {
		for j := i + 1; j < len(existing); j++ {
			if existing[j] > existing[i] {
				existing[i], existing[j] = existing[j], existing[i]
			}
		}
	}
	return existing
}

// navigateToSelected navigates to the selected item's detail view.
func (v *RepositoryView) navigateToSelected() tea.Cmd {
	item, ok := v.cardlist.SelectedItem()
	if !ok {
		return nil
	}
	items := v.cardlist.Items()
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location:    tuicore.GetNavTarget(item),
			Action:      tuicore.NavPush,
			SourcePath:  "/social/repository",
			SourceIndex: v.cardlist.Selected(),
			SourceTotal: v.pag.Total(len(items)),
		}
	}
}

// handleKey processes view-specific keyboard input.
func (v *RepositoryView) handleKey(msg tea.KeyPressMsg, state *tuicore.State) tea.Cmd {
	switch msg.String() {
	case "r":
		v.restoreIndex = v.cardlist.Selected()
		v.pag.ResetForRefresh(len(v.cardlist.Items()))
		return v.loadPosts()
	case "[":
		if !v.isWorkspace {
			return v.fetchOlderMonth(state)
		}
	case "]":
		if !v.isWorkspace {
			return v.fetchNewerMonth(state)
		}
	}
	return nil
}

// handleLoaded processes the loaded repository posts.
func (v *RepositoryView) handleLoaded(msg RepositoryLoadedMsg) {
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

// Render renders the repository view to a string.
func (v *RepositoryView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)

	var content string
	if v.isFetching {
		content = tuicore.Dim.Render(v.fetchingLabel)
	} else if len(v.cardlist.Items()) == 0 {
		if len(v.fetchedMonths) > 0 && !cache.IsRepositoryInAnyList(v.url, v.workdir) {
			content = tuicore.Dim.Render("No posts in " + social.FormatMonthRangeDisplay(v.fetchedMonths))
		} else {
			content = tuicore.Dim.Render("No posts")
		}
	} else {
		content = v.cardlist.View()
	}

	ctx := tuicore.Repository
	if v.isWorkspace {
		ctx = tuicore.MyRepository
	}
	var footer string
	if v.isFetching {
		footer = tuicore.RenderMessageFooter(v.fetchingLabel, tuicore.MessageTypeNone, wrapper.ContentWidth())
	} else {
		status := GetFollowStatus(v.url, v.allLists, v.followerSet)
		isFollowed := v.isWorkspace || status == FollowStatusFollowed || status == FollowStatusMutual
		include := map[string]bool{"%": !v.isWorkspace}
		if isFollowed {
			exclude := map[string]bool{"[": true, "]": true}
			footer = tuicore.RenderFooterInclude(state.Registry, ctx, wrapper.ContentWidth(), exclude, include)
		} else {
			footer = tuicore.RenderFooterInclude(state.Registry, ctx, wrapper.ContentWidth(), nil, include)
		}
	}
	return wrapper.Render(content, footer)
}

// IsInputActive returns false since repository view has no text input.
func (v *RepositoryView) IsInputActive() bool {
	return false
}

// IsWorkspace returns true if viewing the workspace repository.
func (v *RepositoryView) IsWorkspace() bool {
	return v.isWorkspace
}

// URL returns the repository URL.
func (v *RepositoryView) URL() string {
	return v.url
}

// HeaderInfo returns position and total for the header.
func (v *RepositoryView) HeaderInfo() (position int, total string) {
	items := v.cardlist.Items()
	if len(items) == 0 {
		return 0, ""
	}
	return v.cardlist.Selected() + 1, v.pag.TotalDisplay(len(items))
}

// Title returns the fully formatted header for the repository view.
// Format: repo name · n/m · x ago · date range · [lists] · url · @branch
func (v *RepositoryView) Title() string {
	if v.isWorkspace {
		pos, total := v.HeaderInfo()
		if total != "" {
			return tuicore.MeTitle.Render("♥  " + v.name + " · " + fmt.Sprintf("%d/%s", pos, total))
		}
		return tuicore.MeTitle.Render("♥  " + v.name)
	}
	if v.name == "" {
		return "Repository"
	}
	status := GetFollowStatus(v.url, v.allLists, v.followerSet)
	listNames := GetListNamesForRepo(v.url, v.allLists, "")
	pos, total := v.HeaderInfo()
	var styledParts []string
	counter := ""
	if total != "" {
		counter = fmt.Sprintf("%d/%s", pos, total)
	}
	switch status {
	case FollowStatusMutual:
		styledParts = append(styledParts, tuicore.MutualTitle.Render("⎇  "+v.name))
		if counter != "" {
			styledParts = append(styledParts, tuicore.MutualTitle.Render(counter))
		}
	case FollowStatusFollowed:
		styledParts = append(styledParts, tuicore.Title.Render("⎇  ✓ "+v.name))
		if counter != "" {
			styledParts = append(styledParts, tuicore.Title.Render(counter))
		}
	default:
		styledParts = append(styledParts, "⎇  ☐ "+v.name)
		if counter != "" {
			styledParts = append(styledParts, counter)
		}
	}
	var dimParts []string
	if !v.lastFetch.IsZero() {
		dimParts = append(dimParts, tuicore.FormatTime(v.lastFetch))
	}
	if !v.isWorkspace && len(v.fetchedMonths) > 0 && !cache.IsRepositoryInAnyList(v.url, v.workdir) {
		dimParts = append(dimParts, social.FormatMonthRangeDisplay(v.fetchedMonths))
	}
	indicator := FormatListIndicator(listNames, 2)
	if indicator == "" && status != FollowStatusMutual && status != FollowStatusFollowed {
		if v.followerSet[protocol.NormalizeURL(v.url)] {
			indicator = "[follows you]"
		} else {
			indicator = "[not followed]"
		}
	}
	if indicator != "" {
		dimParts = append(dimParts, indicator)
	}
	if v.url != "" {
		branch := v.branch
		if branch == "" {
			branch = "main"
		}
		repoRef := protocol.CreateRef(protocol.RefTypeBranch, branch, v.url, "")
		treeURL := protocol.BranchURL(v.url, branch)
		dimParts = append(dimParts, tuicore.Hyperlink(treeURL, repoRef))
	}
	result := strings.Join(styledParts, " · ")
	if len(dimParts) > 0 {
		result += " · " + tuicore.Dim.Render(strings.Join(dimParts, " · "))
	}
	return result
}

// GetDisplayItemAt returns the full DisplayItem at the given index.
func (v *RepositoryView) GetDisplayItemAt(index int) (tuicore.DisplayItem, bool) {
	items := v.cardlist.Items()
	if index >= 0 && index < len(items) {
		return items[index], true
	}
	return nil, false
}

// GetItemAt returns the post ID at the given index.
func (v *RepositoryView) GetItemAt(index int) (string, bool) {
	items := v.cardlist.Items()
	if index >= 0 && index < len(items) {
		return items[index].ItemID(), true
	}
	return "", false
}

// GetItemCount returns the total number of items.
func (v *RepositoryView) GetItemCount() int {
	return len(v.cardlist.Items())
}

// DisplayItems returns all repository items.
func (v *RepositoryView) DisplayItems() []tuicore.DisplayItem {
	return v.cardlist.Items()
}

// SetDisplayItems replaces all repository items.
func (v *RepositoryView) SetDisplayItems(items []tuicore.DisplayItem) {
	v.cardlist.SetItems(items)
	if v.restoreIndex >= 0 {
		v.cardlist.SetSelected(v.restoreIndex)
		v.restoreIndex = -1
	}
}

// SelectedDisplayItem returns the currently selected item.
func (v *RepositoryView) SelectedDisplayItem() (tuicore.DisplayItem, bool) {
	return v.cardlist.SelectedItem()
}

// UpdateItem updates an item in the list by ID.
func (v *RepositoryView) UpdateItem(item tuicore.DisplayItem) {
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
func (v *RepositoryView) RemoveItem(itemID string) {
	items := v.cardlist.Items()
	for i, item := range items {
		if item.ItemID() == itemID {
			v.cardlist.SetItems(append(items[:i], items[i+1:]...))
			return
		}
	}
}

// FollowRepository opens the list picker to add this repository to a list.
func (v *RepositoryView) FollowRepository() tea.Cmd {
	if v.isWorkspace || v.url == "" {
		return nil
	}
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocListsWithRepo(v.url),
			Action:   tuicore.NavPush,
		}
	}
}

// OpenRepoLists opens the repository's defined lists.
func (v *RepositoryView) OpenRepoLists() tea.Cmd {
	if v.isWorkspace || v.url == "" {
		return nil
	}
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocRepoLists(v.url),
			Action:   tuicore.NavPush,
		}
	}
}

// SearchInRepository opens search with the repository scope prefilled.
func (v *RepositoryView) SearchInRepository() tea.Cmd {
	if !v.isWorkspace && v.url == "" {
		return nil
	}
	scope := v.url
	if v.isWorkspace {
		scope = "my"
	}
	query := "repository:" + scope
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocSearchQuery(query),
			Action:   tuicore.NavPush,
		}
	}
}
