// sprints.go - Sprints list view for PM
package tuipm

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// SprintsView displays a list of sprints.
type SprintsView struct {
	workdir        string
	workspaceURL   string
	repoURL        string
	branch         string
	isRemote       bool
	width          int
	height         int
	loaded         bool
	showAll        bool
	assigneeFilter string // "" = all, "me" = created by me
	userEmail      string
	showEmail      bool
	cardList       *tuicore.CardList
	allSprints     []pm.Sprint
	searchActive   bool
	searchInput    textinput.Model
	searchQuery    string
	pag            tuicore.Pagination
	restoreIndex   int // cursor position to restore after refresh (-1 = none)
}

// NewSprintsView creates a new sprints view.
func NewSprintsView(workdir string) *SprintsView {
	searchInput := textinput.New()
	searchInput.Placeholder = "Filter sprints..."
	searchInput.CharLimit = 100
	searchInput.Prompt = "/ "
	tuicore.StyleTextInput(&searchInput, tuicore.Title, tuicore.Title, tuicore.Dim)
	return &SprintsView{
		workdir:      workdir,
		userEmail:    git.GetUserEmail(workdir),
		cardList:     tuicore.NewCardList(nil),
		searchInput:  searchInput,
		restoreIndex: -1,
	}
}

// SetSize sets the view dimensions.
func (v *SprintsView) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.cardList.SetSize(w, h-3)
}

// Activate loads the sprints.
func (v *SprintsView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	v.searchActive = false
	v.searchQuery = ""
	v.searchInput.SetValue("")
	loc := state.Router.Location()
	url := loc.Param("url")
	branch := loc.Param("branch")
	v.workspaceURL = gitmsg.ResolveRepoURL(v.workdir)
	if url != "" {
		v.repoURL = url
		v.branch = branch
		v.isRemote = true
	} else {
		v.repoURL = v.workspaceURL
		v.branch = gitmsg.GetExtBranch(v.workdir, "pm")
		v.isRemote = false
	}
	v.cardList.SetCardOptions(tuicore.CardOptions{
		MaxLines:  1,
		ShowStats: true,
		Separator: true,
	})
	v.pag.Reset()
	return v.loadSprints()
}

func (v *SprintsView) loadSprints() tea.Cmd {
	v.pag.StartLoading()
	showAll := v.showAll
	repoURL := v.repoURL
	branch := v.branch
	workdir := v.workdir
	limit := v.pag.Limit()
	return func() tea.Msg {
		var states []string
		if showAll {
			states = []string{
				string(pm.SprintStatePlanned),
				string(pm.SprintStateActive),
				string(pm.SprintStateCompleted),
				string(pm.SprintStateCancelled),
			}
		} else {
			states = []string{string(pm.SprintStatePlanned), string(pm.SprintStateActive)}
		}
		result := pm.GetSprints(repoURL, branch, states, "", limit+1)
		if !result.Success {
			return SprintsLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		sprints, hasMore := tuicore.TrimPage(result.Data, limit)
		unpushed, _ := git.GetUnpushedCommits(workdir, branch)
		for i := range sprints {
			ref := protocol.ParseRef(sprints[i].ID)
			if _, ok := unpushed[ref.Value]; ok {
				sprints[i].IsUnpushed = true
			}
		}
		total, _ := pm.CountSprints(repoURL, branch, states)
		return SprintsLoadedMsg{Sprints: sprints, HasMore: hasMore, Total: total}
	}
}

func (v *SprintsView) loadMoreSprints() tea.Cmd {
	v.pag.StartLoading()
	showAll := v.showAll
	repoURL := v.repoURL
	branch := v.branch
	workdir := v.workdir
	cursor := v.pag.Cursor
	return func() tea.Msg {
		var states []string
		if showAll {
			states = []string{
				string(pm.SprintStatePlanned),
				string(pm.SprintStateActive),
				string(pm.SprintStateCompleted),
				string(pm.SprintStateCancelled),
			}
		} else {
			states = []string{string(pm.SprintStatePlanned), string(pm.SprintStateActive)}
		}
		result := pm.GetSprints(repoURL, branch, states, cursor, tuicore.PageSize+1)
		if !result.Success {
			return SprintsLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		sprints, hasMore := tuicore.TrimPage(result.Data, tuicore.PageSize)
		unpushed, _ := git.GetUnpushedCommits(workdir, branch)
		for i := range sprints {
			ref := protocol.ParseRef(sprints[i].ID)
			if _, ok := unpushed[ref.Value]; ok {
				sprints[i].IsUnpushed = true
			}
		}
		return SprintsLoadedMsg{Sprints: sprints, HasMore: hasMore, Append: true}
	}
}

// LoadMorePosts implements the loadMoreHandler interface for infinite scroll.
func (v *SprintsView) LoadMorePosts() tea.Cmd {
	return v.pag.LoadMore(v.loadMoreSprints)
}

// SprintsLoadedMsg signals that sprints have been loaded.
type SprintsLoadedMsg struct {
	Sprints []pm.Sprint
	HasMore bool
	Append  bool
	Total   int
	Err     error
}

// Update handles messages.
func (v *SprintsView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case SprintsLoadedMsg:
		v.pag.Loading = false
		if msg.Err == nil {
			v.pag.HasMore = msg.HasMore
			v.pag.SetTotal(msg.Total)
			if msg.Append {
				v.allSprints = append(v.allSprints, msg.Sprints...)
				newItems := SprintsToItems(msg.Sprints, v.userEmail, v.showEmail, v.workspaceURL)
				v.cardList.AppendItems(newItems)
			} else {
				v.allSprints = msg.Sprints
				v.applyFilter()
				if v.restoreIndex >= 0 {
					v.cardList.SetSelected(v.restoreIndex)
					v.restoreIndex = -1
				}
			}
			if len(msg.Sprints) > 0 {
				v.pag.Cursor = msg.Sprints[len(msg.Sprints)-1].Timestamp.Format(time.RFC3339)
			}
			v.loaded = true
		}
		return nil

	case SprintCreatedMsg:
		if msg.Err == nil {
			v.pag.Reset()
			return v.loadSprints()
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
				return tea.Batch(tuicore.ConsumedCmd, v.loadMoreSprints())
			}
			return tuicore.ConsumedCmd
		}
		if key, ok := msg.(tea.KeyPressMsg); ok {
			return v.handleKey(key, state)
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

// IsInputActive returns true when text input is active.
func (v *SprintsView) IsInputActive() bool {
	return v.searchActive
}

// navigateToSelected navigates to the selected sprint's detail view.
func (v *SprintsView) navigateToSelected() tea.Cmd {
	item, ok := v.cardList.SelectedItem()
	if !ok {
		return nil
	}
	sprint, ok := ItemToSprint(item)
	if !ok {
		return nil
	}
	sprintID := sprint.ID
	items := v.cardList.Items()
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location:    tuicore.LocPMSprintDetail(sprintID),
			Action:      tuicore.NavPush,
			SourcePath:  "/pm/sprints",
			SourceIndex: v.cardList.Selected(),
			SourceTotal: v.pag.Total(len(items)),
		}
	}
}

// GetItemAt returns the item ID at the given index.
func (v *SprintsView) GetItemAt(index int) (string, bool) {
	items := v.cardList.Items()
	if index >= 0 && index < len(items) {
		return items[index].ItemID(), true
	}
	return "", false
}

// GetItemCount returns the total number of items.
func (v *SprintsView) GetItemCount() int {
	return len(v.cardList.Items())
}

func (v *SprintsView) handleKey(msg tea.KeyPressMsg, _ *tuicore.State) tea.Cmd {
	switch msg.String() {
	case "N":
		if v.isRemote {
			return nil
		}
		return func() tea.Msg {
			return tuicore.NavigateMsg{
				Location: tuicore.LocPMNewSprint,
				Action:   tuicore.NavPush,
			}
		}
	case "F":
		v.showAll = !v.showAll
		v.pag.Reset()
		return v.loadSprints()
	case "m":
		if v.assigneeFilter == "" {
			v.assigneeFilter = "me"
		} else {
			v.assigneeFilter = ""
		}
		v.applyFilter()
		return nil
	case "r":
		v.restoreIndex = v.cardList.Selected()
		v.pag.ResetForRefresh(len(v.cardList.Items()))
		return v.loadSprints()
	case "/":
		v.searchActive = true
		v.searchInput.SetValue(v.searchQuery)
		return v.searchInput.Focus()
	}
	return nil
}

func (v *SprintsView) handleSearchKey(msg tea.KeyPressMsg) tea.Cmd {
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

// applyFilter filters sprints by author and search query, then updates the card list.
func (v *SprintsView) applyFilter() {
	filtered := v.allSprints
	if v.assigneeFilter == "me" && v.userEmail != "" {
		var mine []pm.Sprint
		for _, sp := range filtered {
			if strings.EqualFold(sp.Author.Email, v.userEmail) {
				mine = append(mine, sp)
			}
		}
		filtered = mine
	}
	if v.searchQuery != "" {
		pattern := tuicore.CompileSearchPattern(v.searchQuery)
		var matched []pm.Sprint
		for _, sp := range filtered {
			if pattern != nil && pattern.MatchString(sp.Title) {
				matched = append(matched, sp)
			}
		}
		filtered = matched
	}
	v.cardList.SetCardOptions(tuicore.CardOptions{
		MaxLines:      1,
		ShowStats:     true,
		Separator:     true,
		HighlightText: v.searchQuery,
	})
	v.cardList.SetItems(SprintsToItems(filtered, v.userEmail, v.showEmail, v.workspaceURL))
}

// Render renders the sprints view.
func (v *SprintsView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)

	var content string
	if !v.loaded {
		content = "Loading sprints..."
	} else if len(v.cardList.Items()) == 0 {
		filter := "active/planned"
		if v.showAll {
			filter = "all"
		}
		content = tuicore.Dim.Render(fmt.Sprintf("  No %s sprints", filter))
	} else {
		v.cardList.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
		content = v.cardList.View()
	}

	var footer string
	if v.searchActive {
		v.searchInput.SetWidth(wrapper.ContentWidth() - 5)
		footer = v.searchInput.View()
	} else {
		footer = tuicore.RenderFooter(state.Registry, tuicore.PMSprints, wrapper.ContentWidth(), nil)
	}
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *SprintsView) Title() string {
	filter := "Active"
	if v.showAll {
		filter = "All"
	}
	title := fmt.Sprintf("◷  %s Sprints", filter)
	if v.assigneeFilter == "me" {
		title += " · Mine"
	}
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
func (v *SprintsView) HeaderInfo() (position int, total string) {
	items := v.cardList.Items()
	if len(items) == 0 {
		return 0, ""
	}
	return v.cardList.Selected() + 1, v.pag.TotalDisplay(len(items))
}

// Bindings returns keybindings for this view.
func (v *SprintsView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "N", Label: "new", Contexts: []tuicore.Context{tuicore.PMSprints}, Handler: noop},
		{Key: "F", Label: "filter", Contexts: []tuicore.Context{tuicore.PMSprints}, Handler: noop},
		{Key: "m", Label: "mine", Contexts: []tuicore.Context{tuicore.PMSprints}, Handler: noop},
		{Key: "r", Label: "refresh", Contexts: []tuicore.Context{tuicore.PMSprints}, Handler: noop},
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.PMSprints}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.PMSprints}, Handler: push},
	}
}

// ViewName returns the view identifier.
func (v *SprintsView) ViewName() string {
	return "pm.sprints"
}
