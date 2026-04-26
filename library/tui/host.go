// host.go - View host that manages routing, state, and cross-view coordination
package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// View capability interfaces - used for type assertions on views

type (
	headerInfoProvider interface {
		HeaderInfo() (position int, total string)
	}

	searchHeaderProvider interface {
		headerInfoProvider
		TotalSearched() int
	}

	titleProvider interface {
		Title() string
	}

	listProvider interface {
		GetSelectedList() *tuicore.SelectedList
	}

	externalListChecker interface {
		IsExternalList() bool
	}

	postEditor interface {
		EditPost() tea.Cmd
		RetractPost() tea.Cmd
	}

	historyViewer interface {
		ShowHistory() tea.Cmd
	}

	rawViewer interface {
		ShowRawView() tea.Cmd
	}

	listHandler interface {
		CreateList() tea.Cmd
		DeleteList() tea.Cmd
	}

	listViewToggler interface {
		ToggleListView() tea.Cmd
	}

	loadMoreHandler interface {
		LoadMorePosts() tea.Cmd
	}

	notificationHandler interface {
		MarkNotificationRead() tea.Cmd
		MarkAllNotificationsRead() tea.Cmd
		MarkNotificationUnread() tea.Cmd
		MarkAllNotificationsUnread() tea.Cmd
		ToggleNotificationFilter() tea.Cmd
	}

	cacheHandler interface {
		RefreshCache() tea.Cmd
		ClearCacheDB() tea.Cmd
		ClearCacheRepos() tea.Cmd
		ClearCacheForks() tea.Cmd
		ClearCacheAll() tea.Cmd
	}

	repositoryFollower interface {
		FollowRepository() tea.Cmd
	}

	repositoryListsOpener interface {
		OpenRepoLists() tea.Cmd
	}

	repositorySearcher interface {
		SearchInRepository() tea.Cmd
	}

	listSearcher interface {
		SearchInList() tea.Cmd
	}

	// displayItemsProvider provides universal item access.
	displayItemsProvider interface {
		DisplayItems() []tuicore.DisplayItem
		SetDisplayItems([]tuicore.DisplayItem)
		SelectedDisplayItem() (tuicore.DisplayItem, bool)
	}

	// displayItemUpdater provides universal item update support.
	displayItemUpdater interface {
		UpdateItem(item tuicore.DisplayItem)
		RemoveItem(itemID string)
	}

	cacheReloader interface {
		ReloadCacheIfActive() tea.Cmd
	}
)

// ClearMessageMsg signals that a timed message should be cleared
type ClearMessageMsg struct {
	ID int
}

// Host manages views and shared state.
// It dispatches messages to the active view and coordinates cross-view updates.
type Host struct {
	state          *tuicore.State
	views          map[string]tuicore.View
	sortedPatterns []string // pre-sorted view patterns (longest first) for prefix matching
	// Frame cache — avoids renderFrame() when inner content unchanged
	frameCache struct {
		content  string
		title    string
		focused  bool
		border   string
		width    int
		height   int
		rendered string
	}
}

// NewHost creates a new view host with shared state.
func NewHost(state *tuicore.State) *Host {
	return &Host{
		state: state,
		views: make(map[string]tuicore.View),
	}
}

// AddView registers a view for a URL pattern.
func (h *Host) AddView(pattern string, view tuicore.View) {
	h.views[pattern] = view
	if bp, ok := view.(tuicore.BindingProvider); ok {
		h.state.Registry.RegisterView(bp)
	}
	// Rebuild sorted patterns for prefix matching (longest first)
	h.sortedPatterns = make([]string, 0, len(h.views))
	for p := range h.views {
		h.sortedPatterns = append(h.sortedPatterns, p)
	}
	sort.Slice(h.sortedPatterns, func(i, j int) bool {
		return len(h.sortedPatterns[i]) > len(h.sortedPatterns[j])
	})
}

// State returns the shared state.
func (h *Host) State() *tuicore.State {
	return h.state
}

// SetSize sets the host dimensions and propagates to views.
func (h *Host) SetSize(width, height int) {
	h.state.Width = width
	h.state.Height = height

	innerW := h.state.InnerWidth()
	innerH := h.state.InnerHeight()
	for _, view := range h.views {
		if sizable, ok := view.(tuicore.Sizable); ok {
			sizable.SetSize(innerW, innerH)
		}
	}
}

// SetFocused sets the focus state of the host.
func (h *Host) SetFocused(focused bool) {
	h.state.Focused = focused
}

// Update delegates message handling to the active view.
func (h *Host) Update(msg tea.Msg) tea.Cmd {
	view := h.resolveView()
	if view != nil {
		return view.Update(msg, h.state)
	}
	// No view found — handle ESC to navigate back instead of getting stuck
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		if keyMsg.String() == "esc" {
			return h.GoBack()
		}
	}
	return nil
}

// View renders the active view with frame.
func (h *Host) View() string {
	h.state.BorderVariant = ""
	view := h.resolveView()
	var content, title string
	if view != nil {
		content = view.Render(h.state)
		title = h.currentTitleFor(view)
	} else {
		content = "No view available"
	}
	fc := &h.frameCache
	if content == fc.content && title == fc.title && h.state.Focused == fc.focused &&
		h.state.BorderVariant == fc.border && h.state.Width == fc.width && h.state.Height == fc.height {
		return fc.rendered
	}
	fc.content = content
	fc.title = title
	fc.focused = h.state.Focused
	fc.border = h.state.BorderVariant
	fc.width = h.state.Width
	fc.height = h.state.Height
	fc.rendered = h.renderFrame(title, content)
	return fc.rendered
}

// ActivateView activates the current view based on router location.
func (h *Host) ActivateView() tea.Cmd {
	view := h.resolveView()
	if view != nil {
		if activator, ok := view.(tuicore.ViewActivator); ok {
			return activator.Activate(h.state)
		}
	}
	return nil
}

// RefreshView reloads data in the current view without resetting view state.
// Falls back to ActivateView if the view doesn't implement ViewRefresher.
func (h *Host) RefreshView() tea.Cmd {
	view := h.resolveView()
	if view != nil {
		if refresher, ok := view.(tuicore.ViewRefresher); ok {
			return refresher.Refresh(h.state)
		}
		if activator, ok := view.(tuicore.ViewActivator); ok {
			return activator.Activate(h.state)
		}
	}
	return nil
}

// CurrentContext returns the context for the current view.
func (h *Host) CurrentContext() tuicore.Context {
	loc := h.state.Router.Location()
	// Special case: /social/repository with no URL param is MyRepository
	if loc.Is("/social/repository") && loc.Param("url") == "" {
		return tuicore.MyRepository
	}
	return tuicore.GetContextForPath(loc.Path)
}

// currentTitleFor builds the title for the given view.
func (h *Host) currentTitleFor(view tuicore.View) string {
	loc := h.state.Router.Location()
	meta, hasMeta := tuicore.GetViewMeta(loc.Path)
	// Special case: /config with extension param
	if loc.Is("/config") {
		ext := loc.Param("extension")
		icon := "※"
		extName := "Core"
		switch ext {
		case "social":
			icon = "⌘"
			extName = "Social"
		case "pm":
			icon = "▢"
			extName = "PM"
		}
		return tuicore.SafeIcon(icon) + "  " + extName + " Configuration"
	}
	// Try view's Title() method first for dynamic titles
	if view != nil {
		if tp, ok := view.(titleProvider); ok {
			if title := tp.Title(); title != "" {
				return title
			}
		}
	}
	// Special case: /social/list/repos appends "repositories"
	if loc.Is("/social/list/repos") {
		title := "List"
		if view != nil {
			if tp, ok := view.(titleProvider); ok {
				if t := tp.Title(); t != "" {
					title = t
				}
			}
		}
		return title + " · repositories"
	}
	// Build title from registry metadata
	if !hasMeta {
		return ""
	}
	var parts []string
	baseTitle := meta.Title
	if icon := tuicore.SafeIcon(meta.Icon); icon != "" {
		baseTitle = icon + "  " + meta.Title
	}
	parts = append(parts, baseTitle)
	// Add position info if view provides it
	if view != nil {
		if provider, ok := view.(headerInfoProvider); ok {
			pos, total := provider.HeaderInfo()
			if total != "" {
				if searchProvider, ok := view.(searchHeaderProvider); ok {
					if totalSearched := searchProvider.TotalSearched(); totalSearched > 0 {
						parts = append(parts, fmt.Sprintf("%d/%s of %d", pos, total, totalSearched))
					} else {
						parts = append(parts, fmt.Sprintf("%d/%s", pos, total))
					}
				} else {
					parts = append(parts, fmt.Sprintf("%d/%s", pos, total))
				}
			}
		}
	}
	// Add fetch info for views that show it
	var dimParts []string
	if meta.ShowFetch {
		if !h.state.LastFetchTime.IsZero() {
			dimParts = append(dimParts, tuicore.FormatTime(h.state.LastFetchTime))
		}
		if h.state.NewItemCount > 0 {
			dimParts = append(dimParts, fmt.Sprintf("+%d new", h.state.NewItemCount))
		}
	}
	result := strings.Join(parts, " · ")
	if len(dimParts) > 0 {
		result += " · " + tuicore.Dim.Render(strings.Join(dimParts, " · "))
	}
	return result
}

// IsInputActive returns true if the current view has active input.
func (h *Host) IsInputActive() bool {
	view := h.resolveView()
	if view != nil {
		if handler, ok := view.(tuicore.InputHandler); ok {
			return handler.IsInputActive()
		}
	}
	return false
}

// resolveView finds the view matching the current route.
func (h *Host) resolveView() tuicore.View {
	loc := h.state.Router.Location()
	path := loc.Path

	if view, ok := h.views[path]; ok {
		return view
	}

	for _, pattern := range h.sortedPatterns {
		if strings.HasPrefix(path, pattern) {
			return h.views[pattern]
		}
	}

	return nil
}

// Pre-computed border and title styles — avoids lipgloss.NewStyle() per frame.
var (
	frameBorderStyles = map[string]lipgloss.Style{
		tuicore.BorderFocused:   lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.BorderFocused)),
		tuicore.BorderUnfocused: lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.BorderUnfocused)),
		tuicore.BorderError:     lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.BorderError)),
		tuicore.BorderWarning:   lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.BorderWarning)),
	}
	frameTitleStyles = map[string]lipgloss.Style{
		tuicore.BorderFocused:   lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.BorderFocused)).Bold(true),
		tuicore.BorderUnfocused: lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.BorderUnfocused)),
		tuicore.BorderError:     lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.BorderError)),
		tuicore.BorderWarning:   lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.BorderWarning)),
	}
)

// renderFrame renders content within a bordered frame.
func (h *Host) renderFrame(title, content string) string {
	borderColor := tuicore.BorderUnfocused
	if h.state.Focused {
		borderColor = tuicore.BorderFocused
	}
	switch h.state.BorderVariant {
	case "error":
		borderColor = tuicore.BorderError
	case "warning":
		borderColor = tuicore.BorderWarning
	}

	borderStyle := frameBorderStyles[borderColor]
	titleStyle := frameTitleStyles[borderColor]
	if h.state.Focused && borderColor != tuicore.BorderFocused {
		titleStyle = titleStyle.Bold(true)
	}

	innerWidth := h.state.Width - 2
	innerHeight := h.state.Height - 2

	// Pre-render border segments once — avoids ~80 redundant style.Render() calls
	// per frame. lipgloss v2 uses grapheme cluster segmentation (UAX#29) per
	// Render() call, so reducing call count matters.
	borderV := borderStyle.Render("│")
	emptyRow := borderV + strings.Repeat(" ", innerWidth) + borderV

	var lines []string
	var topLine string
	if title != "" {
		titleRendered := titleStyle.Render(title)
		titleLen := tuicore.AnsiWidth(title)
		rightPadLen := innerWidth - titleLen - 3
		if rightPadLen < 0 {
			rightPadLen = 0
		}
		topLine = borderStyle.Render("╭─ ") + titleRendered + borderStyle.Render(" "+strings.Repeat("─", rightPadLen)+"╮")
	} else {
		topLine = borderStyle.Render("╭" + strings.Repeat("─", innerWidth) + "╮")
	}
	lines = append(lines, topLine)

	for i := 0; i < tuicore.ContentPaddingTop; i++ {
		lines = append(lines, emptyRow)
	}

	contentLines := strings.Split(content, "\n")
	leftPad := strings.Repeat(" ", tuicore.ContentPaddingLeft)
	contentHeight := innerHeight - tuicore.ContentPaddingTop
	for i := 0; i < contentHeight && i < len(contentLines); i++ {
		line := contentLines[i]
		lineWidth := tuicore.AnsiWidth(line)
		rightPad := innerWidth - lineWidth - tuicore.ContentPaddingLeft - tuicore.ContentPaddingRight
		if rightPad < 0 {
			rightPad = 0
		}
		lines = append(lines, borderV+leftPad+line+strings.Repeat(" ", rightPad+tuicore.ContentPaddingRight)+borderV)
	}

	for i := len(contentLines); i < contentHeight; i++ {
		lines = append(lines, emptyRow)
	}

	bottomLine := borderStyle.Render("╰" + strings.Repeat("─", innerWidth) + "╯")
	lines = append(lines, bottomLine)

	return strings.Join(lines, "\n")
}

// View delegation methods

// GetSelectedDisplayItem returns the selected item from the current view (extension-agnostic).
func (h *Host) GetSelectedDisplayItem() (tuicore.DisplayItem, bool) {
	if view := h.resolveView(); view != nil {
		if provider, ok := view.(displayItemsProvider); ok {
			return provider.SelectedDisplayItem()
		}
	}
	return nil, false
}

// GetSelectedList returns the selected list from the current view.
func (h *Host) GetSelectedList() *tuicore.SelectedList {
	if view := h.resolveView(); view != nil {
		if provider, ok := view.(listProvider); ok {
			return provider.GetSelectedList()
		}
	}
	return nil
}

// IsExternalList returns true if viewing an external list.
func (h *Host) IsExternalList() bool {
	if view := h.resolveView(); view != nil {
		if checker, ok := view.(externalListChecker); ok {
			return checker.IsExternalList()
		}
	}
	return false
}

// GoBack navigates to the previous view in history.
func (h *Host) GoBack() tea.Cmd {
	if !h.state.Router.Back() {
		h.state.Router.Replace(tuicore.LocTimeline)
	}
	return h.ActivateView()
}

// OpenRepository navigates to a repository view.
func (h *Host) OpenRepository(url, branch string) tea.Cmd {
	h.state.Router.Push(tuicore.LocRepository(url, branch))
	return h.ActivateView()
}

// OpenMyRepo navigates to the workspace repository view.
func (h *Host) OpenMyRepo() tea.Cmd {
	h.state.Router.Push(tuicore.LocMyRepo)
	return h.ActivateView()
}

// OpenLists navigates to the lists view.
func (h *Host) OpenLists() tea.Cmd {
	h.state.Router.Push(tuicore.LocLists)
	return h.ActivateView()
}

// EditPost delegates post editing to the current view.
func (h *Host) EditPost() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if editor, ok := view.(postEditor); ok {
			return editor.EditPost()
		}
	}
	return nil
}

// RetractPost delegates post retraction to the current view.
func (h *Host) RetractPost() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if editor, ok := view.(postEditor); ok {
			return editor.RetractPost()
		}
	}
	return nil
}

// ShowHistory delegates history view to the current view.
func (h *Host) ShowHistory() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if viewer, ok := view.(historyViewer); ok {
			return viewer.ShowHistory()
		}
	}
	return nil
}

// ShowRawView delegates raw view toggle to the current view.
func (h *Host) ShowRawView() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if viewer, ok := view.(rawViewer); ok {
			return viewer.ShowRawView()
		}
	}
	return nil
}

// CreateList delegates list creation to the current view.
func (h *Host) CreateList() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(listHandler); ok {
			return handler.CreateList()
		}
	}
	return nil
}

// DeleteList delegates list deletion to the current view.
func (h *Host) DeleteList() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(listHandler); ok {
			return handler.DeleteList()
		}
	}
	return nil
}

// LoadMorePosts delegates loading more posts to the current view.
func (h *Host) LoadMorePosts() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(loadMoreHandler); ok {
			return handler.LoadMorePosts()
		}
	}
	return nil
}

// ToggleListView delegates list view toggle to the current view.
func (h *Host) ToggleListView() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if toggler, ok := view.(listViewToggler); ok {
			return toggler.ToggleListView()
		}
	}
	return nil
}

// MarkNotificationRead delegates marking notification read to the current view.
func (h *Host) MarkNotificationRead() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(notificationHandler); ok {
			return handler.MarkNotificationRead()
		}
	}
	return nil
}

// MarkAllNotificationsRead delegates marking all notifications read.
func (h *Host) MarkAllNotificationsRead() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(notificationHandler); ok {
			return handler.MarkAllNotificationsRead()
		}
	}
	return nil
}

// MarkNotificationUnread delegates marking notification unread to the current view.
func (h *Host) MarkNotificationUnread() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(notificationHandler); ok {
			return handler.MarkNotificationUnread()
		}
	}
	return nil
}

// MarkAllNotificationsUnread delegates marking all notifications unread.
func (h *Host) MarkAllNotificationsUnread() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(notificationHandler); ok {
			return handler.MarkAllNotificationsUnread()
		}
	}
	return nil
}

// ToggleNotificationFilter delegates notification filter toggle.
func (h *Host) ToggleNotificationFilter() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(notificationHandler); ok {
			return handler.ToggleNotificationFilter()
		}
	}
	return nil
}

// RefreshCache delegates cache refresh to the current view.
func (h *Host) RefreshCache() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(cacheHandler); ok {
			return handler.RefreshCache()
		}
	}
	return nil
}

// ClearCacheDB delegates clearing the cache database.
func (h *Host) ClearCacheDB() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(cacheHandler); ok {
			return handler.ClearCacheDB()
		}
	}
	return nil
}

// ClearCacheRepos delegates clearing the cached repositories.
func (h *Host) ClearCacheRepos() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(cacheHandler); ok {
			return handler.ClearCacheRepos()
		}
	}
	return nil
}

// ClearCacheForks delegates clearing the fork bare repos.
func (h *Host) ClearCacheForks() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(cacheHandler); ok {
			return handler.ClearCacheForks()
		}
	}
	return nil
}

// ClearCacheAll delegates clearing all cache data.
func (h *Host) ClearCacheAll() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(cacheHandler); ok {
			return handler.ClearCacheAll()
		}
	}
	return nil
}

// FollowRepository delegates repository following to the current view.
func (h *Host) FollowRepository() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(repositoryFollower); ok {
			return handler.FollowRepository()
		}
	}
	return nil
}

// OpenRepoLists delegates opening repository lists to the current view.
func (h *Host) OpenRepoLists() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(repositoryListsOpener); ok {
			return handler.OpenRepoLists()
		}
	}
	return nil
}

// SearchInRepository delegates repository search to the current view.
func (h *Host) SearchInRepository() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(repositorySearcher); ok {
			return handler.SearchInRepository()
		}
	}
	return nil
}

// SearchInList delegates list search to the current view.
func (h *Host) SearchInList() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if handler, ok := view.(listSearcher); ok {
			return handler.SearchInList()
		}
	}
	return nil
}

// Cross-view data methods

// DisplayItems returns all items from the current view.
func (h *Host) DisplayItems() []tuicore.DisplayItem {
	if view := h.resolveView(); view != nil {
		if provider, ok := view.(displayItemsProvider); ok {
			return provider.DisplayItems()
		}
	}
	return nil
}

// SetDisplayItems updates items in the current view (extension-agnostic).
func (h *Host) SetDisplayItems(items []tuicore.DisplayItem) {
	if view := h.resolveView(); view != nil {
		if provider, ok := view.(displayItemsProvider); ok {
			provider.SetDisplayItems(items)
		}
	}
}

// SelectedDisplayItem returns the selected item from the current view (extension-agnostic).
func (h *Host) SelectedDisplayItem() (tuicore.DisplayItem, bool) {
	if view := h.resolveView(); view != nil {
		if provider, ok := view.(displayItemsProvider); ok {
			return provider.SelectedDisplayItem()
		}
	}
	return nil, false
}

// UpdateDisplayItem propagates an item update to all views (extension-agnostic).
func (h *Host) UpdateDisplayItem(item tuicore.DisplayItem) {
	for _, view := range h.views {
		if updater, ok := view.(displayItemUpdater); ok {
			updater.UpdateItem(item)
		}
	}
}

// RemoveDisplayItem propagates an item removal to all views.
func (h *Host) RemoveDisplayItem(itemID string) {
	for _, view := range h.views {
		if updater, ok := view.(displayItemUpdater); ok {
			updater.RemoveItem(itemID)
		}
	}
}

// ReloadCacheIfActive reloads cache if the cache view is active.
func (h *Host) ReloadCacheIfActive() tea.Cmd {
	if view := h.resolveView(); view != nil {
		if reloader, ok := view.(cacheReloader); ok {
			return reloader.ReloadCacheIfActive()
		}
	}
	return nil
}

// State setters

// SetFetchStatus updates the fetch status in state.
func (h *Host) SetFetchStatus(fetchTime time.Time, newItems int) {
	h.state.LastFetchTime = fetchTime
	h.state.NewItemCount = newItems
}

// SetFetchingInfo sets the fetch progress info.
func (h *Host) SetFetchingInfo(repos, lists int) {
	h.state.FetchRepos = repos
	h.state.FetchLists = lists
}

// SetSyncing sets the syncing state.
func (h *Host) SetSyncing(syncing bool) {
	h.state.Syncing = syncing
}

// SetFetching sets the fetching state.
func (h *Host) SetFetching(fetching bool) {
	h.state.Fetching = fetching
}

// SetPushing sets the pushing state.
func (h *Host) SetPushing(pushing bool) {
	h.state.Pushing = pushing
}

// SetPushingInfo sets the push remote info.
func (h *Host) SetPushingInfo(remote string) {
	h.state.PushRemote = remote
}

// SetSaving sets the saving state.
func (h *Host) SetSaving(saving bool) {
	h.state.Saving = saving
}

// SetRetracting sets the retracting state.
func (h *Host) SetRetracting(retracting bool) {
	h.state.Retracting = retracting
}

// SetMessage sets a status message with type.
// Increments MessageID to cancel any pending auto-clear timers.
func (h *Host) SetMessage(msg string, msgType tuicore.MessageType) {
	h.state.MessageID++
	h.state.Message = msg
	h.state.MessageType = msgType
}

// SetMessageWithTimeout sets a status message that auto-clears.
func (h *Host) SetMessageWithTimeout(msg string, msgType tuicore.MessageType, d time.Duration) tea.Cmd {
	h.state.MessageID++
	h.state.Message = msg
	h.state.MessageType = msgType
	id := h.state.MessageID
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return ClearMessageMsg{ID: id}
	})
}

// GetSourceItem gets a post from the source list with offset.
func (h *Host) GetSourceItem(offset int) (postID string, newIndex int, ok bool) {
	ctx := h.state.DetailSource
	if ctx == nil {
		return "", 0, false
	}
	view, exists := h.views[ctx.Path]
	if !exists {
		return "", 0, false
	}
	provider, isProvider := view.(tuicore.SourceListProvider)
	if !isProvider {
		return "", 0, false
	}
	newIndex = ctx.Index + offset
	if newIndex < 0 || newIndex >= provider.GetItemCount() {
		return "", 0, false
	}
	postID, ok = provider.GetItemAt(newIndex)
	return postID, newIndex, ok
}

// GetSourceDisplayItem gets a full DisplayItem from the source list with offset.
// Used for type-aware navigation when the source contains mixed item types (e.g., search).
func (h *Host) GetSourceDisplayItem(offset int) (tuicore.DisplayItem, int, bool) {
	ctx := h.state.DetailSource
	if ctx == nil {
		return nil, 0, false
	}
	view, exists := h.views[ctx.Path]
	if !exists {
		return nil, 0, false
	}
	provider, isProvider := view.(tuicore.SourceListProvider)
	if !isProvider {
		return nil, 0, false
	}
	newIndex := ctx.Index + offset
	if newIndex < 0 || newIndex >= provider.GetItemCount() {
		return nil, 0, false
	}
	if dp, ok := view.(tuicore.DisplayItemProvider); ok {
		item, found := dp.GetDisplayItemAt(newIndex)
		if found {
			return item, newIndex, true
		}
	}
	return nil, 0, false
}

// UpdateSourceIndex updates the source context index.
func (h *Host) UpdateSourceIndex(index, total int) {
	if h.state.DetailSource != nil {
		h.state.DetailSource.Index = index
		h.state.DetailSource.Total = total
	}
}

// Layout calculates panel dimensions
type Layout struct {
	Width  int
	Height int

	NavWidth     int
	ContentWidth int
}

const navPanelWidth = 32

// NewLayout creates a new layout with the given dimensions.
func NewLayout(width, height int) Layout {
	navWidth := navPanelWidth
	if width < 80 {
		navWidth = 0
	}

	return Layout{
		Width:        width,
		Height:       height,
		NavWidth:     navWidth,
		ContentWidth: width - navWidth,
	}
}

// ShowNav returns true if nav panel should be shown.
func (l Layout) ShowNav() bool {
	return l.NavWidth > 0
}
