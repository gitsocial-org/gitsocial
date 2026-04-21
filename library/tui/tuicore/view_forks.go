// view_forks.go - Fork management view for registering/removing fork repositories
package tuicore

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

func init() {
	RegisterViewMeta(ViewMeta{Path: "/config/forks", Context: CoreForks, Title: "Forks", Icon: "⑂", NavItemID: "config.forks"})
	RegisterMessageHandler(handleForkMessages)
}

// forkSort identifies the sort mode for the forks list.
type forkSort int

const (
	forkSortAlpha   forkSort = iota // alphabetical by display name
	forkSortFetched                 // most recently fetched first
	forkSortCommits                 // most commits first
)

// ForksView displays registered forks with add/remove support.
type ForksView struct {
	workdir      string
	forks        []string
	cursor       int
	scroll       int
	lastClickIdx int

	// Input modes
	inputMode bool
	input     textinput.Model
	confirm   ConfirmDialog
	choice    ChoiceDialog

	// Search
	searchActive bool
	searchInput  textinput.Model
	searchQuery  string

	// Sort
	sortMode forkSort

	zonePrefix string
}

// NewForksView creates a new forks management view.
func NewForksView(workdir string) *ForksView {
	input := textinput.New()
	input.Placeholder = "Fork repository URL..."
	input.CharLimit = 512
	input.Prompt = "+ "
	StyleTextInput(&input, Title, lipgloss.NewStyle(), Dim)

	searchInput := textinput.New()
	searchInput.Placeholder = ""
	searchInput.CharLimit = 100
	searchInput.Prompt = "/ "
	StyleTextInput(&searchInput, Title, Title, Dim)

	return &ForksView{
		workdir:      workdir,
		input:        input,
		searchInput:  searchInput,
		lastClickIdx: -1,
		zonePrefix:   zone.NewPrefix(),
	}
}

// SetSize sets the view dimensions.
func (v *ForksView) SetSize(width, height int) {}

// Activate loads forks when the view becomes active.
func (v *ForksView) Activate(state *State) tea.Cmd {
	v.inputMode = false
	v.searchActive = false
	v.searchQuery = ""
	v.searchInput.SetValue("")
	v.confirm.Reset()
	v.choice.Reset()
	v.input.SetValue("")
	v.cursor = 0
	v.scroll = 0
	v.forks = gitmsg.GetForks(v.workdir)
	v.sortForks()
	return nil
}

// Deactivate is called when the view is hidden.
func (v *ForksView) Deactivate() {}

// Update handles messages and returns commands.
func (v *ForksView) Update(msg tea.Msg, state *State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if v.inputMode || v.confirm.IsActive() || v.searchActive {
			return nil
		}
		return v.handleMouse(msg)
	case tea.KeyPressMsg:
		return v.handleKey(msg, state)
	case ForkAddedMsg:
		if msg.Err != nil {
			return nil
		}
		v.forks = append(v.forks, msg.ForkURL)
		v.sortForks()
		for i, f := range v.forks {
			if f == msg.ForkURL {
				v.cursor = i
				break
			}
		}
	case ForkRemovedMsg:
		if msg.Err != nil {
			return nil
		}
		for i, f := range v.forks {
			if f == msg.ForkURL {
				v.forks = append(v.forks[:i], v.forks[i+1:]...)
				if v.cursor >= len(v.forks) && v.cursor > 0 {
					v.cursor--
				}
				break
			}
		}
	default:
		if v.inputMode {
			var cmd tea.Cmd
			v.input, cmd = v.input.Update(msg)
			return cmd
		}
		if v.searchActive {
			var cmd tea.Cmd
			v.searchInput, cmd = v.searchInput.Update(msg)
			return cmd
		}
	}
	return nil
}

// handleMouse processes mouse input.
func (v *ForksView) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg:
		idx := ZoneClicked(msg, len(v.filteredIndices()), v.zonePrefix)
		if idx >= 0 {
			if idx == v.lastClickIdx && idx == v.cursor {
				v.lastClickIdx = -1
				return v.activateSelected()
			}
			v.cursor = idx
			v.lastClickIdx = idx
		}
	case tea.MouseWheelMsg:
		m := msg.Mouse()
		count := len(v.filteredIndices())
		if m.Button == tea.MouseWheelUp {
			if v.cursor > 0 {
				v.cursor--
			}
		} else {
			if v.cursor < count-1 {
				v.cursor++
			}
		}
	}
	return nil
}

// activateSelected navigates to the selected fork repository.
func (v *ForksView) activateSelected() tea.Cmd {
	indices := v.filteredIndices()
	if len(indices) == 0 || v.cursor >= len(indices) {
		return nil
	}
	repoURL := v.forks[indices[v.cursor]]
	return func() tea.Msg {
		return NavigateMsg{
			Location: LocRepository(repoURL, ""),
			Action:   NavPush,
		}
	}
}

// handleKey processes keyboard input.
func (v *ForksView) handleKey(msg tea.KeyPressMsg, state *State) tea.Cmd {
	key := msg.String()

	if handled, cmd := v.confirm.HandleKey(key); handled {
		return cmd
	}
	if handled, cmd := v.choice.HandleKey(key); handled {
		return cmd
	}

	if v.searchActive {
		return v.handleSearchKey(msg)
	}
	if v.inputMode {
		return v.handleInputKey(msg)
	}

	count := len(v.filteredIndices())
	halfPage := state.InnerHeight() / 2

	switch key {
	case "j", "down":
		if v.cursor < count-1 {
			v.cursor++
		}
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "ctrl+d", "pgdown":
		v.cursor += halfPage
		if v.cursor >= count {
			v.cursor = count - 1
		}
		if v.cursor < 0 {
			v.cursor = 0
		}
	case "ctrl+u", "pgup":
		v.cursor -= halfPage
		if v.cursor < 0 {
			v.cursor = 0
		}
	case "home":
		v.cursor = 0
	case "end":
		if count > 0 {
			v.cursor = count - 1
		}
	case "enter":
		return v.activateSelected()
	case "a":
		v.inputMode = true
		v.input.Blur()
		v.input.Reset()
		v.input.Placeholder = ""
		return v.input.Focus()
	case "x":
		indices := v.filteredIndices()
		if len(indices) > 0 && v.cursor < len(indices) {
			forkURL := v.forks[indices[v.cursor]]
			name := protocol.GetDisplayName(forkURL)
			v.confirm.Show("Remove fork '"+name+"'?", true, func() tea.Cmd { return v.removeFork(forkURL) })
		}
	case "/":
		v.searchActive = true
		v.searchInput.SetValue(v.searchQuery)
		return v.searchInput.Focus()
	case "v":
		v.choice.Show("Sort by:", []Choice{
			{Key: "a", Label: "name"},
			{Key: "f", Label: "fetched"},
			{Key: "c", Label: "commits"},
		}, func(key string) tea.Cmd {
			switch key {
			case "a":
				v.sortMode = forkSortAlpha
			case "f":
				v.sortMode = forkSortFetched
			case "c":
				v.sortMode = forkSortCommits
			}
			v.sortForks()
			v.cursor = 0
			v.scroll = 0
			return nil
		})
	}
	return nil
}

// handleInputKey handles input in add-fork mode.
func (v *ForksView) handleInputKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		forkURL := strings.TrimSpace(v.input.Value())
		if forkURL != "" {
			v.inputMode = false
			v.input.Blur()
			return v.addFork(forkURL)
		}
	case "esc":
		v.inputMode = false
		v.input.Blur()
	default:
		var cmd tea.Cmd
		v.input, cmd = v.input.Update(msg)
		return cmd
	}
	return nil
}

// handleSearchKey handles input in search mode.
func (v *ForksView) handleSearchKey(msg tea.KeyPressMsg) tea.Cmd {
	count := len(v.filteredIndices())
	switch msg.String() {
	case "esc":
		v.searchActive = false
		v.searchQuery = ""
		v.searchInput.SetValue("")
		v.searchInput.Blur()
		v.cursor = 0
		v.scroll = 0
		return nil
	case "enter":
		v.searchActive = false
		v.searchInput.Blur()
		return v.activateSelected()
	case "down":
		if v.cursor < count-1 {
			v.cursor++
		}
		return nil
	case "up":
		if v.cursor > 0 {
			v.cursor--
		}
		return nil
	}
	var cmd tea.Cmd
	v.searchInput, cmd = v.searchInput.Update(msg)
	v.searchQuery = v.searchInput.Value()
	v.cursor = 0
	v.scroll = 0
	return cmd
}

// filteredIndices returns indices into v.forks matching the search query.
func (v *ForksView) filteredIndices() []int {
	if v.searchQuery == "" {
		indices := make([]int, len(v.forks))
		for i := range v.forks {
			indices[i] = i
		}
		return indices
	}
	pattern := CompileSearchPattern(v.searchQuery)
	if pattern == nil {
		indices := make([]int, len(v.forks))
		for i := range v.forks {
			indices[i] = i
		}
		return indices
	}
	var indices []int
	for i, forkURL := range v.forks {
		if pattern.MatchString(forkURL) {
			indices = append(indices, i)
		}
	}
	return indices
}

// sortForks sorts the forks list by the current sort mode.
func (v *ForksView) sortForks() {
	switch v.sortMode {
	case forkSortAlpha:
		sort.Slice(v.forks, func(i, j int) bool {
			return strings.ToLower(v.forks[i]) < strings.ToLower(v.forks[j])
		})
	case forkSortFetched:
		sort.Slice(v.forks, func(i, j int) bool {
			return forkFetchTime(v.forks[i]).After(forkFetchTime(v.forks[j]))
		})
	case forkSortCommits:
		sort.Slice(v.forks, func(i, j int) bool {
			return forkCommitCount(v.forks[i]) > forkCommitCount(v.forks[j])
		})
	}
}

// addFork registers a fork URL.
func (v *ForksView) addFork(forkURL string) tea.Cmd {
	workdir := v.workdir
	return func() tea.Msg {
		normalized := protocol.NormalizeURL(forkURL)
		err := gitmsg.AddFork(workdir, forkURL)
		return ForkAddedMsg{ForkURL: normalized, Err: err}
	}
}

// removeFork removes a fork URL.
func (v *ForksView) removeFork(forkURL string) tea.Cmd {
	workdir := v.workdir
	return func() tea.Msg {
		err := gitmsg.RemoveFork(workdir, forkURL)
		return ForkRemovedMsg{ForkURL: forkURL, Err: err}
	}
}

// Render renders the forks view.
func (v *ForksView) Render(state *State) string {
	wrapper := NewViewWrapper(state)
	height := wrapper.ContentHeight()

	var lines []string
	if v.inputMode {
		lines = append(lines, v.input.View(), "")
	}
	if v.searchActive {
		lines = append(lines, v.searchInput.View(), "")
	}

	indices := v.filteredIndices()

	if len(v.forks) == 0 {
		lines = append(lines, Dim.Render("No forks registered"))
	} else if len(indices) == 0 {
		lines = append(lines, Dim.Render("No matches"))
	} else {
		// Keep cursor in bounds
		if v.cursor >= len(indices) {
			v.cursor = len(indices) - 1
		}
		if v.cursor < 0 {
			v.cursor = 0
		}

		// Keep cursor in view (account for header line)
		listHeight := height - len(lines) - 1 // -1 for header
		if listHeight < 1 {
			listHeight = 1
		}
		if v.cursor < v.scroll {
			v.scroll = v.cursor
		} else if v.cursor >= v.scroll+listHeight {
			v.scroll = v.cursor - listHeight + 1
		}
		if v.scroll < 0 {
			v.scroll = 0
		}

		// Pre-compute to find column widths
		type forkRow struct {
			url, commits, fetched string
		}
		rows := make([]forkRow, len(indices))
		maxCommits := len("Commits")
		maxFetched := len("Fetched")
		for ri, idx := range indices {
			forkURL := v.forks[idx]
			c, f := forkMeta(forkURL)
			rows[ri] = forkRow{
				url:     strings.TrimPrefix(strings.TrimPrefix(forkURL, "https://"), "http://"),
				commits: c,
				fetched: f,
			}
			if len(c) > maxCommits {
				maxCommits = len(c)
			}
			if len(f) > maxFetched {
				maxFetched = len(f)
			}
		}
		commitsCol := lipgloss.NewStyle().Width(maxCommits).Align(lipgloss.Right)
		fetchedCol := lipgloss.NewStyle().Width(maxFetched)

		// Header with sort indicator
		commitsHeader := "Commits"
		fetchedHeader := "Fetched"
		urlHeader := "URL"
		switch v.sortMode {
		case forkSortCommits:
			commitsHeader += " ↓"
		case forkSortFetched:
			fetchedHeader += " ↓"
		case forkSortAlpha:
			urlHeader += " ↓"
		}
		header := fmt.Sprintf("  %s  %s  %s",
			Dim.Render(commitsCol.Render(commitsHeader)),
			Dim.Render(fetchedCol.Render(fetchedHeader)),
			Dim.Render(urlHeader),
		)
		lines = append(lines, header)

		// Visible slice
		end := v.scroll + listHeight
		if end > len(indices) {
			end = len(indices)
		}
		visibleRows := rows[v.scroll:end]
		visibleIndices := indices[v.scroll:end]

		searchPattern := CompileSearchPattern(v.searchQuery)

		for vi, r := range visibleRows {
			listIdx := v.scroll + vi
			selected := listIdx == v.cursor
			prefix := "  "
			if selected {
				prefix = Title.Render("▸ ")
			}
			dim := Dim
			if selected {
				dim = DimSelected
			}

			var line strings.Builder
			line.WriteString(prefix)
			line.WriteString(dim.Render(commitsCol.Render(r.commits)))
			line.WriteString(dim.Render("  "))
			line.WriteString(dim.Render(fetchedCol.Render(r.fetched)))
			line.WriteString(dim.Render("  "))
			forkURL := v.forks[visibleIndices[vi]]
			if searchPattern != nil {
				line.WriteString(highlightMatch(r.url, searchPattern))
			} else if selected {
				line.WriteString(Hyperlink(forkURL, TitleSelected.Render(r.url)))
			} else {
				line.WriteString(Hyperlink(forkURL, r.url))
			}
			lines = append(lines, MarkZone(ZoneID(v.zonePrefix, listIdx), line.String()))
		}
	}

	// Pad to fill height so footer stays at bottom
	for len(lines) < height {
		lines = append(lines, "")
	}

	var footer string
	if v.confirm.IsActive() {
		footer = v.confirm.Render()
	} else if v.choice.IsActive() {
		footer = v.choice.Render()
	} else {
		footer = RenderFooter(state.Registry, CoreForks, wrapper.ContentWidth(), nil)
	}
	return wrapper.Render(strings.Join(lines[:height], "\n"), footer)
}

// highlightMatch highlights search matches in a fork name.
func highlightMatch(text string, pattern *regexp.Regexp) string {
	return pattern.ReplaceAllStringFunc(text, func(match string) string {
		return Highlight.Render(match)
	})
}

// IsInputActive returns true when input or confirmation is active.
func (v *ForksView) IsInputActive() bool {
	return v.inputMode || v.confirm.IsActive() || v.searchActive || v.choice.IsActive()
}

// Title returns the view title.
func (v *ForksView) Title() string {
	indices := v.filteredIndices()
	total := len(v.forks)
	if v.searchQuery != "" && len(indices) != total {
		return fmt.Sprintf("⑂  Forks (%d/%d)", len(indices), total)
	}
	return fmt.Sprintf("⑂  Forks (%d)", total)
}

// HeaderInfo returns position and total for the header.
func (v *ForksView) HeaderInfo() (position, total int) {
	indices := v.filteredIndices()
	if len(indices) == 0 {
		return 0, 0
	}
	return v.cursor + 1, len(indices)
}

// Bindings returns keybindings for the forks view.
func (v *ForksView) Bindings() []Binding {
	noop := func(ctx *HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []Binding{
		{Key: "a", Label: "add fork", Contexts: []Context{CoreForks}, Handler: noop},
		{Key: "x", Label: "remove fork", Contexts: []Context{CoreForks}, Handler: noop},
		{Key: "v", Label: "sort", Contexts: []Context{CoreForks}, Handler: noop},
		{Key: "/", Label: "search", Contexts: []Context{CoreForks}, Handler: noop},
		{Key: "enter", Label: "open repo", Contexts: []Context{CoreForks}, Handler: noop},
	}
}

// ForkAddedMsg is sent when a fork is added.
type ForkAddedMsg struct {
	ForkURL string
	Err     error
}

// ForkRemovedMsg is sent when a fork is removed.
type ForkRemovedMsg struct {
	ForkURL string
	Err     error
}

// forkMeta returns commits count and last fetch time as separate strings.
func forkMeta(forkURL string) (commits, fetched string) {
	meta, err := cache.GetRepositoryFetchMeta(forkURL)
	if err != nil || !meta.HasCommits {
		repo, err := cache.GetRepository(forkURL)
		if err != nil || !repo.LastFetch.Valid {
			return "-", "-"
		}
		if t, err := time.Parse(time.RFC3339, repo.LastFetch.String); err == nil {
			return "0", FormatTime(t)
		}
		return "0", "-"
	}
	commits = fmt.Sprintf("%d", meta.CommitCount)
	repo, err := cache.GetRepository(forkURL)
	if err == nil && repo.LastFetch.Valid {
		if t, err := time.Parse(time.RFC3339, repo.LastFetch.String); err == nil {
			return commits, FormatTime(t)
		}
	}
	return commits, "-"
}

// forkFetchTime returns the last fetch time for sorting (zero time if unknown).
func forkFetchTime(forkURL string) time.Time {
	repo, err := cache.GetRepository(forkURL)
	if err != nil || !repo.LastFetch.Valid {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, repo.LastFetch.String)
	if err != nil {
		return time.Time{}
	}
	return t
}

// forkCommitCount returns the commit count for sorting (0 if unknown).
func forkCommitCount(forkURL string) int {
	meta, err := cache.GetRepositoryFetchMeta(forkURL)
	if err != nil {
		return 0
	}
	return meta.CommitCount
}

// handleForkMessages handles fork-related messages at the core level.
func handleForkMessages(msg tea.Msg, ctx AppContext) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case ForkAddedMsg:
		return handleForkAdded(msg, ctx)
	case ForkRemovedMsg:
		return handleForkRemoved(msg, ctx)
	}
	return false, nil
}

func handleForkAdded(msg ForkAddedMsg, ctx AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), MessageTypeError)
		return true, ctx.Host().Update(msg)
	}
	msgCmd := ctx.Host().SetMessageWithTimeout(
		fmt.Sprintf("Fork added: %s", protocol.GetDisplayName(msg.ForkURL)),
		MessageTypeSuccess,
		5*time.Second,
	)
	viewCmd := ctx.Host().Update(msg)
	return true, tea.Batch(msgCmd, viewCmd)
}

func handleForkRemoved(msg ForkRemovedMsg, ctx AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), MessageTypeError)
		return true, ctx.Host().Update(msg)
	}
	msgCmd := ctx.Host().SetMessageWithTimeout(
		fmt.Sprintf("Fork removed: %s", protocol.GetDisplayName(msg.ForkURL)),
		MessageTypeSuccess,
		5*time.Second,
	)
	viewCmd := ctx.Host().Update(msg)
	return true, tea.Batch(msgCmd, viewCmd)
}
