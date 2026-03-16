// repository_lists.go - View showing lists published by an external repository
package tuisocial

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// RepoListsView displays lists published by an external repository.
type RepoListsView struct {
	repoURL      string
	repoName     string
	lists        []cache.ExternalRepoList
	cursor       int
	lastClickIdx int
	loading      bool
	workdir      string
	zonePrefix   string
}

// Bindings returns keybindings for the repo lists view.
func (v *RepoListsView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "enter", Label: "view posts", Contexts: []tuicore.Context{tuicore.RepoLists}, Handler: noop},
		{Key: "r", Label: "repositories", Contexts: []tuicore.Context{tuicore.RepoLists}, Handler: noop},
		{Key: "j", Label: "down", Contexts: []tuicore.Context{tuicore.RepoLists}, Handler: noop},
		{Key: "k", Label: "up", Contexts: []tuicore.Context{tuicore.RepoLists}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.RepoLists}, Handler: push},
	}
}

// NewRepoListsView creates a new repo lists view.
func NewRepoListsView(workdir string) *RepoListsView {
	return &RepoListsView{
		workdir:      workdir,
		lastClickIdx: -1,
		zonePrefix:   zone.NewPrefix(),
	}
}

// SetSize sets the view dimensions.
func (v *RepoListsView) SetSize(width, height int) {
	// Uses text rendering, not CardList
}

// Activate loads the lists when the view becomes active.
func (v *RepoListsView) Activate(state *tuicore.State) tea.Cmd {
	v.loading = true
	v.cursor = 0
	loc := state.Router.Location()
	v.repoURL = loc.Param("url")
	v.repoName = protocol.GetDisplayName(v.repoURL)
	return v.loadLists()
}

// loadLists fetches the external repo's published lists.
func (v *RepoListsView) loadLists() tea.Cmd {
	repoURL := v.repoURL
	return func() tea.Msg {
		lists, err := cache.GetExternalRepoLists(repoURL)
		return RepoListsLoadedMsg{Lists: lists, Err: err}
	}
}

// Update handles messages and returns commands.
func (v *RepoListsView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if v.loading {
			return nil
		}
		return v.handleMouse(msg)
	case tea.KeyPressMsg:
		return v.handleKey(msg, state)
	case RepoListsLoadedMsg:
		v.handleLoaded(msg)
	}
	return nil
}

// handleMouse processes mouse input.
func (v *RepoListsView) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg:
		idx := tuicore.ZoneClicked(msg, len(v.lists), v.zonePrefix)
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
		if m.Button == tea.MouseWheelUp {
			if v.cursor > 0 {
				v.cursor--
			}
		} else {
			if v.cursor < len(v.lists)-1 {
				v.cursor++
			}
		}
	}
	return nil
}

// activateSelected navigates to the selected list's posts.
func (v *RepoListsView) activateSelected() tea.Cmd {
	if len(v.lists) == 0 || v.cursor >= len(v.lists) {
		return nil
	}
	list := v.lists[v.cursor]
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocExternalList(v.repoURL, list.ListID),
			Action:   tuicore.NavPush,
		}
	}
}

// handleKey processes keyboard input.
func (v *RepoListsView) handleKey(msg tea.KeyPressMsg, _ *tuicore.State) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		if v.cursor < len(v.lists)-1 {
			v.cursor++
		}
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "enter":
		if len(v.lists) > 0 && v.cursor < len(v.lists) {
			list := v.lists[v.cursor]
			return func() tea.Msg {
				return tuicore.NavigateMsg{
					Location: tuicore.LocExternalList(v.repoURL, list.ListID),
					Action:   tuicore.NavPush,
				}
			}
		}
	case "r":
		if len(v.lists) > 0 && v.cursor < len(v.lists) {
			list := v.lists[v.cursor]
			return func() tea.Msg {
				return tuicore.NavigateMsg{
					Location: tuicore.LocExternalListRepos(v.repoURL, list.ListID),
					Action:   tuicore.NavPush,
				}
			}
		}
	}
	return nil
}

// handleLoaded processes the loaded lists data.
func (v *RepoListsView) handleLoaded(msg RepoListsLoadedMsg) {
	v.loading = false
	if msg.Err != nil {
		return
	}
	v.lists = msg.Lists
}

// Render renders the repo lists view to a string.
func (v *RepoListsView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	var b strings.Builder
	if v.loading {
		b.WriteString(tuicore.Dim.Render("Loading lists..."))
	} else if len(v.lists) == 0 {
		b.WriteString(tuicore.Dim.Render("No lists published"))
		b.WriteString("\n\n")
		b.WriteString(tuicore.Dim.Render("Press 'esc' to go back"))
	} else {
		for i, list := range v.lists {
			prefix := "  "
			if i == v.cursor {
				prefix = tuicore.Title.Render("▸ ")
			}
			var line strings.Builder
			line.WriteString(prefix)
			name := list.Name
			if name == "" {
				name = list.ListID
			}
			if i == v.cursor {
				line.WriteString(tuicore.Title.Render(name))
			} else {
				line.WriteString(name)
			}
			line.WriteString(tuicore.Dim.Render(" (" + list.ListID + ")"))
			repoCount := len(list.Repositories)
			if repoCount > 0 {
				line.WriteString(tuicore.Dim.Render(" · "))
				if repoCount == 1 {
					line.WriteString(tuicore.Dim.Render("1 repo"))
				} else {
					line.WriteString(tuicore.Dim.Render(fmt.Sprintf("%d repos", repoCount)))
				}
			}
			b.WriteString(tuicore.MarkZone(tuicore.ZoneID(v.zonePrefix, i), line.String()))
			b.WriteString("\n")
		}
	}
	footer := tuicore.RenderFooter(state.Registry, tuicore.RepoLists, wrapper.ContentWidth(), nil)
	return wrapper.Render(b.String(), footer)
}

// Title returns the view title for the header.
func (v *RepoListsView) Title() string {
	if v.repoName != "" {
		return "☷  " + v.repoName + " lists"
	}
	return "☷  Repository Lists"
}
