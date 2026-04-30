// post.go - Post detail view with thread comments and actions
package tuisocial

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/social"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

const (
	maxThreadDepth = 8
	indentPerLevel = 4
)

var noopCmd tea.Cmd = func() tea.Msg { return nil }

type matchLocation struct {
	postIndex int
	matchNum  int
}

// PostView displays a post detail with thread.
type PostView struct {
	post              social.Post
	thread            []social.Post
	width             int
	height            int
	selectedIndex     int
	prevSelectedIndex int // to detect selection changes for auto-scroll
	scrollOffset      int
	postStartLines    []int // cached line positions for each post
	postEndLines      []int // cached line positions for each post
	showRaw           bool
	showEmail         bool
	loading           bool
	loadErr           error
	requestedPostID   string
	workdir           string
	userEmail         string
	sourceIndex       int
	sourceTotal       int
	searchActive      bool
	searchInputMode   bool
	searchInput       textinput.Model
	searchQuery       string
	highlightQuery    string // from navigation source (e.g. global search)
	matchIndex        int
	matchCount        int
	matches           []matchLocation
	diffStats         *git.DiffStats
	confirm           tuicore.ConfirmDialog
	zonePrefix        string
	focusedLink       int
	linkZones         []tuicore.CardLinkZone
}

// Bindings returns keybindings for the post view.
func (v *PostView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "c", Label: "comment", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: noop},
		{Key: "y", Label: "repost", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: noop},
		{Key: "e", Label: "edit", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				item, ok := ctx.Panel.GetSelectedDisplayItem()
				if !ok {
					return false, nil
				}
				post, ok := ItemToPost(item)
				if !ok || !post.Display.IsWorkspacePost {
					return false, nil
				}
				return true, ctx.Panel.EditPost()
			}},
		{Key: "X", Label: "retract", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				item, ok := ctx.Panel.GetSelectedDisplayItem()
				if !ok {
					return false, nil
				}
				post, ok := ItemToPost(item)
				if !ok || !post.Display.IsWorkspacePost {
					return false, nil
				}
				return true, ctx.Panel.RetractPost()
			}},
		{Key: "h", Label: "history", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				item, ok := ctx.Panel.GetSelectedDisplayItem()
				if !ok {
					return false, nil
				}
				post, ok := ItemToPost(item)
				if !ok || !post.IsEdited {
					return false, nil
				}
				return true, ctx.Panel.ShowHistory()
			}},
		{Key: "d", Label: "diff", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: noop},
		{Key: "v", Label: "raw", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: tuicore.RawViewHandler},
		{Key: "r", Label: "repository", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: noop},
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				return false, nil // Handled by view's handleKey
			}},
		{Key: "n", Label: "next match", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: noop},
		{Key: "N", Label: "prev match", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: noop},
		{Key: "left", Label: "prev", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: noop},
		{Key: "right", Label: "next", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: noop},
		{Key: "j", Label: "scroll down", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: noop},
		{Key: "k", Label: "scroll up", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: noop},
		{Key: "ctrl+d", Label: "half-page down", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: noop},
		{Key: "ctrl+u", Label: "half-page up", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: noop},
		{Key: "home", Label: "top", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: noop},
		{Key: "end", Label: "bottom", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: noop},
		{Key: "enter", Label: "activate", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.Detail, tuicore.Thread}, Handler: push},
	}
}

// NewPostView creates a new post detail view.
func NewPostView(workdir string) *PostView {
	input := textinput.New()
	input.Placeholder = "Search in thread..."
	input.CharLimit = 100
	input.Prompt = "> "
	tuicore.StyleTextInput(&input, tuicore.Title, tuicore.Title, tuicore.Dim)
	return &PostView{
		workdir:     workdir,
		width:       80,
		height:      20,
		searchInput: input,
		zonePrefix:  zone.NewPrefix(),
		focusedLink: -1,
	}
}

// SetUserEmail sets the user email for display.
func (v *PostView) SetUserEmail(email string) {
	v.userEmail = email
}

// SetSize sets the view dimensions.
func (v *PostView) SetSize(width, height int) {
	v.width = width
	v.height = height - 3 // -3 for footer
}

// Activate loads the thread when the view becomes active.
func (v *PostView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	v.searchInputMode = false
	v.searchActive = false
	v.focusedLink = -1
	v.linkZones = nil
	v.diffStats = nil
	v.confirm.Reset()
	loc := state.Router.Location()
	postID := loc.Param("postID")
	// Capture source context for left/right navigation
	if state.DetailSource != nil {
		v.sourceIndex = state.DetailSource.Index
		v.sourceTotal = state.DetailSource.Total
		if state.DetailSource.SearchQuery != "" {
			v.highlightQuery = ExtractSearchTerms(state.DetailSource.SearchQuery)
		} else {
			v.highlightQuery = ""
		}
	} else {
		v.sourceIndex = 0
		v.sourceTotal = 0
		v.highlightQuery = ""
	}
	if postID == "" {
		v.post = social.Post{}
		v.thread = nil
		v.loading = false
		v.loadErr = fmt.Errorf("no post ID in location")
		return nil
	}
	// Only reset if navigating to a different post
	if postID != v.requestedPostID {
		v.post = social.Post{}
		v.thread = nil
		v.loadErr = nil
		v.loading = true
		v.selectedIndex = 0
		v.scrollOffset = 0
	}
	v.requestedPostID = postID
	return v.loadThread(postID)
}

// loadThread fetches the thread for the given post ID.
func (v *PostView) loadThread(postID string) tea.Cmd {
	workdir := v.workdir
	return func() tea.Msg {
		result := social.GetPosts(workdir, "thread:"+postID, nil)
		if !result.Success {
			return ThreadLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return ThreadLoadedMsg{Posts: result.Data}
	}
}

// Update handles messages and returns commands.
func (v *PostView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if v.searchInputMode || v.confirm.IsActive() {
			return nil
		}
		switch msg.(type) {
		case tea.MouseWheelMsg:
			m := msg.Mouse()
			if m.Button == tea.MouseWheelUp {
				if v.canScrollUp() {
					v.scrollOffset -= v.scrollStep()
					if v.scrollOffset < 0 {
						v.scrollOffset = 0
					}
				} else if v.selectedIndex > 0 {
					v.selectedIndex--
				}
			} else {
				if v.canScrollDown() {
					v.scrollOffset += v.scrollStep()
				} else if v.selectedIndex < len(v.thread)-1 {
					v.selectedIndex++
				}
			}
			return nil
		case tea.MouseClickMsg:
			if loc := tuicore.LinkZoneClicked(msg, v.linkZones); loc != nil {
				navLoc := *loc
				return func() tea.Msg {
					return tuicore.NavigateMsg{Location: navLoc, Action: tuicore.NavPush}
				}
			}
			idx := tuicore.ZoneClicked(msg, len(v.thread), v.zonePrefix)
			if idx < 0 {
				return nil
			}
			if idx == v.selectedIndex {
				return v.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter}, state)
			}
			v.selectedIndex = idx
			v.focusedLink = -1
			return nil
		}
	case tea.KeyPressMsg:
		return v.handleKey(msg, state)
	case ThreadLoadedMsg:
		v.handleThreadLoaded(msg)
		return v.loadDiffStats()
	case diffStatsLoadedMsg:
		v.diffStats = msg.stats
	}
	if v.searchInputMode {
		var cmd tea.Cmd
		v.searchInput, cmd = v.searchInput.Update(msg)
		return cmd
	}
	return nil
}

// handleKey processes keyboard input.
func (v *PostView) handleKey(msg tea.KeyPressMsg, state *tuicore.State) tea.Cmd {
	key := msg.String()
	if handled, cmd := v.confirm.HandleKey(key); handled {
		return cmd
	}
	if v.searchInputMode {
		switch key {
		case "esc":
			v.exitSearch()
			return noopCmd
		case "enter":
			v.searchInputMode = false
			v.searchInput.Blur()
			if v.searchQuery == "" {
				v.searchActive = false
			}
			return noopCmd
		default:
			var cmd tea.Cmd
			v.searchInput, cmd = v.searchInput.Update(msg)
			v.updateLiveSearch()
			return cmd
		}
	}
	if v.searchActive {
		switch key {
		case "n":
			v.nextMatch()
			return noopCmd
		case "N":
			v.prevMatch()
			return noopCmd
		case "/":
			v.searchInputMode = true
			return v.searchInput.Focus()
		case "esc":
			v.exitSearch()
			return noopCmd
		}
		return noopCmd
	}
	switch key {
	case "esc":
		if v.focusedLink >= 0 {
			v.focusedLink = -1
			return noopCmd
		}
		if v.highlightQuery != "" {
			v.highlightQuery = ""
			v.searchQuery = ""
			return noopCmd
		}
	case "j", "down":
		if v.canScrollDown() {
			v.scrollOffset += v.scrollStep()
		} else if v.selectedIndex < len(v.thread)-1 {
			v.selectedIndex++
			v.focusedLink = -1
		}
	case "k", "up":
		if v.canScrollUp() {
			v.scrollOffset -= v.scrollStep()
			if v.scrollOffset < 0 {
				v.scrollOffset = 0
			}
		} else if v.selectedIndex > 0 {
			v.selectedIndex--
			v.focusedLink = -1
		}
	case "ctrl+d", "pgdown":
		v.scrollOffset += v.height / 2
	case "ctrl+u", "pgup":
		v.scrollOffset -= v.height / 2
		if v.scrollOffset < 0 {
			v.scrollOffset = 0
		}
	case "home":
		v.selectedIndex = 0
		v.scrollOffset = 0
		v.focusedLink = -1
	case "end":
		if len(v.thread) > 0 {
			v.selectedIndex = len(v.thread) - 1
			v.focusedLink = -1
		}
	case ";":
		v.cycleLinkForward()
		return noopCmd
	case ",":
		v.cycleLinkBackward()
		return noopCmd
	case "enter":
		if loc := v.focusedLinkLocation(); loc != nil {
			navLoc := *loc
			return func() tea.Msg {
				return tuicore.NavigateMsg{Location: navLoc, Action: tuicore.NavPush}
			}
		}
		if v.selectedIndex >= 0 && v.selectedIndex < len(v.thread) {
			selected := v.thread[v.selectedIndex]
			if selected.ID != v.requestedPostID {
				item := tuicore.NewItem(selected.ID, "social", string(selected.Type), selected.Timestamp, selected)
				if selected.HeaderExt != "" && selected.HeaderExt != "social" {
					item.OriginalExt = selected.HeaderExt
					item.OriginalType = selected.HeaderType
				}
				location := tuicore.GetNavTarget(item)
				return func() tea.Msg {
					return tuicore.NavigateMsg{
						Location: location,
						Action:   tuicore.NavPush,
					}
				}
			}
		}
	case "left":
		return v.navigateSource(state, -1)
	case "right":
		return v.navigateSource(state, 1)
	case "c":
		return v.openComment()
	case "y":
		return v.openRepost()
	case "r":
		return v.openRepository()
	case "d":
		return v.openDiff()
	case "/":
		v.searchActive = true
		v.searchInputMode = true
		v.searchInput.SetValue("")
		v.searchInput.Placeholder = ""
		return v.searchInput.Focus()
	}
	return nil
}

// navigateSource navigates to adjacent items in the source list.
func (v *PostView) navigateSource(state *tuicore.State, offset int) tea.Cmd {
	if state.DetailSource == nil {
		return nil
	}
	return func() tea.Msg {
		return tuicore.SourceNavigateMsg{Offset: offset, MakeLocation: tuicore.LocDetail}
	}
}

// openComment opens the editor to create a comment on the selected post.
func (v *PostView) openComment() tea.Cmd {
	if v.selectedIndex < 0 || v.selectedIndex >= len(v.thread) {
		return nil
	}
	selected := v.thread[v.selectedIndex]
	if selected.ID == "" {
		return nil
	}
	return func() tea.Msg {
		return tuicore.OpenEditorMsg{
			Mode:     "comment",
			TargetID: selected.ID,
		}
	}
}

// openRepost opens the editor to create a repost of the selected post.
func (v *PostView) openRepost() tea.Cmd {
	if v.selectedIndex < 0 || v.selectedIndex >= len(v.thread) {
		return nil
	}
	selected := v.thread[v.selectedIndex]
	if selected.ID == "" {
		return nil
	}
	return func() tea.Msg {
		return tuicore.OpenEditorMsg{
			Mode:     "repost",
			TargetID: selected.ID,
		}
	}
}

// openRepository navigates to the repository view for the selected post.
func (v *PostView) openRepository() tea.Cmd {
	if v.selectedIndex < 0 || v.selectedIndex >= len(v.thread) {
		return nil
	}
	selected := v.thread[v.selectedIndex]
	if selected.Repository == "" {
		return nil
	}
	if selected.Display.IsWorkspacePost {
		return func() tea.Msg {
			return tuicore.NavigateMsg{
				Location: tuicore.LocMyRepo,
				Action:   tuicore.NavPush,
			}
		}
	}
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocRepository(selected.Repository, selected.Branch),
			Action:   tuicore.NavPush,
		}
	}
}

// showHistory navigates to the edit history view.
func (v *PostView) showHistory() tea.Cmd {
	if v.post.ID == "" || !v.post.IsEdited {
		return nil
	}
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocHistory(v.post.ID),
			Action:   tuicore.NavPush,
		}
	}
}

type diffStatsLoadedMsg struct {
	stats *git.DiffStats
}

// loadDiffStats fires a command to load diff stats for workspace posts.
func (v *PostView) loadDiffStats() tea.Cmd {
	if v.post.Display.CommitHash == "" || !v.post.Display.IsWorkspacePost {
		return nil
	}
	workdir := v.workdir
	commit := v.post.Display.CommitHash
	return func() tea.Msg {
		stats, err := git.GetDiffStats(workdir, commit+"^", commit)
		if err != nil {
			return diffStatsLoadedMsg{}
		}
		return diffStatsLoadedMsg{stats: &stats}
	}
}

// openDiff navigates to the commit diff view for the selected post.
func (v *PostView) openDiff() tea.Cmd {
	if v.selectedIndex < 0 || v.selectedIndex >= len(v.thread) {
		return nil
	}
	selected := v.thread[v.selectedIndex]
	if selected.Display.CommitHash == "" || !selected.Display.IsWorkspacePost {
		return nil
	}
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocCommitDiff(selected.Display.CommitHash),
			Action:   tuicore.NavPush,
		}
	}
}

// ShowHistory implements historyViewer interface for registry handler.
func (v *PostView) ShowHistory() tea.Cmd {
	return v.showHistory()
}

// ShowRawView toggles between rendered body and full commit message.
func (v *PostView) ShowRawView() tea.Cmd {
	v.showRaw = !v.showRaw
	return noopCmd
}

// exitSearch clears and exits search mode.
func (v *PostView) exitSearch() {
	v.searchActive = false
	v.searchInputMode = false
	v.searchInput.Blur()
	// Restore navigation highlight if present, otherwise clear
	if v.highlightQuery != "" {
		v.searchQuery = v.highlightQuery
	} else {
		v.searchQuery = ""
	}
	v.matchIndex = 0
	v.matchCount = 0
	v.matches = nil
}

// updateLiveSearch updates search results as the user types.
func (v *PostView) updateLiveSearch() {
	v.searchQuery = v.searchInput.Value()
	if v.searchQuery == "" {
		v.matches = nil
		v.matchIndex = 0
		v.matchCount = 0
		return
	}
	v.buildMatchLocations()
	v.matchCount = len(v.matches)
	if v.matchCount == 0 {
		v.matchIndex = 0
	} else {
		v.matchIndex = 1
		v.selectedIndex = v.matches[0].postIndex
	}
}

// nextMatch moves to the next search match.
func (v *PostView) nextMatch() {
	if v.matchCount == 0 {
		return
	}
	v.matchIndex++
	if v.matchIndex > v.matchCount {
		v.matchIndex = 1
	}
	v.selectedIndex = v.matches[v.matchIndex-1].postIndex
}

// prevMatch moves to the previous search match.
func (v *PostView) prevMatch() {
	if v.matchCount == 0 {
		return
	}
	v.matchIndex--
	if v.matchIndex < 1 {
		v.matchIndex = v.matchCount
	}
	v.selectedIndex = v.matches[v.matchIndex-1].postIndex
}

// buildMatchLocations builds the list of match locations in the thread.
func (v *PostView) buildMatchLocations() {
	v.matches = nil
	if v.searchQuery == "" {
		return
	}
	pattern := tuicore.CompileSearchPattern(v.searchQuery)
	if pattern == nil {
		return
	}
	for postIdx, p := range v.thread {
		matches := pattern.FindAllStringIndex(p.Content, -1)
		for matchNum := range matches {
			v.matches = append(v.matches, matchLocation{
				postIndex: postIdx,
				matchNum:  matchNum,
			})
		}
	}
}

// handleThreadLoaded processes the loaded thread data.
func (v *PostView) handleThreadLoaded(msg ThreadLoadedMsg) {
	v.loading = false
	if msg.Err != nil {
		v.loadErr = msg.Err
		return
	}
	v.thread = msg.Posts
	v.scrollOffset = 0
	v.prevSelectedIndex = -1 // Force auto-scroll on first render
	// Find anchor index for initial selection (thread contains [parents...] + [anchor] + [children...])
	for i, p := range msg.Posts {
		if p.ID == v.requestedPostID {
			v.post = p
			v.selectedIndex = i
			if v.highlightQuery != "" {
				v.searchQuery = v.highlightQuery
			}
			return
		}
	}
	if len(msg.Posts) > 0 {
		v.loadErr = fmt.Errorf("requested post not found in thread: %s", v.requestedPostID)
	} else {
		v.loadErr = fmt.Errorf("thread empty for: %s", v.requestedPostID)
	}
}

// Render renders the post view to a string.
func (v *PostView) Render(state *tuicore.State) string {
	if v.post.IsRetracted {
		state.BorderVariant = "warning"
	}
	wrapper := tuicore.NewViewWrapper(state)

	var content string
	if v.loadErr != nil {
		content = tuicore.Dim.Render(fmt.Sprintf("Error: %s", v.loadErr.Error()))
	} else if v.post.ID == "" {
		if v.loading {
			content = tuicore.Dim.Render("Loading...")
		} else {
			content = tuicore.Dim.Render("No post selected")
		}
	} else {
		content = v.renderContent()
	}

	var footer string
	if v.searchActive {
		footer = v.searchInput.View() + "\n" + v.renderSearchFooter(wrapper.ContentWidth())
	} else if v.confirm.IsActive() {
		footer = v.confirm.Render()
	} else {
		exclude := map[string]bool{
			"n": true,
			"N": true,
		}
		if !v.post.IsEdited {
			exclude["h"] = true
		}
		if !v.post.Display.IsWorkspacePost {
			exclude["e"] = true
			exclude["D"] = true
			exclude["d"] = true
		}
		if v.post.Display.CommitHash == "" {
			exclude["d"] = true
		}
		footer = tuicore.RenderFooterWithPosition(state.Registry, tuicore.Detail, wrapper.ContentWidth(), v.sourceIndex+1, v.sourceTotal, exclude)
	}
	return wrapper.Render(content, footer)
}

// renderSearchFooter renders the search mode footer.
func (v *PostView) renderSearchFooter(width int) string {
	return tuicore.RenderSearchFooter(width, v.matchIndex, v.matchCount, v.searchInputMode, v.searchQuery != "")
}

// renderContent renders the thread with cards.
func (v *PostView) renderContent() string {
	resolver := func(postID string) (social.Post, bool) {
		for _, p := range v.thread {
			if p.ID == postID {
				return p, true
			}
		}
		return social.Post{}, false
	}
	// Find anchor index
	anchorIdx := 0
	for i, p := range v.thread {
		if p.ID == v.requestedPostID {
			anchorIdx = i
			break
		}
	}
	// Track line positions for each post (cached for scroll helpers)
	v.postStartLines = make([]int, len(v.thread))
	v.postEndLines = make([]int, len(v.thread))
	v.linkZones = v.linkZones[:0]
	var allLines []string
	itemAnchors := make([]*tuicore.AnchorCollector, 0, len(v.thread))
	for i, p := range v.thread {
		v.postStartLines[i] = len(allLines)
		isParent := i < anchorIdx
		isAnchor := i == anchorIdx
		isChild := i > anchorIdx
		isSelected := i == v.selectedIndex
		// SkipNested: only show nested cards for reposts (parent/anchor), always skip for children
		skipNested := true
		if !isChild && p.Type == social.PostTypeRepost {
			skipNested = false
		}
		card := PostToCardWithOptions(p, resolver, PostToCardOptions{
			SkipNested: skipNested,
			UserEmail:  v.userEmail,
			ShowEmail:  v.showEmail,
		})
		if isAnchor && v.diffStats != nil && (v.diffStats.Added > 0 || v.diffStats.Removed > 0) {
			card.Header.Subtitle = append(card.Header.Subtitle, tuicore.HeaderPart{
				Text: fmt.Sprintf("±%d %s", v.diffStats.Files, tuicore.RenderDiffStatsBadge(v.diffStats.Added, v.diffStats.Removed)),
			})
		}
		// Determine indent for children based on depth
		indent := ""
		availableWidth := v.width - 6
		if isChild {
			depth := p.Depth
			if depth > maxThreadDepth {
				depth = maxThreadDepth
			}
			if depth >= 1 {
				indent = strings.Repeat(" ", depth*indentPerLevel)
				availableWidth = v.width - 6 - len(indent)
			}
		}
		// Create anchor collector for the selected post's links
		var anchors *tuicore.AnchorCollector
		if isSelected {
			focused := v.focusedLink
			anchors = tuicore.NewAnchorCollector(fmt.Sprintf("%s_link_%d", v.zonePrefix, i), focused)
		}
		var commitMsg string
		if v.showRaw {
			ref := protocol.ParseRef(p.ID)
			if ref.Value != "" {
				if c, err := cache.GetCommit(ref.Repository, ref.Value, ref.Branch); err == nil && c.Message != "" {
					commitMsg = fmt.Sprintf("%s\n%s\n%s\n\n%s",
						tuicore.Dim.Render("commit "+c.Hash),
						tuicore.Dim.Render(fmt.Sprintf("Author: %s <%s>", c.AuthorName, c.AuthorEmail)),
						tuicore.Dim.Render("Date:   "+c.Timestamp.Format("Mon Jan 2 15:04:05 2006 -0700")),
						c.Message)
				}
			}
		}
		opts := tuicore.CardOptions{
			MaxLines:      v.getMaxLines(isParent, isAnchor),
			ShowStats:     true,
			Selected:      isSelected,
			Width:         v.width,
			WrapWidth:     availableWidth,
			Dimmed:        isParent,
			Bold:          isAnchor && !v.showRaw,
			Indent:        indent,
			Markdown:      !v.showRaw,
			Raw:           v.showRaw,
			CommitMessage: commitMsg,
			HighlightText: v.searchQuery,
			Anchors:       anchors,
		}
		// Separator before anchor (if there are parents)
		if isAnchor && anchorIdx > 0 {
			allLines = append(allLines, tuicore.RenderSectionSeparator(v.width), "")
		}
		cardLines := strings.Split(tuicore.RenderCard(card, opts), "\n")
		allLines = append(allLines, cardLines...)
		itemAnchors = append(itemAnchors, anchors)
		// Blank line between parents
		if isParent {
			allLines = append(allLines, "")
		}
		// Separator after anchor
		if isAnchor {
			allLines = append(allLines, "", tuicore.RenderSectionSeparator(v.width), "")
		}
		// Dim separator between children
		if isChild && i < len(v.thread)-1 {
			nextDepth := v.thread[i+1].Depth
			allLines = append(allLines, "", tuicore.RenderItemSeparator(v.width, nextDepth), "")
		}
		v.postEndLines[i] = len(allLines)
	}
	// Auto-scroll ONLY when selection changed (not during manual scroll within same post)
	selectionChanged := v.selectedIndex != v.prevSelectedIndex
	v.prevSelectedIndex = v.selectedIndex
	if selectionChanged && v.selectedIndex >= 0 && v.selectedIndex < len(v.thread) {
		selStart := v.postStartLines[v.selectedIndex]
		selEnd := v.postEndLines[v.selectedIndex]
		postHeight := selEnd - selStart
		if selStart < v.scrollOffset {
			// Post starts above viewport - scroll up to show start
			v.scrollOffset = selStart
		} else if selStart >= v.scrollOffset+v.height {
			// Post starts below viewport - scroll down to show start
			v.scrollOffset = selStart
		} else if postHeight <= v.height && selEnd > v.scrollOffset+v.height {
			// Post fits in viewport but end is cut off - scroll to show end
			v.scrollOffset = selEnd - v.height
		}
		// If post is taller than viewport and start is visible, don't scroll
	}
	// Clamp scroll offset
	maxScroll := len(allLines) - v.height
	if maxScroll < 0 {
		maxScroll = 0
	}
	if v.scrollOffset > maxScroll {
		v.scrollOffset = maxScroll
	}
	if v.scrollOffset < 0 {
		v.scrollOffset = 0
	}
	// Extract visible lines and mark zones
	endLine := v.scrollOffset + v.height
	if endLine > len(allLines) {
		endLine = len(allLines)
	}
	visibleLines := allLines[v.scrollOffset:endLine]
	for i := range v.postStartLines {
		if i >= len(v.postEndLines) {
			break
		}
		// Find range of visible lines for this post
		startVis := -1
		endVis := -1
		for lineIdx := v.postStartLines[i]; lineIdx < v.postEndLines[i]; lineIdx++ {
			visIdx := lineIdx - v.scrollOffset
			if visIdx >= 0 && visIdx < len(visibleLines) {
				if startVis < 0 {
					startVis = visIdx
				}
				endVis = visIdx + 1
			}
		}
		if startVis < 0 {
			continue
		}
		// Collect link zones from visible items
		if i < len(itemAnchors) && itemAnchors[i] != nil {
			v.linkZones = append(v.linkZones, itemAnchors[i].Zones()...)
		}
		// Wrap all visible lines in a single zone (matching CardList pattern)
		joined := strings.Join(visibleLines[startVis:endVis], "\n")
		trimmed := strings.TrimRight(joined, "\n")
		trailing := len(joined) - len(trimmed)
		marked := tuicore.MarkZone(tuicore.ZoneID(v.zonePrefix, i), trimmed)
		if trailing > 0 {
			marked += strings.Repeat("\n", trailing)
		}
		copy(visibleLines[startVis:endVis], strings.Split(marked, "\n"))
	}
	return strings.Join(visibleLines, "\n")
}

// getMaxLines returns the max content lines based on position.
func (v *PostView) getMaxLines(isParent, _ bool) int {
	if isParent {
		return 5
	}
	return -1 // unlimited for anchor and children
}

// selectedPostLinks returns the links for the currently selected thread post.
func (v *PostView) selectedPostLinks() []tuicore.CardLink {
	if v.selectedIndex < 0 || v.selectedIndex >= len(v.thread) {
		return nil
	}
	p := v.thread[v.selectedIndex]
	resolver := func(postID string) (social.Post, bool) {
		for _, tp := range v.thread {
			if tp.ID == postID {
				return tp, true
			}
		}
		return social.Post{}, false
	}
	card := PostToCardWithOptions(p, resolver, PostToCardOptions{
		SkipNested: true,
		UserEmail:  v.userEmail,
		ShowEmail:  v.showEmail,
	})
	return card.AllLinks()
}

// cycleLinkForward advances to the next link on the selected post.
func (v *PostView) cycleLinkForward() {
	links := v.selectedPostLinks()
	if len(links) == 0 {
		return
	}
	v.focusedLink++
	if v.focusedLink >= len(links) {
		v.focusedLink = -1
	}
}

// cycleLinkBackward moves to the previous link on the selected post.
func (v *PostView) cycleLinkBackward() {
	links := v.selectedPostLinks()
	if len(links) == 0 {
		return
	}
	v.focusedLink--
	if v.focusedLink < -1 {
		v.focusedLink = len(links) - 1
	}
}

// focusedLinkLocation returns the Location of the focused link, if any.
func (v *PostView) focusedLinkLocation() *tuicore.Location {
	if v.focusedLink < 0 {
		return nil
	}
	links := v.selectedPostLinks()
	if v.focusedLink >= len(links) {
		return nil
	}
	loc := links[v.focusedLink].Location
	return &loc
}

// canScrollDown returns true if current post extends below viewport.
func (v *PostView) canScrollDown() bool {
	if v.selectedIndex < 0 || v.selectedIndex >= len(v.postEndLines) {
		return false
	}
	selEnd := v.postEndLines[v.selectedIndex]
	return selEnd > v.scrollOffset+v.height
}

// canScrollUp returns true if current post extends above viewport.
func (v *PostView) canScrollUp() bool {
	if v.selectedIndex < 0 || v.selectedIndex >= len(v.postStartLines) {
		return false
	}
	selStart := v.postStartLines[v.selectedIndex]
	return selStart < v.scrollOffset
}

// scrollStep returns the number of lines to scroll per keypress.
func (v *PostView) scrollStep() int {
	return 3
}

// IsInputActive returns true when search input or confirm is active.
func (v *PostView) IsInputActive() bool {
	return v.searchInputMode || v.confirm.IsActive()
}

// Post returns the current post for editing/retraction.
func (v *PostView) Post() social.Post {
	return v.post
}

// SelectedDisplayItem returns the main post as a DisplayItem for keybinding handlers.
func (v *PostView) SelectedDisplayItem() (tuicore.DisplayItem, bool) {
	if v.post.ID == "" {
		return nil, false
	}
	return tuicore.NewItem(v.post.ID, "social", string(v.post.Type), v.post.Timestamp, v.post), true
}

// DisplayItems returns a single-element slice with the main post.
func (v *PostView) DisplayItems() []tuicore.DisplayItem {
	if v.post.ID == "" {
		return nil
	}
	item, _ := v.SelectedDisplayItem()
	return []tuicore.DisplayItem{item}
}

// SetDisplayItems is a no-op for detail views.
func (v *PostView) SetDisplayItems(_ []tuicore.DisplayItem) {}

// EditPost opens the editor to edit the current post.
func (v *PostView) EditPost() tea.Cmd {
	if v.post.ID == "" {
		return nil
	}
	return func() tea.Msg {
		return tuicore.OpenEditorMsg{
			Mode:           "edit",
			TargetID:       v.post.ID,
			InitialContent: v.post.Content,
		}
	}
}

// RetractPost shows confirmation prompt before retracting.
func (v *PostView) RetractPost() tea.Cmd {
	if v.post.ID == "" {
		return nil
	}
	v.confirm.Show("Retract this post?", false, func() tea.Cmd { return v.doRetract() })
	return nil
}

// doRetract executes the retraction of the current post.
func (v *PostView) doRetract() tea.Cmd {
	postID := v.post.ID
	workdir := v.workdir
	return tea.Sequence(
		func() tea.Msg { return RetractStartedMsg{} },
		func() tea.Msg {
			result := social.RetractPost(workdir, postID)
			if !result.Success {
				return PostRetractedMsg{PostID: postID, Err: fmt.Errorf("%s", result.Error.Message)}
			}
			return PostRetractedMsg{PostID: postID}
		},
	)
}

// Title returns the view header with author, timestamp, and repo link.
func (v *PostView) Title() string {
	if v.post.ID == "" {
		return "Thread"
	}
	icon := "•"
	switch v.post.Type {
	case social.PostTypeRepost:
		icon = "↻"
	case social.PostTypeQuote:
		icon = "↻"
	case social.PostTypeComment:
		icon = "↩"
	}
	switch v.post.HeaderExt {
	case "pm":
		switch v.post.HeaderType {
		case "issue":
			if v.post.HeaderState == "closed" || v.post.HeaderState == "canceled" {
				icon = "●"
			} else {
				icon = "○"
			}
		case "milestone":
			icon = "◇"
		case "sprint":
			icon = "◷"
		}
	case "review":
		if v.post.HeaderType == "pull-request" {
			icon = "⑂"
		}
	case "release":
		icon = "⏏"
	}
	if v.post.Display.IsUnpushed {
		icon += "  ⇡"
	}
	name := v.post.Author.Name
	if name == "" {
		name = "Anonymous"
	}
	if v.showEmail && v.post.Author.Email != "" {
		name += " <" + v.post.Author.Email + ">"
	}
	title := icon + "  " + name
	title += " · " + tuicore.FormatFullTime(v.post.Timestamp)
	if ref := tuicore.BuildRef(v.post.ID, v.post.Repository, v.post.Branch, v.post.Display.IsWorkspacePost); ref != "" {
		title += " · " + ref
	}
	return title
}

// HeaderInfo returns position and total for the header display.
func (v *PostView) HeaderInfo() (position int, total string) {
	if v.sourceTotal > 0 {
		return v.sourceIndex + 1, fmt.Sprintf("%d", v.sourceTotal)
	}
	return 0, ""
}
