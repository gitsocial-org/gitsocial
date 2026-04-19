// search.go - Search view with query input and filtered results
package tuicore

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// SearchResultsMsg is sent when search results are loaded.
type SearchResultsMsg struct {
	Result SearchResult
	Query  string
	Append bool
	Err    error
}

// SearchView displays search input and results.
type SearchView struct {
	input        textinput.Model
	query        string
	scope        string
	results      *CardList
	loading      bool
	total        int
	totalItems   int
	inputMode    bool
	workdir      string
	searchFunc   SearchFunc
	resolveFunc  ResolveItemFunc
	restoreIndex int // cursor position to restore after refresh (-1 = none)
	pag          Pagination
	searchOffset int // current offset for pagination
}

// NewSearchView creates a new search view with injected dependencies.
func NewSearchView(workdir string, searchFn SearchFunc, resolveFn ResolveItemFunc) *SearchView {
	input := textinput.New()
	input.Placeholder = ""
	input.CharLimit = 100
	input.Prompt = "> "
	StyleTextInput(&input, Title, Title, Dim)

	v := &SearchView{
		input:        input,
		inputMode:    true,
		workdir:      workdir,
		searchFunc:   searchFn,
		resolveFunc:  resolveFn,
		restoreIndex: -1,
	}
	v.results = NewCardList(nil)
	if resolveFn != nil {
		v.results.SetItemResolver(func(itemID string) (DisplayItem, bool) {
			return resolveFn(workdir, itemID)
		})
	}
	return v
}

// SetSize sets the view dimensions.
func (v *SearchView) SetSize(width, height int) {
	// Height: input (1) + gap (2) + footer (3) = 6
	v.results.SetSize(width, height-6)
}

// Activate initializes the search view.
func (v *SearchView) Activate(state *State) tea.Cmd {
	// Restore cursor position when returning from detail view
	if state.DetailSource != nil && state.DetailSource.Path == "/search" {
		v.restoreIndex = state.DetailSource.Index
	} else {
		v.restoreIndex = -1
	}

	loc := state.Router.Location()
	query := loc.Param("q")

	// If URL has a query param, use it
	if query != "" {
		// If same query and we have results, just restore focus
		if query == v.query && len(v.results.Items()) > 0 {
			v.inputMode = false
			v.input.SetValue(query)
			v.input.Blur()
			v.results.SetActive(true)
			return nil
		}
		terms := ExtractSearchTerms(query)
		if len(terms) >= 3 {
			// Enough search terms - execute search immediately, focus results
			v.query = query
			v.input.SetValue(query)
			v.input.Blur()
			v.inputMode = false
			v.loading = true
			v.results.SetActive(true)
			return v.doSearch()
		}
		// Filter-only or short terms - show input with query prefilled
		v.query = query
		v.input.SetValue(query + " ")
		v.input.Placeholder = ""
		v.results.SetItems(nil)
		v.results.SetActive(false)
		v.pag.Reset()
		v.searchOffset = 0
		v.inputMode = true
		return v.input.Focus()
	}

	// No URL query - preserve existing state if we have results
	if v.query != "" && len(v.results.Items()) > 0 {
		v.inputMode = false
		v.input.Blur()
		v.results.SetActive(true)
		return nil
	}

	// Fresh search - reset input state
	v.input.Blur()
	v.input.Reset()
	v.input.SetValue("")
	v.input.Placeholder = ""
	StyleTextInput(&v.input, Title, Title, Dim)
	v.query = ""
	v.results.SetItems(nil)
	v.results.SetActive(false)
	v.pag.Reset()
	v.searchOffset = 0
	v.inputMode = true
	return v.input.Focus()
}

// doSearch executes a fresh search query (offset 0).
func (v *SearchView) doSearch() tea.Cmd {
	v.searchOffset = 0
	v.pag.Reset()
	v.pag.StartLoading()
	query := v.query
	scope := v.scope
	workdir := v.workdir
	searchFn := v.searchFunc
	return func() tea.Msg {
		if searchFn == nil {
			return SearchResultsMsg{Err: fmt.Errorf("search not available"), Query: query}
		}
		result, err := searchFn(workdir, query, scope, PageSize+1, 0)
		if err != nil {
			return SearchResultsMsg{Err: err, Query: query}
		}
		return SearchResultsMsg{Result: result, Query: query}
	}
}

// doSearchMore fetches the next page of search results.
func (v *SearchView) doSearchMore() tea.Cmd {
	v.pag.StartLoading()
	query := v.query
	scope := v.scope
	workdir := v.workdir
	searchFn := v.searchFunc
	offset := v.searchOffset
	return func() tea.Msg {
		if searchFn == nil {
			return SearchResultsMsg{Err: fmt.Errorf("search not available"), Query: query, Append: true}
		}
		result, err := searchFn(workdir, query, scope, PageSize+1, offset)
		if err != nil {
			return SearchResultsMsg{Err: err, Query: query, Append: true}
		}
		return SearchResultsMsg{Result: result, Query: query, Append: true}
	}
}

// LoadMorePosts implements the loadMoreHandler interface for infinite scroll.
func (v *SearchView) LoadMorePosts() tea.Cmd {
	return v.pag.LoadMore(v.doSearchMore)
}

// Update handles input for the search view.
func (v *SearchView) Update(msg tea.Msg, state *State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if !v.inputMode {
			consumed, activate, link := v.results.Update(msg)
			if link != nil {
				return func() tea.Msg { return NavigateMsg{Location: *link, Action: NavPush} }
			}
			if activate {
				return v.navigateToSelected()
			}
			if consumed {
				return nil
			}
		}
	case tea.KeyPressMsg:
		return v.handleKey(msg, state)
	case SearchResultsMsg:
		return v.handleSearchResults(msg)
	}

	// Update input for cursor blinking
	if v.inputMode {
		var cmd tea.Cmd
		v.input, cmd = v.input.Update(msg)
		return cmd
	}
	return nil
}

// navigateToSelected navigates to the selected search result.
func (v *SearchView) navigateToSelected() tea.Cmd {
	item, ok := v.results.SelectedItem()
	if !ok {
		return nil
	}
	query := v.query
	total := v.total
	loc := GetNavTarget(item)
	return func() tea.Msg {
		return NavigateMsg{
			Location:    loc,
			Action:      NavPush,
			SourcePath:  "/search",
			SourceIndex: v.results.Selected(),
			SourceTotal: total,
			SearchQuery: query,
		}
	}
}

// handleKey processes keyboard input.
func (v *SearchView) handleKey(msg tea.KeyPressMsg, _ *State) tea.Cmd {
	if v.inputMode {
		switch msg.String() {
		case "enter":
			query := v.input.Value()
			if query != "" {
				v.inputMode = false
				v.input.Blur()
				v.results.SetActive(true)
				if query != v.query {
					v.query = query
					v.loading = true
					return v.doSearch()
				}
			}
			return nil
		case "down":
			if len(v.results.Items()) > 0 {
				v.inputMode = false
				v.input.Blur()
				v.results.SetActive(true)
			}
		case "esc":
			if v.query != "" {
				v.inputMode = false
				v.input.Blur()
				v.results.SetActive(true)
			} else {
				return func() tea.Msg {
					return NavigateMsg{Action: NavBack}
				}
			}
		case "?":
			return func() tea.Msg {
				return NavigateMsg{Location: Location{Path: "/search/help"}, Action: NavPush}
			}
		default:
			var cmd tea.Cmd
			v.input, cmd = v.input.Update(msg)
			query := v.input.Value()
			if query != v.query {
				v.query = query
				terms := ExtractSearchTerms(query)
				if len(terms) >= 3 {
					v.loading = true
					return tea.Batch(cmd, v.doSearch())
				}
				v.results.SetItems(nil)
				v.total = 0
				v.totalItems = 0
				v.pag.Reset()
				v.searchOffset = 0
			}
			return cmd
		}
		return nil
	}

	consumed, activate, link := v.results.Update(msg)
	if link != nil {
		return func() tea.Msg { return NavigateMsg{Location: *link, Action: NavPush} }
	}
	if activate {
		return v.navigateToSelected()
	}
	if consumed {
		if v.results.NearBottom() && v.pag.CanLoadMore() {
			return tea.Batch(ConsumedCmd, v.doSearchMore())
		}
		return ConsumedCmd
	}
	switch msg.String() {
	case "/":
		v.inputMode = true
		v.results.SetActive(false)
		return v.input.Focus()
	case "?":
		return func() tea.Msg {
			return NavigateMsg{Location: Location{Path: "/search/help"}, Action: NavPush}
		}
	case "up", "k":
		if v.results.Selected() == 0 {
			v.inputMode = true
			v.results.SetActive(false)
			return v.input.Focus()
		}
	}
	return nil
}

// handleSearchResults processes search results.
func (v *SearchView) handleSearchResults(msg SearchResultsMsg) tea.Cmd {
	// Ignore stale results from a previous query
	if msg.Query != v.query {
		return nil
	}
	v.loading = false
	v.pag.Loading = false
	if msg.Err != nil {
		return nil
	}

	items, trimmedMore := TrimPage(msg.Result.Items, PageSize)
	v.pag.HasMore = trimmedMore || msg.Result.HasMore
	v.total = msg.Result.Total
	v.totalItems = msg.Result.TotalSearched

	if msg.Append {
		v.results.AppendItems(items)
		v.searchOffset += len(items)
	} else {
		v.results.SetItems(items)
		v.searchOffset = len(items)
		if v.restoreIndex >= 0 {
			v.results.SetSelected(v.restoreIndex)
			v.restoreIndex = -1
		}
	}

	if len(v.results.Items()) > 0 {
		if !v.inputMode {
			v.results.SetActive(true)
		}
		return nil
	}
	// No results and not typing - keep focus on input
	if !v.inputMode {
		v.inputMode = true
		v.results.SetActive(false)
		return v.input.Focus()
	}
	return nil
}

// Render renders the search view.
func (v *SearchView) Render(state *State) string {
	wrapper := NewViewWrapper(state)

	var b strings.Builder
	b.WriteString(v.input.View())
	b.WriteString("\n\n")

	if v.loading {
		b.WriteString(Dim.Render("Searching..."))
	} else if len(v.results.Items()) == 0 && v.query != "" {
		b.WriteString(Dim.Render("No results"))
	} else {
		b.WriteString(v.results.View())
	}

	footer := RenderFooter(state.Registry, Search, wrapper.ContentWidth(), nil)
	return wrapper.Render(b.String(), footer)
}

// IsInputActive returns true if search input is focused.
func (v *SearchView) IsInputActive() bool {
	return v.inputMode
}

// HeaderInfo returns position and total for the header display.
func (v *SearchView) HeaderInfo() (position, total int) {
	items := v.results.Items()
	if len(items) == 0 {
		return 0, 0
	}
	if v.total > 0 {
		return v.results.Selected() + 1, v.total
	}
	return v.results.Selected() + 1, len(items)
}

// TotalSearched returns the total number of items searched.
// Returns 0 to suppress the "of X" suffix when total already shows the count.
func (v *SearchView) TotalSearched() int {
	return 0
}

// GetItemAt returns the post ID at the given index.
func (v *SearchView) GetItemAt(index int) (string, bool) {
	items := v.results.Items()
	if index >= 0 && index < len(items) {
		return items[index].ItemID(), true
	}
	return "", false
}

// GetItemCount returns the total number of items.
func (v *SearchView) GetItemCount() int {
	return len(v.results.Items())
}

// GetDisplayItemAt returns the full DisplayItem at the given index.
func (v *SearchView) GetDisplayItemAt(index int) (DisplayItem, bool) {
	items := v.results.Items()
	if index >= 0 && index < len(items) {
		return items[index], true
	}
	return nil, false
}

// DisplayItems returns all search result items (extension-agnostic).
func (v *SearchView) DisplayItems() []DisplayItem {
	return v.results.Items()
}

// SetDisplayItems replaces all search result items (extension-agnostic).
func (v *SearchView) SetDisplayItems(items []DisplayItem) {
	v.results.SetItems(items)
}

// SelectedDisplayItem returns the currently selected item (extension-agnostic).
func (v *SearchView) SelectedDisplayItem() (DisplayItem, bool) {
	return v.results.SelectedItem()
}

// Bindings returns view-specific key bindings.
func (v *SearchView) Bindings() []Binding {
	noop := func(ctx *HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []Binding{
		{Key: "enter", Label: "search/open", Contexts: []Context{Search}, Handler: noop},
		{Key: "esc", Label: "exit input", Contexts: []Context{Search}, Handler: noop},
		{Key: "down", Label: "to results", Contexts: []Context{Search}, Handler: noop},
		{Key: "up", Label: "to input", Contexts: []Context{Search}, Handler: noop},
		{Key: "?", Label: "search help", Contexts: []Context{Search}, Handler: noop},
	}
}
