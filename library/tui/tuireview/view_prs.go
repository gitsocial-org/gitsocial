// view_prs.go - Pull request list view
package tuireview

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// PRsView displays a list of pull requests.
type PRsView struct {
	workdir        string
	workspaceURL   string
	width          int
	height         int
	loaded         bool
	showEmail      bool
	userEmail      string
	assigneeFilter string // "" = all, "me" = my PRs
	allPRs         []review.PullRequest
	cardList       *tuicore.CardList
	searchActive   bool
	searchInput    textinput.Model
	searchQuery    string
	pag            tuicore.Pagination
	restoreIndex   int // cursor position to restore after refresh (-1 = none)
}

// NewPRsView creates a new pull requests view.
func NewPRsView(workdir string) *PRsView {
	searchInput := textinput.New()
	searchInput.Placeholder = "Filter pull requests..."
	searchInput.CharLimit = 100
	searchInput.Prompt = "/ "
	tuicore.StyleTextInput(&searchInput, tuicore.Title, tuicore.Title, tuicore.Dim)
	return &PRsView{
		workdir:      workdir,
		userEmail:    git.GetUserEmail(workdir),
		cardList:     tuicore.NewCardList(nil),
		searchInput:  searchInput,
		restoreIndex: -1,
	}
}

// SetSize sets the view dimensions.
func (v *PRsView) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.cardList.SetSize(w, h-2)
}

// Activate loads the pull requests.
func (v *PRsView) Activate(state *tuicore.State) tea.Cmd {
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
	return v.loadPRs()
}

// Deactivate is called when the view is hidden.
func (v *PRsView) Deactivate() {}

// navigateToSelected navigates to the selected PR's detail view.
func (v *PRsView) navigateToSelected() tea.Cmd {
	item, ok := v.cardList.SelectedItem()
	if !ok {
		return nil
	}
	prID := item.ItemID()
	items := v.cardList.Items()
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location:    tuicore.LocReviewPRDetail(prID),
			Action:      tuicore.NavPush,
			SourcePath:  "/review/prs",
			SourceIndex: v.cardList.Selected(),
			SourceTotal: v.pag.Total(len(items)),
		}
	}
}

// GetItemAt returns the item ID at the given index.
func (v *PRsView) GetItemAt(index int) (string, bool) {
	items := v.cardList.Items()
	if index >= 0 && index < len(items) {
		return items[index].ItemID(), true
	}
	return "", false
}

// GetItemCount returns the total number of items.
func (v *PRsView) GetItemCount() int {
	return len(v.cardList.Items())
}

// Update handles messages.
func (v *PRsView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case prsLoadedMsg:
		v.pag.Loading = false
		v.loaded = true
		if msg.err != nil {
			return nil
		}
		v.pag.HasMore = msg.hasMore
		v.pag.SetTotal(msg.total)
		if msg.append {
			v.allPRs = append(v.allPRs, msg.prs...)
			newItems := make([]tuicore.DisplayItem, len(msg.prs))
			for i, pr := range msg.prs {
				newItems[i] = tuicore.NewItem(pr.ID, "review", "pull-request", pr.Timestamp, prItemData{
					PR:           pr,
					ShowEmail:    v.showEmail,
					UserEmail:    v.userEmail,
					WorkspaceURL: v.workspaceURL,
				})
			}
			v.cardList.AppendItems(newItems)
		} else {
			v.allPRs = msg.prs
			v.applyFilter()
			if v.restoreIndex >= 0 {
				v.cardList.SetSelected(v.restoreIndex)
				v.restoreIndex = -1
			}
		}
		if len(msg.prs) > 0 {
			v.pag.Cursor = msg.prs[len(msg.prs)-1].Timestamp.Format(time.RFC3339)
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
				return tea.Batch(tuicore.ConsumedCmd, v.loadMorePRs())
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
func (v *PRsView) IsInputActive() bool {
	return v.searchActive
}

func (v *PRsView) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "N":
		return func() tea.Msg {
			return tuicore.NavigateMsg{
				Location: tuicore.LocReviewNewPR,
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
		v.restoreIndex = v.cardList.Selected()
		v.pag.ResetForRefresh(len(v.cardList.Items()))
		return v.loadPRs()
	case "F":
		return func() tea.Msg {
			return tuicore.NavigateMsg{
				Location: tuicore.LocForks,
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

func (v *PRsView) handleSearchKey(msg tea.KeyPressMsg) tea.Cmd {
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

// applyFilter filters PRs by author and search query, then updates the card list.
func (v *PRsView) applyFilter() {
	filtered := v.allPRs
	if v.assigneeFilter == "me" && v.userEmail != "" {
		var mine []review.PullRequest
		for _, pr := range filtered {
			if strings.EqualFold(pr.Author.Email, v.userEmail) {
				mine = append(mine, pr)
			}
		}
		filtered = mine
	}
	if v.searchQuery != "" {
		pattern := tuicore.CompileSearchPattern(v.searchQuery)
		var matched []review.PullRequest
		for _, pr := range filtered {
			if pattern != nil && pattern.MatchString(pr.Subject) {
				matched = append(matched, pr)
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
	for i, pr := range filtered {
		items[i] = tuicore.NewItem(pr.ID, "review", "pull-request", pr.Timestamp, prItemData{
			PR:           pr,
			ShowEmail:    v.showEmail,
			UserEmail:    v.userEmail,
			WorkspaceURL: v.workspaceURL,
		})
	}
	v.cardList.SetItems(items)
}

// Render renders the view.
func (v *PRsView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)

	var content string
	if !v.loaded {
		content = "Loading pull requests..."
	} else if len(v.cardList.Items()) == 0 {
		content = tuicore.Dim.Render("  No pull requests found")
	} else {
		v.cardList.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
		content = v.cardList.View()
	}

	var footer string
	if v.searchActive {
		v.searchInput.SetWidth(wrapper.ContentWidth() - 5)
		footer = v.searchInput.View()
	} else {
		footer = tuicore.RenderFooter(state.Registry, tuicore.ReviewPRs, wrapper.ContentWidth(), nil)
	}
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *PRsView) Title() string {
	title := "⑂  Pull Requests"
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
func (v *PRsView) HeaderInfo() (position int, total string) {
	items := v.cardList.Items()
	if len(items) == 0 {
		return 0, ""
	}
	return v.cardList.Selected() + 1, v.pag.TotalDisplay(len(items))
}

// Bindings returns keybindings for this view.
func (v *PRsView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "N", Label: "new", Contexts: []tuicore.Context{tuicore.ReviewPRs}, Handler: noop},
		{Key: "m", Label: "mine", Contexts: []tuicore.Context{tuicore.ReviewPRs}, Handler: noop},
		{Key: "r", Label: "refresh", Contexts: []tuicore.Context{tuicore.ReviewPRs}, Handler: noop},
		{Key: "F", Label: "forks", Contexts: []tuicore.Context{tuicore.ReviewPRs}, Handler: noop},
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.ReviewPRs}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.ReviewPRs}, Handler: push},
	}
}

type prsLoadedMsg struct {
	prs     []review.PullRequest
	hasMore bool
	append  bool
	total   int
	err     error
}

func (v *PRsView) loadPRs() tea.Cmd {
	v.pag.StartLoading()
	workdir := v.workdir
	limit := v.pag.Limit()
	return func() tea.Msg {
		repoURL := gitmsg.ResolveRepoURL(workdir)
		v.workspaceURL = repoURL
		branch := gitmsg.GetExtBranch(workdir, "review")
		forks := review.GetForks(workdir)

		res := review.GetPullRequestsWithForks(repoURL, branch, forks, nil, "", limit+1)
		if !res.Success {
			return prsLoadedMsg{err: fmt.Errorf("%s", res.Error.Message)}
		}

		prs, hasMore := tuicore.TrimPage(res.Data, limit)

		// Populate review summaries in a single batch query
		keys := make([]review.PRKey, len(prs))
		for i, pr := range prs {
			keys[i] = review.PRKey{
				RepoURL:   pr.Repository,
				Hash:      extractHashFromID(pr.ID),
				Branch:    pr.Branch,
				Reviewers: pr.Reviewers,
			}
		}
		summaries := review.GetBatchReviewSummaries(keys)
		for i := range prs {
			hash := extractHashFromID(prs[i].ID)
			if s, ok := summaries[hash]; ok {
				prs[i].ReviewSummary = s
			}
		}

		total := review.CountPRsWithForks(repoURL, branch, forks, nil)
		return prsLoadedMsg{prs: prs, hasMore: hasMore, total: total}
	}
}

func (v *PRsView) loadMorePRs() tea.Cmd {
	v.pag.StartLoading()
	workdir := v.workdir
	cursor := v.pag.Cursor
	return func() tea.Msg {
		repoURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "review")
		forks := review.GetForks(workdir)

		res := review.GetPullRequestsWithForks(repoURL, branch, forks, nil, cursor, tuicore.PageSize+1)
		if !res.Success {
			return prsLoadedMsg{err: fmt.Errorf("%s", res.Error.Message)}
		}

		prs, hasMore := tuicore.TrimPage(res.Data, tuicore.PageSize)

		// Populate review summaries for new items only
		keys := make([]review.PRKey, len(prs))
		for i, pr := range prs {
			keys[i] = review.PRKey{
				RepoURL:   pr.Repository,
				Hash:      extractHashFromID(pr.ID),
				Branch:    pr.Branch,
				Reviewers: pr.Reviewers,
			}
		}
		summaries := review.GetBatchReviewSummaries(keys)
		for i := range prs {
			hash := extractHashFromID(prs[i].ID)
			if s, ok := summaries[hash]; ok {
				prs[i].ReviewSummary = s
			}
		}

		return prsLoadedMsg{prs: prs, hasMore: hasMore, append: true}
	}
}

// LoadMorePosts implements the loadMoreHandler interface for infinite scroll.
func (v *PRsView) LoadMorePosts() tea.Cmd {
	return v.pag.LoadMore(v.loadMorePRs)
}

func extractHashFromID(id string) string {
	parsed := protocol.ParseRef(id)
	return parsed.Value
}
