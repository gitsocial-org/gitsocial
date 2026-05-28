// view_explore.go - Repository discovery: browse all known repos (explore),
// list repos related to a given one (related), and browse the workspace's
// followers — mirroring the CLI explore/related/followers commands. One view
// serves all three since each is a list of repos with follow indicators.
package tuisocial

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

type exploreMode int

const (
	modeExplore   exploreMode = iota // /explore — every known repo
	modeRelated                      // /explore/related — repos related to targetURL
	modeFollowers                    // /social/followers — workspaces that follow us
)

// exploreRow is one rendered repository row plus optional relationship hints
// (only populated in related mode).
type exploreRow struct {
	url    string
	branch string
	name   string
	hints  string
}

// ExploreView lists repositories for discovery. The mode is resolved from the
// route. Selecting a row opens the repository view; `r` drills into the related
// repos of the selection.
type ExploreView struct {
	workdir      string
	mode         exploreMode
	targetURL    string
	rows         []exploreRow
	cursor       int
	loaded       bool
	allLists     []social.List
	followerSet  map[string]bool
	lastClickIdx int
	zonePrefix   string
}

// NewExploreView creates a new repository discovery view.
func NewExploreView(workdir string) *ExploreView {
	return &ExploreView{workdir: workdir, lastClickIdx: -1, zonePrefix: zone.NewPrefix()}
}

// SetSize is a no-op — the view renders plain rows, not a sized component.
func (v *ExploreView) SetSize(width, height int) {}

// Activate resolves the mode from the route and loads the repository list.
func (v *ExploreView) Activate(state *tuicore.State) tea.Cmd {
	loc := state.Router.Location()
	v.targetURL = ""
	switch {
	case loc.Param("related") != "":
		v.mode = modeRelated
		v.targetURL = loc.Param("related")
	case loc.Path == "/social/followers":
		v.mode = modeFollowers
	default:
		v.mode = modeExplore
	}
	v.cursor = 0
	v.loaded = false
	v.rows = nil
	v.lastClickIdx = -1
	if listsResult := social.GetLists(state.Workdir); listsResult.Success {
		v.allLists = listsResult.Data
	}
	if set, err := social.GetFollowerSet(gitmsg.ResolveRepoURL(state.Workdir)); err == nil {
		v.followerSet = set
	} else {
		log.Debug("explore: follower set", "error", err)
	}
	return v.load()
}

// load fetches the repository list for the current mode off the UI thread.
func (v *ExploreView) load() tea.Cmd {
	workdir := v.workdir
	mode := v.mode
	target := v.targetURL
	return func() tea.Msg {
		switch mode {
		case modeRelated:
			res := social.GetRelatedRepositories(workdir, target)
			if !res.Success {
				return exploreLoadedMsg{err: fmt.Errorf("%s", res.Error.Message)}
			}
			rows := make([]exploreRow, 0, len(res.Data))
			for _, r := range res.Data {
				rows = append(rows, exploreRow{
					url:    r.URL,
					branch: r.Branch,
					name:   protocol.GetDisplayName(r.URL),
					hints:  relationshipHints(r.Relationships),
				})
			}
			return exploreLoadedMsg{rows: rows}
		case modeFollowers:
			urls, err := social.GetFollowers(gitmsg.ResolveRepoURL(workdir))
			if err != nil {
				return exploreLoadedMsg{err: err}
			}
			rows := make([]exploreRow, 0, len(urls))
			for _, u := range urls {
				rows = append(rows, exploreRow{url: u, name: protocol.GetDisplayName(u)})
			}
			return exploreLoadedMsg{rows: rows}
		default:
			res := social.GetRepositories(workdir, "all", 0)
			if !res.Success {
				return exploreLoadedMsg{err: fmt.Errorf("%s", res.Error.Message)}
			}
			rows := make([]exploreRow, 0, len(res.Data))
			for _, r := range res.Data {
				rows = append(rows, exploreRow{url: r.URL, branch: r.Branch, name: protocol.GetDisplayName(r.URL)})
			}
			return exploreLoadedMsg{rows: rows}
		}
	}
}

// relationshipHints summarizes why two repos are related (shared lists/authors).
func relationshipHints(rel social.RelationshipInfo) string {
	var parts []string
	if n := len(rel.SharedLists); n > 0 {
		parts = append(parts, fmt.Sprintf("%d shared list(s)", n))
	}
	if n := len(rel.SharedAuthors); n > 0 {
		parts = append(parts, fmt.Sprintf("%d shared author(s)", n))
	}
	return strings.Join(parts, " · ")
}

type exploreLoadedMsg struct {
	rows []exploreRow
	err  error
}

// Update handles the async load result, mouse, and key input.
func (v *ExploreView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case exploreLoadedMsg:
		v.loaded = true
		if msg.err != nil {
			return nil
		}
		// Explore + followers sort alphabetically; related preserves the
		// library's relevance ordering.
		if v.mode != modeRelated {
			sort.Slice(msg.rows, func(i, j int) bool {
				return strings.ToLower(msg.rows[i].name) < strings.ToLower(msg.rows[j].name)
			})
		}
		v.rows = msg.rows
		if v.cursor >= len(v.rows) {
			v.cursor = len(v.rows) - 1
		}
		if v.cursor < 0 {
			v.cursor = 0
		}
		return nil
	case tea.MouseMsg:
		return v.handleMouse(msg)
	case tea.KeyPressMsg:
		return v.handleKey(msg)
	}
	return nil
}

// handleMouse moves the cursor on wheel and opens a repo on double-click.
func (v *ExploreView) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg:
		idx := tuicore.ZoneClicked(msg, len(v.rows), v.zonePrefix)
		if idx >= 0 {
			if idx == v.lastClickIdx && idx == v.cursor {
				v.lastClickIdx = -1
				return v.openSelected()
			}
			v.cursor = idx
			v.lastClickIdx = idx
		}
	case tea.MouseWheelMsg:
		m := msg.Mouse()
		if m.Button == tea.MouseWheelUp {
			if v.cursor > 0 {
				v.cursor--
			}
		} else if v.cursor < len(v.rows)-1 {
			v.cursor++
		}
	}
	return nil
}

// handleKey processes list navigation, open, and related drill-down.
func (v *ExploreView) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		if v.cursor < len(v.rows)-1 {
			v.cursor++
		}
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "g", "home":
		v.cursor = 0
	case "G", "end":
		if len(v.rows) > 0 {
			v.cursor = len(v.rows) - 1
		}
	case "enter":
		return v.openSelected()
	case "r":
		if v.cursor >= 0 && v.cursor < len(v.rows) {
			url := v.rows[v.cursor].url
			return func() tea.Msg {
				return tuicore.NavigateMsg{Location: tuicore.LocExploreRelated(url), Action: tuicore.NavPush}
			}
		}
	}
	return nil
}

// openSelected navigates to the selected repository's view.
func (v *ExploreView) openSelected() tea.Cmd {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return nil
	}
	row := v.rows[v.cursor]
	return func() tea.Msg {
		return tuicore.NavigateMsg{Location: tuicore.LocRepository(row.url, row.branch), Action: tuicore.NavPush}
	}
}

// IsInputActive returns false — the view never owns text input.
func (v *ExploreView) IsInputActive() bool { return false }

// Bindings returns the view's keybindings.
func (v *ExploreView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []tuicore.Binding{
		{Key: "enter", Label: "open", Contexts: []tuicore.Context{tuicore.Explore}, Handler: noop},
		{Key: "r", Label: "related", Contexts: []tuicore.Context{tuicore.Explore}, Handler: noop},
		{Key: "j", Label: "down", Contexts: []tuicore.Context{tuicore.Explore}, Handler: noop},
		{Key: "k", Label: "up", Contexts: []tuicore.Context{tuicore.Explore}, Handler: noop},
	}
}

// Title returns the header for the current mode.
func (v *ExploreView) Title() string {
	switch v.mode {
	case modeRelated:
		return "➼  Related · " + protocol.GetDisplayName(v.targetURL)
	case modeFollowers:
		return "㋡  My Followers"
	default:
		return "➼  Explore"
	}
}

// HeaderInfo returns the position indicator for the title bar.
func (v *ExploreView) HeaderInfo() (int, string) {
	if len(v.rows) == 0 {
		return 0, ""
	}
	return v.cursor + 1, fmt.Sprintf("%d", len(v.rows))
}

// Render draws the repository list as a table: name | status | url | hints.
func (v *ExploreView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	var b strings.Builder
	switch {
	case !v.loaded:
		b.WriteString("Loading repositories...")
	case len(v.rows) == 0 && v.mode == modeFollowers:
		b.WriteString(tuicore.Dim.Render("No followers detected yet — run fetch to detect them"))
	case len(v.rows) == 0 && v.mode == modeRelated:
		b.WriteString(tuicore.Dim.Render("No related repositories found"))
	case len(v.rows) == 0:
		b.WriteString(tuicore.Dim.Render("No repositories found — follow repos or add them to lists"))
	default:
		b.WriteString(v.renderTable(wrapper.ContentWidth()))
	}
	footer := tuicore.RenderFooter(state.Registry, tuicore.Explore, nil)
	return wrapper.Render(b.String(), footer)
}

const (
	exploreNameColWidth   = 30
	exploreStatusColWidth = 22
)

// renderTable renders the loaded rows as a fixed-column table with hover/select
// highlighting that extends to the full content width.
func (v *ExploreView) renderTable(contentWidth int) string {
	var b strings.Builder
	selectedBg := tuicore.Selected.GetBackground()
	bgFill := lipgloss.NewStyle().Background(selectedBg)

	for i, row := range v.rows {
		selected := i == v.cursor
		status := GetFollowStatus(row.url, v.allLists, v.followerSet)
		listNames := GetListNamesForRepo(row.url, v.allLists, "")

		// Cursor + name column
		prefix := "  "
		if selected {
			prefix = tuicore.TitleSelected.Render("▸ ")
		}
		var nameInner string
		switch {
		case selected && status == FollowStatusMutual:
			nameInner = tuicore.MutualTitle.Background(selectedBg).Render(row.name)
		case selected:
			nameInner = tuicore.NormalSelected.Render(row.name)
		case status == FollowStatusMutual:
			nameInner = tuicore.MutualTitle.Render(row.name)
		default:
			nameInner = row.name
		}
		nameColStyle := lipgloss.NewStyle().Width(exploreNameColWidth)
		if selected {
			nameColStyle = nameColStyle.Background(selectedBg)
		}
		nameCol := nameColStyle.Render(nameInner)

		// Status column
		indicator := RenderFollowIndicator(status, listNames, selected)
		statusColStyle := lipgloss.NewStyle().Width(exploreStatusColWidth)
		if selected {
			statusColStyle = statusColStyle.Background(selectedBg)
		}
		statusCol := statusColStyle.Render(indicator)

		// URL (dimmed, hyperlinked)
		stripped := strings.TrimPrefix(strings.TrimPrefix(row.url, "https://"), "http://")
		urlText := tuicore.Hyperlink(row.url, stripped)
		var urlCol string
		if selected {
			urlCol = tuicore.DimSelected.Render(urlText)
		} else {
			urlCol = tuicore.Dim.Render(urlText)
		}

		// Hints (related mode only)
		var hintsCol string
		if row.hints != "" {
			hintsText := "  (" + row.hints + ")"
			if selected {
				hintsCol = tuicore.DimSelected.Render(hintsText)
			} else {
				hintsCol = tuicore.Dim.Render(hintsText)
			}
		}

		line := prefix + nameCol + statusCol + urlCol + hintsCol

		// Extend selected background to the full content width.
		if selected {
			if pad := contentWidth - tuicore.AnsiWidth(line); pad > 0 {
				line += bgFill.Render(strings.Repeat(" ", pad))
			}
		}

		b.WriteString(tuicore.MarkZone(tuicore.ZoneID(v.zonePrefix, i), line))
		b.WriteString("\n")
	}
	return b.String()
}
