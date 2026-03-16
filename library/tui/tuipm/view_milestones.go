// milestones.go - Milestones list view for PM
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

// MilestonesView displays a list of milestones.
type MilestonesView struct {
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
	allMilestones  []pm.Milestone
	searchActive   bool
	searchInput    textinput.Model
	searchQuery    string
	pag            tuicore.Pagination
	restoreIndex   int // cursor position to restore after refresh (-1 = none)
}

// NewMilestonesView creates a new milestones view.
func NewMilestonesView(workdir string) *MilestonesView {
	searchInput := textinput.New()
	searchInput.Placeholder = "Filter milestones..."
	searchInput.CharLimit = 100
	searchInput.Prompt = "/ "
	tuicore.StyleTextInput(&searchInput, tuicore.Title, tuicore.Title, tuicore.Dim)
	return &MilestonesView{
		workdir:      workdir,
		userEmail:    git.GetUserEmail(workdir),
		cardList:     tuicore.NewCardList(nil),
		searchInput:  searchInput,
		restoreIndex: -1,
	}
}

// SetSize sets the view dimensions.
func (v *MilestonesView) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.cardList.SetSize(w, h-3)
}

// Activate loads the milestones.
func (v *MilestonesView) Activate(state *tuicore.State) tea.Cmd {
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
	return v.loadMilestones()
}

func (v *MilestonesView) loadMilestones() tea.Cmd {
	v.pag.StartLoading()
	showAll := v.showAll
	repoURL := v.repoURL
	branch := v.branch
	workdir := v.workdir
	limit := v.pag.Limit()
	return func() tea.Msg {
		var states []string
		if showAll {
			states = []string{string(pm.StateOpen), string(pm.StateClosed), string(pm.StateCancelled)}
		} else {
			states = []string{string(pm.StateOpen)}
		}
		result := pm.GetMilestones(repoURL, branch, states, "", limit+1)
		if !result.Success {
			return MilestonesLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		milestones, hasMore := tuicore.TrimPage(result.Data, limit)
		unpushed, _ := git.GetUnpushedCommits(workdir, branch)
		for i := range milestones {
			ref := protocol.ParseRef(milestones[i].ID)
			if _, ok := unpushed[ref.Value]; ok {
				milestones[i].IsUnpushed = true
			}
		}
		total, _ := pm.CountMilestones(repoURL, branch, states)
		return MilestonesLoadedMsg{Milestones: milestones, HasMore: hasMore, Total: total}
	}
}

func (v *MilestonesView) loadMoreMilestones() tea.Cmd {
	v.pag.StartLoading()
	showAll := v.showAll
	repoURL := v.repoURL
	branch := v.branch
	workdir := v.workdir
	cursor := v.pag.Cursor
	return func() tea.Msg {
		var states []string
		if showAll {
			states = []string{string(pm.StateOpen), string(pm.StateClosed), string(pm.StateCancelled)}
		} else {
			states = []string{string(pm.StateOpen)}
		}
		result := pm.GetMilestones(repoURL, branch, states, cursor, tuicore.PageSize+1)
		if !result.Success {
			return MilestonesLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		milestones, hasMore := tuicore.TrimPage(result.Data, tuicore.PageSize)
		unpushed, _ := git.GetUnpushedCommits(workdir, branch)
		for i := range milestones {
			ref := protocol.ParseRef(milestones[i].ID)
			if _, ok := unpushed[ref.Value]; ok {
				milestones[i].IsUnpushed = true
			}
		}
		return MilestonesLoadedMsg{Milestones: milestones, HasMore: hasMore, Append: true}
	}
}

// LoadMorePosts implements the loadMoreHandler interface for infinite scroll.
func (v *MilestonesView) LoadMorePosts() tea.Cmd {
	return v.pag.LoadMore(v.loadMoreMilestones)
}

// MilestonesLoadedMsg signals that milestones have been loaded.
type MilestonesLoadedMsg struct {
	Milestones []pm.Milestone
	HasMore    bool
	Append     bool
	Total      int
	Err        error
}

// MilestoneCreatedMsg signals that a milestone was created.
type MilestoneCreatedMsg struct {
	Milestone pm.Milestone
	Err       error
}

// Update handles messages.
func (v *MilestonesView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case MilestonesLoadedMsg:
		v.pag.Loading = false
		if msg.Err == nil {
			v.pag.HasMore = msg.HasMore
			v.pag.SetTotal(msg.Total)
			if msg.Append {
				v.allMilestones = append(v.allMilestones, msg.Milestones...)
				newItems := MilestonesToItems(msg.Milestones, v.userEmail, v.showEmail, v.workspaceURL)
				v.cardList.AppendItems(newItems)
			} else {
				v.allMilestones = msg.Milestones
				v.applyFilter()
				if v.restoreIndex >= 0 {
					v.cardList.SetSelected(v.restoreIndex)
					v.restoreIndex = -1
				}
			}
			if len(msg.Milestones) > 0 {
				v.pag.Cursor = msg.Milestones[len(msg.Milestones)-1].Timestamp.Format(time.RFC3339)
			}
			v.loaded = true
		}
		return nil

	case MilestoneCreatedMsg:
		if msg.Err == nil {
			v.pag.Reset()
			return v.loadMilestones()
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
				return tea.Batch(tuicore.ConsumedCmd, v.loadMoreMilestones())
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
func (v *MilestonesView) IsInputActive() bool {
	return v.searchActive
}

// navigateToSelected navigates to the selected milestone's detail view.
func (v *MilestonesView) navigateToSelected() tea.Cmd {
	item, ok := v.cardList.SelectedItem()
	if !ok {
		return nil
	}
	milestone, ok := ItemToMilestone(item)
	if !ok {
		return nil
	}
	milestoneID := milestone.ID
	items := v.cardList.Items()
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location:    tuicore.LocPMMilestoneDetail(milestoneID),
			Action:      tuicore.NavPush,
			SourcePath:  "/pm/milestones",
			SourceIndex: v.cardList.Selected(),
			SourceTotal: v.pag.Total(len(items)),
		}
	}
}

// GetItemAt returns the item ID at the given index.
func (v *MilestonesView) GetItemAt(index int) (string, bool) {
	items := v.cardList.Items()
	if index >= 0 && index < len(items) {
		return items[index].ItemID(), true
	}
	return "", false
}

// GetItemCount returns the total number of items.
func (v *MilestonesView) GetItemCount() int {
	return len(v.cardList.Items())
}

func (v *MilestonesView) handleKey(msg tea.KeyPressMsg, _ *tuicore.State) tea.Cmd {
	switch msg.String() {
	case "F":
		v.showAll = !v.showAll
		v.pag.Reset()
		return v.loadMilestones()
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
		return v.loadMilestones()
	case "N":
		if v.isRemote {
			return nil
		}
		return func() tea.Msg {
			return tuicore.NavigateMsg{
				Location: tuicore.LocPMNewMilestone,
				Action:   tuicore.NavPush,
			}
		}
	case "/":
		v.searchActive = true
		v.searchInput.SetValue(v.searchQuery)
		return v.searchInput.Focus()
	}
	return nil
}

func (v *MilestonesView) handleSearchKey(msg tea.KeyPressMsg) tea.Cmd {
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

// applyFilter filters milestones by author and search query, then updates the card list.
func (v *MilestonesView) applyFilter() {
	filtered := v.allMilestones
	if v.assigneeFilter == "me" && v.userEmail != "" {
		var mine []pm.Milestone
		for _, ms := range filtered {
			if strings.EqualFold(ms.Author.Email, v.userEmail) {
				mine = append(mine, ms)
			}
		}
		filtered = mine
	}
	if v.searchQuery != "" {
		pattern := tuicore.CompileSearchPattern(v.searchQuery)
		var matched []pm.Milestone
		for _, ms := range filtered {
			if pattern != nil && pattern.MatchString(ms.Title) {
				matched = append(matched, ms)
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
	v.cardList.SetItems(MilestonesToItems(filtered, v.userEmail, v.showEmail, v.workspaceURL))
}

// Render renders the milestones view.
func (v *MilestonesView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)

	var content string
	if !v.loaded {
		content = "Loading milestones..."
	} else if len(v.cardList.Items()) == 0 {
		filter := "open"
		if v.showAll {
			filter = "all"
		}
		content = tuicore.Dim.Render(fmt.Sprintf("  No %s milestones", filter))
	} else {
		v.cardList.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
		content = v.cardList.View()
	}

	var footer string
	if v.searchActive {
		v.searchInput.SetWidth(wrapper.ContentWidth() - 5)
		footer = v.searchInput.View()
	} else {
		footer = tuicore.RenderFooter(state.Registry, tuicore.PMMilestones, wrapper.ContentWidth(), nil)
	}
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *MilestonesView) Title() string {
	filter := "Open"
	if v.showAll {
		filter = "All"
	}
	title := fmt.Sprintf("◇  %s Milestones", filter)
	if v.assigneeFilter == "me" {
		title += " · Mine"
	}
	if v.searchQuery != "" {
		title += fmt.Sprintf(" · /%s", v.searchQuery)
	}
	items := v.cardList.Items()
	if len(items) > 0 {
		return fmt.Sprintf("%s · %d/%d", title, v.cardList.Selected()+1, v.pag.Total(len(items)))
	}
	return title
}

// HeaderInfo returns position info for the title.
func (v *MilestonesView) HeaderInfo() (position, total int) {
	items := v.cardList.Items()
	if len(items) == 0 {
		return 0, 0
	}
	return v.cardList.Selected() + 1, v.pag.Total(len(items))
}

// Bindings returns keybindings for this view.
func (v *MilestonesView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "N", Label: "new", Contexts: []tuicore.Context{tuicore.PMMilestones}, Handler: noop},
		{Key: "F", Label: "filter", Contexts: []tuicore.Context{tuicore.PMMilestones}, Handler: noop},
		{Key: "m", Label: "mine", Contexts: []tuicore.Context{tuicore.PMMilestones}, Handler: noop},
		{Key: "r", Label: "refresh", Contexts: []tuicore.Context{tuicore.PMMilestones}, Handler: noop},
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.PMMilestones}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.PMMilestones}, Handler: push},
	}
}

// ViewName returns the view identifier.
func (v *MilestonesView) ViewName() string {
	return "pm.milestones"
}
