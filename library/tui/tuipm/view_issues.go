// view_issues.go - Issues list view for PM
package tuipm

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// issuesView displays a list of issues.
type issuesView struct {
	workdir          string
	repoURL          string
	branch           string
	isRemote         bool
	width            int
	height           int
	loaded           bool
	showAll          bool
	showEmail        bool
	assigneeFilter   string // "" = all, "me" = assigned to me
	userEmail        string
	allIssues        []pm.Issue // unfiltered issues for client-side filtering
	contributorNames map[string]string
	cardList         *tuicore.CardList
	searchActive     bool
	searchInput      textinput.Model
	searchQuery      string
	pag              tuicore.Pagination
	restoreID        string // item ID to reselect after reload ("" = none)
}

// newIssuesView creates a new issues view.
func newIssuesView(workdir string) *issuesView {
	searchInput := textinput.New()
	searchInput.Placeholder = "Filter issues..."
	searchInput.CharLimit = 100
	searchInput.Prompt = "/ "
	tuicore.StyleTextInput(&searchInput, tuicore.Title, tuicore.Title, tuicore.Dim)
	return &issuesView{
		workdir:     workdir,
		searchInput: searchInput,
		userEmail:   git.GetUserEmail(workdir),
		cardList:    tuicore.NewCardList(nil),
	}
}

// SetSize sets the view dimensions.
func (v *issuesView) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.cardList.SetSize(w, h-3)
}

// Activate loads the issues.
func (v *issuesView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	v.searchActive = false
	v.searchQuery = ""
	v.searchInput.SetValue("")
	loc := state.Router.Location()
	url := loc.Param("url")
	branch := loc.Param("branch")
	if url != "" {
		v.repoURL = url
		v.branch = branch
		v.isRemote = true
	} else {
		v.repoURL = gitmsg.ResolveRepoURL(v.workdir)
		v.branch = gitmsg.GetExtBranch(v.workdir, "pm")
		v.isRemote = false
	}
	v.cardList.SetCardOptions(tuicore.CardOptions{
		MaxLines:  1,
		ShowStats: true,
		Separator: true,
	})
	// Returning from this list's detail view: reselect the focused row by ID
	// after the reload. DetailSource.Index tracks left/right paging and indexes
	// the still-loaded list, so read the focused id there.
	v.restoreID = ""
	if state.DetailSource != nil && state.DetailSource.Path == "/pm/issues" {
		if id, ok := v.GetItemAt(state.DetailSource.Index); ok {
			v.restoreID = id
		}
	}
	v.pag.Reset()
	return v.loadIssues()
}

// Refresh reloads issues in place, preserving the focused row by ID.
func (v *issuesView) Refresh(_ *tuicore.State) tea.Cmd {
	if id, ok := v.cardList.SelectedID(); ok {
		v.restoreID = id
	}
	v.pag.ResetForRefresh(len(v.cardList.Items()))
	return v.loadIssues()
}

func (v *issuesView) loadIssues() tea.Cmd {
	v.pag.StartLoading()
	showAll := v.showAll
	repoURL := v.repoURL
	branch := v.branch
	workdir := v.workdir
	limit := v.pag.Limit()
	return func() tea.Msg {
		var states []string
		if showAll {
			states = []string{"open", "closed"}
		} else {
			states = []string{"open"}
		}
		forks := gitmsg.GetForks(workdir)
		result := pm.GetIssuesWithForks(repoURL, branch, forks, states, "", limit+1)
		if !result.Success {
			return issuesLoadedMsg{Err: fmt.Errorf("%s", result.Error.Text())}
		}
		issues, hasMore := tuicore.TrimPage(result.Data, limit)
		unpushed, _ := git.GetUnpushedCommits(workdir, branch)
		for i := range issues {
			ref := protocol.ParseRef(issues[i].ID)
			if _, ok := unpushed[ref.Value]; ok {
				issues[i].IsUnpushed = true
			}
		}
		contributorNames := buildContributorNameMap(workdir)
		total := pm.CountIssuesWithForks(repoURL, branch, forks, states)
		return issuesLoadedMsg{Issues: issues, ContributorNames: contributorNames, HasMore: hasMore, Total: total}
	}
}

func (v *issuesView) loadMoreIssues() tea.Cmd {
	v.pag.StartLoading()
	showAll := v.showAll
	repoURL := v.repoURL
	branch := v.branch
	workdir := v.workdir
	cursor := v.pag.Cursor
	return func() tea.Msg {
		var states []string
		if showAll {
			states = []string{"open", "closed"}
		} else {
			states = []string{"open"}
		}
		forks := gitmsg.GetForks(workdir)
		result := pm.GetIssuesWithForks(repoURL, branch, forks, states, cursor, tuicore.PageSize+1)
		if !result.Success {
			return issuesLoadedMsg{Err: fmt.Errorf("%s", result.Error.Text())}
		}
		issues, hasMore := tuicore.TrimPage(result.Data, tuicore.PageSize)
		unpushed, _ := git.GetUnpushedCommits(workdir, branch)
		for i := range issues {
			ref := protocol.ParseRef(issues[i].ID)
			if _, ok := unpushed[ref.Value]; ok {
				issues[i].IsUnpushed = true
			}
		}
		return issuesLoadedMsg{Issues: issues, HasMore: hasMore, Append: true}
	}
}

// LoadMorePosts implements the loadMoreHandler interface for infinite scroll.
func (v *issuesView) LoadMorePosts() tea.Cmd {
	return v.pag.LoadMore(v.loadMoreIssues)
}

// issuesLoadedMsg signals that issues have been loaded.
type issuesLoadedMsg struct {
	Issues           []pm.Issue
	ContributorNames map[string]string
	HasMore          bool
	Append           bool
	Total            int
	Err              error
}

// Update handles messages.
func (v *issuesView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case issuesLoadedMsg:
		v.pag.Loading = false
		if msg.Err == nil {
			v.pag.HasMore = msg.HasMore
			v.pag.SetTotal(msg.Total)
			if msg.Append {
				v.allIssues = append(v.allIssues, msg.Issues...)
				newItems := issuesToItems(msg.Issues, v.userEmail, v.contributorNames, v.showEmail)
				v.cardList.AppendItems(newItems)
			} else {
				v.allIssues = msg.Issues
				if msg.ContributorNames != nil {
					v.contributorNames = msg.ContributorNames
				}
				v.applyFilter()
				if v.restoreID != "" {
					v.cardList.SelectByID(v.restoreID)
					v.restoreID = ""
				}
			}
			if len(msg.Issues) > 0 {
				v.pag.Cursor = msg.Issues[len(msg.Issues)-1].Timestamp.Format(time.RFC3339)
			}
			v.loaded = true
		}
		return nil

	case issueCreatedMsg:
		if msg.Err == nil {
			v.pag.Reset()
			return v.loadIssues()
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
				return tea.Batch(tuicore.ConsumedCmd, v.loadMoreIssues())
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
func (v *issuesView) IsInputActive() bool { return v.searchActive }

// navigateToSelected navigates to the selected issue's detail view.
func (v *issuesView) navigateToSelected() tea.Cmd {
	item, ok := v.cardList.SelectedItem()
	if !ok {
		return nil
	}
	issue, ok := itemToIssue(item)
	if !ok {
		return nil
	}
	issueID := issue.ID
	items := v.cardList.Items()
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location:    tuicore.LocPMIssueDetail(issueID),
			Action:      tuicore.NavPush,
			SourcePath:  "/pm/issues",
			SourceIndex: v.cardList.Selected(),
			SourceTotal: v.pag.Total(len(items)),
		}
	}
}

// GetItemAt returns the item ID at the given index.
func (v *issuesView) GetItemAt(index int) (string, bool) {
	items := v.cardList.Items()
	if index >= 0 && index < len(items) {
		return items[index].ItemID(), true
	}
	return "", false
}

// GetItemCount returns the total number of items.
func (v *issuesView) GetItemCount() int {
	return len(v.cardList.Items())
}

func (v *issuesView) handleKey(msg tea.KeyPressMsg, _ *tuicore.State) tea.Cmd {
	switch msg.String() {
	case "F":
		v.showAll = !v.showAll
		v.pag.Reset()
		return v.loadIssues()
	case "K":
		return func() tea.Msg {
			return tuicore.NavigateMsg{
				Location: tuicore.LocForks,
				Action:   tuicore.NavPush,
			}
		}
	case "m":
		if v.assigneeFilter == "" {
			v.assigneeFilter = "me"
		} else {
			v.assigneeFilter = ""
		}
		v.applyFilter()
		return nil
	case "r":
		if id, ok := v.cardList.SelectedID(); ok {
			v.restoreID = id
		}
		v.pag.ResetForRefresh(len(v.cardList.Items()))
		return v.loadIssues()
	case "n":
		if v.isRemote {
			return nil
		}
		return func() tea.Msg {
			return tuicore.NavigateMsg{
				Location: tuicore.LocPMNewIssue,
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

func (v *issuesView) handleSearchKey(msg tea.KeyPressMsg) tea.Cmd {
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

// applyFilter filters allIssues by assignee and search query, then updates the card list.
func (v *issuesView) applyFilter() {
	filtered := v.allIssues
	if v.assigneeFilter == "me" && v.userEmail != "" {
		var mine []pm.Issue
		for _, issue := range v.allIssues {
			for _, a := range issue.Assignees {
				if strings.EqualFold(a, v.userEmail) {
					mine = append(mine, issue)
					break
				}
			}
		}
		filtered = mine
	}
	if v.searchQuery != "" {
		pattern := tuicore.CompileSearchPattern(v.searchQuery)
		var matched []pm.Issue
		for _, issue := range filtered {
			if pattern != nil && pattern.MatchString(issue.Subject) {
				matched = append(matched, issue)
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
	v.cardList.SetItems(issuesToItems(filtered, v.userEmail, v.contributorNames, v.showEmail))
}

// Render renders the issues view.
func (v *issuesView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)

	var content string
	if !v.loaded {
		content = "Loading issues..."
	} else if len(v.cardList.Items()) == 0 {
		if v.showAll {
			content = tuicore.Dim.Render("  No issues")
		} else {
			content = tuicore.Dim.Render("  No open issues")
		}
	} else {
		v.cardList.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
		content = v.cardList.View()
	}

	var footer string
	if v.searchActive {
		v.searchInput.SetWidth(wrapper.ContentWidth() - 5)
		footer = v.searchInput.View()
	} else {
		footer = tuicore.RenderFooter(state.Registry, tuicore.PMIssues, nil)
	}
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *issuesView) Title() string {
	stateFilter := "Open"
	if v.showAll {
		stateFilter = "All"
	}
	title := fmt.Sprintf("○  %s Issues", stateFilter)
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
func (v *issuesView) HeaderInfo() (position int, total string) {
	items := v.cardList.Items()
	if len(items) == 0 {
		return 0, ""
	}
	return v.cardList.Selected() + 1, v.pag.TotalDisplay(len(items))
}

// Bindings returns keybindings for this view.
func (v *issuesView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "n", Label: "new", Contexts: []tuicore.Context{tuicore.PMIssues}, Handler: noop},
		{Key: "F", Label: "filter", Contexts: []tuicore.Context{tuicore.PMIssues}, Handler: noop},
		{Key: "m", Label: "mine", Contexts: []tuicore.Context{tuicore.PMIssues}, Handler: noop},
		{Key: "K", Label: "forks", Contexts: []tuicore.Context{tuicore.PMIssues}, Handler: noop},
		{Key: "r", Label: "refresh", Contexts: []tuicore.Context{tuicore.PMIssues}, Handler: noop},
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.PMIssues}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.PMIssues}, Handler: push},
	}
}
