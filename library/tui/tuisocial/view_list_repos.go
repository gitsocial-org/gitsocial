// list_repos.go - Repository list view for managing repos in a list
package tuisocial

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/social"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// ListReposView displays repositories in a list.
type ListReposView struct {
	list              social.List
	repos             []string
	cursor            int
	lastClickIdx      int
	externalListOwner string
	workdir           string
	allLists          []social.List
	followerSet       map[string]bool
	inputMode         bool
	input             textinput.Model
	confirm           tuicore.ConfirmDialog
	zonePrefix        string
}

// Bindings returns keybindings for the list repos view.
func (v *ListReposView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "a", Label: "add repository", Contexts: []tuicore.Context{tuicore.ListRepos}, Handler: noop},
		{Key: "x", Label: "remove repository", Contexts: []tuicore.Context{tuicore.ListRepos}, Handler: noop},
		{Key: "j", Label: "down", Contexts: []tuicore.Context{tuicore.ListRepos}, Handler: noop},
		{Key: "k", Label: "up", Contexts: []tuicore.Context{tuicore.ListRepos}, Handler: noop},
		{Key: "enter", Label: "open repo", Contexts: []tuicore.Context{tuicore.ListRepos}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.ListRepos}, Handler: push},
	}
}

// NewListReposView creates a new list repos view.
func NewListReposView(workdir string) *ListReposView {
	input := textinput.New()
	input.Placeholder = ""
	input.CharLimit = 512
	input.Prompt = "+ "
	tuicore.StyleTextInput(&input, tuicore.Title, lipgloss.NewStyle(), tuicore.Dim)
	return &ListReposView{
		workdir:      workdir,
		input:        input,
		lastClickIdx: -1,
		zonePrefix:   zone.NewPrefix(),
	}
}

// SetSize sets the view dimensions.
func (v *ListReposView) SetSize(width, height int) {
	// List repos uses text rendering, not CardList
}

// Activate loads list repos when the view becomes active.
func (v *ListReposView) Activate(state *tuicore.State) tea.Cmd {
	v.inputMode = false
	v.confirm.Reset()
	v.input.SetValue("")
	loc := state.Router.Location()
	listID := loc.Param("listID")
	owner := loc.Param("owner")

	v.externalListOwner = owner
	v.cursor = 0

	if owner != "" {
		// External list - get repos from cache
		lists, _ := cache.GetExternalRepoLists(owner)
		for _, list := range lists {
			if list.ListID == listID {
				v.list = social.List{ID: listID, Name: list.Name}
				v.repos = make([]string, 0, len(list.Repositories))
				for _, repo := range list.Repositories {
					v.repos = append(v.repos, repo.RepoURL)
				}
				break
			}
		}
	} else {
		// Workspace list
		result := social.GetLists(state.Workdir)
		if result.Success {
			for _, list := range result.Data {
				if list.ID == listID {
					v.list = list
					v.repos = list.Repositories
					break
				}
			}
		}
	}

	// Load all lists for follow status detection
	if listsResult := social.GetLists(state.Workdir); listsResult.Success {
		v.allLists = listsResult.Data
	}

	// Load follower set for mutual follow detection
	workspaceURL := gitmsg.ResolveRepoURL(state.Workdir)
	var err error
	v.followerSet, err = social.GetFollowerSet(workspaceURL)
	if err != nil {
		log.Debug("failed to get follower set", "error", err)
	}

	// Sort repos alphabetically by name
	sort.Slice(v.repos, func(i, j int) bool {
		return strings.ToLower(protocol.GetDisplayName(v.repos[i])) < strings.ToLower(protocol.GetDisplayName(v.repos[j]))
	})

	return nil
}

// Update handles messages and returns commands.
func (v *ListReposView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if v.inputMode || v.confirm.IsActive() {
			return nil
		}
		return v.handleMouse(msg)
	case tea.KeyPressMsg:
		return v.handleKey(msg, state)
	case RepoAddedMsg:
		if msg.Err == nil && msg.ListID == v.list.ID {
			v.repos = append(v.repos, msg.RepoURL)
			// Re-sort alphabetically
			sort.Slice(v.repos, func(i, j int) bool {
				return strings.ToLower(protocol.GetDisplayName(v.repos[i])) < strings.ToLower(protocol.GetDisplayName(v.repos[j]))
			})
			// Focus cursor on the newly added repo
			for i, repo := range v.repos {
				if repo == msg.RepoURL {
					v.cursor = i
					break
				}
			}
			// Update allLists so follow status shows correctly
			for i := range v.allLists {
				if v.allLists[i].ID == v.list.ID {
					v.allLists[i].Repositories = append(v.allLists[i].Repositories, msg.RepoURL)
					break
				}
			}
		}
	case RepoRemovedMsg:
		if msg.Err == nil && msg.ListID == v.list.ID {
			for i, repo := range v.repos {
				if repo == msg.RepoURL {
					v.repos = append(v.repos[:i], v.repos[i+1:]...)
					if v.cursor >= len(v.repos) && v.cursor > 0 {
						v.cursor--
					}
					break
				}
			}
			// Update allLists so follow status shows correctly
			for i := range v.allLists {
				if v.allLists[i].ID == v.list.ID {
					for j, repo := range v.allLists[i].Repositories {
						if repo == msg.RepoURL {
							v.allLists[i].Repositories = append(v.allLists[i].Repositories[:j], v.allLists[i].Repositories[j+1:]...)
							break
						}
					}
					break
				}
			}
		}
	}
	return nil
}

// handleMouse processes mouse input.
func (v *ListReposView) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg:
		idx := tuicore.ZoneClicked(msg, len(v.repos), v.zonePrefix)
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
			if v.cursor < len(v.repos)-1 {
				v.cursor++
			}
		}
	}
	return nil
}

// activateSelected navigates to the selected repository.
func (v *ListReposView) activateSelected() tea.Cmd {
	if len(v.repos) == 0 || v.cursor >= len(v.repos) {
		return nil
	}
	id := protocol.ParseRepositoryID(v.repos[v.cursor])
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocRepository(id.Repository, id.Branch),
			Action:   tuicore.NavPush,
		}
	}
}

// handleKey processes keyboard input.
func (v *ListReposView) handleKey(msg tea.KeyPressMsg, _ *tuicore.State) tea.Cmd {
	key := msg.String()
	if handled, cmd := v.confirm.HandleKey(key); handled {
		return cmd
	}
	if v.inputMode {
		switch key {
		case "enter":
			raw := strings.TrimSpace(v.input.Value())
			if raw != "" {
				v.inputMode = false
				v.input.Blur()
				repoURL, branch, allBranches := parseRepoInput(raw)
				return v.addRepo(repoURL, branch, allBranches)
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
	switch key {
	case "j", "down":
		if v.cursor < len(v.repos)-1 {
			v.cursor++
		}
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "enter":
		if len(v.repos) > 0 && v.cursor < len(v.repos) {
			id := protocol.ParseRepositoryID(v.repos[v.cursor])
			return func() tea.Msg {
				return tuicore.NavigateMsg{
					Location: tuicore.LocRepository(id.Repository, id.Branch),
					Action:   tuicore.NavPush,
				}
			}
		}
	case "a":
		if v.externalListOwner == "" {
			v.inputMode = true
			v.input.Blur()
			v.input.Reset()
			v.input.Placeholder = ""
			return v.input.Focus()
		}
	case "x":
		if v.externalListOwner == "" && len(v.repos) > 0 && v.cursor < len(v.repos) {
			repoURL := v.repos[v.cursor]
			repoName := protocol.GetDisplayName(repoURL)
			v.confirm.Show("Remove '"+repoName+"' from list?", true, func() tea.Cmd { return v.removeRepo(repoURL) })
		}
	}
	return nil
}

// removeRepo removes a repository from the list.
func (v *ListReposView) removeRepo(repoURL string) tea.Cmd {
	workdir := v.workdir
	listID := v.list.ID
	return func() tea.Msg {
		result := social.RemoveRepositoryFromList(workdir, listID, repoURL)
		if !result.Success {
			return RepoRemovedMsg{ListID: listID, RepoURL: repoURL, Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return RepoRemovedMsg{ListID: listID, RepoURL: repoURL}
	}
}

// addRepo adds a repository to the list.
func (v *ListReposView) addRepo(repoURL, branch string, allBranches bool) tea.Cmd {
	workdir := v.workdir
	listID := v.list.ID
	listName := v.list.Name
	return func() tea.Msg {
		result := social.AddRepositoryToList(workdir, listID, repoURL, branch, allBranches)
		if !result.Success {
			return RepoAddedMsg{ListID: listID, ListName: listName, RepoURL: repoURL, Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return RepoAddedMsg{ListID: listID, ListName: listName, RepoURL: result.Data}
	}
}

// Render renders the list repos view to a string.
func (v *ListReposView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)

	var b strings.Builder
	// Show input form at top for local lists
	if v.externalListOwner == "" {
		if v.inputMode {
			b.WriteString(v.input.View())
			b.WriteString("\n")
			b.WriteString(tuicore.Dim.Render("  url [branch | *]"))
		} else {
			keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.BorderFocused)).Bold(true)
			b.WriteString(keyStyle.Render("a") + tuicore.Dim.Render(":add repository"))
		}
		b.WriteString("\n\n")
	}
	if len(v.repos) == 0 {
		b.WriteString(tuicore.Dim.Render("No repositories in this list"))
	} else {
		for i, repo := range v.repos {
			selected := i == v.cursor
			id := protocol.ParseRepositoryID(repo)
			status := GetFollowStatus(id.Repository, v.allLists, v.followerSet)
			listNames := GetListNamesForRepo(id.Repository, v.allLists, v.list.ID)
			prefix := "  "
			if selected {
				prefix = tuicore.Title.Render("▸ ")
			}
			var line strings.Builder
			line.WriteString(prefix)
			name := protocol.GetDisplayName(repo)
			if selected {
				if status == FollowStatusMutual {
					line.WriteString(tuicore.MutualTitle.Background(tuicore.Selected.GetBackground()).Render(name))
				} else {
					line.WriteString(tuicore.TitleSelected.Render(name))
				}
			} else {
				if status == FollowStatusMutual {
					line.WriteString(tuicore.MutualTitle.Render(name))
				} else {
					line.WriteString(name)
				}
			}
			indicator := RenderFollowIndicator(status, listNames, selected)
			if indicator != "" {
				line.WriteString(" ")
				line.WriteString(indicator)
			}
			strippedRepo := strings.TrimPrefix(strings.TrimPrefix(repo, "https://"), "http://")
			if selected {
				line.WriteString(tuicore.DimSelected.Render(" · "))
				line.WriteString(tuicore.Hyperlink(repo, strippedRepo))
			} else {
				line.WriteString(tuicore.Dim.Render(" · "))
				line.WriteString(tuicore.Hyperlink(repo, strippedRepo))
			}
			b.WriteString(tuicore.MarkZone(tuicore.ZoneID(v.zonePrefix, i), line.String()))
			b.WriteString("\n")
		}
	}

	var footer string
	if v.confirm.IsActive() {
		footer = v.confirm.Render()
	} else {
		var exclude map[string]bool
		if v.externalListOwner != "" {
			exclude = map[string]bool{"a": true, "x": true, "p": true}
		}
		footer = tuicore.RenderFooter(state.Registry, tuicore.ListRepos, wrapper.ContentWidth(), exclude)
	}
	return wrapper.Render(b.String(), footer)
}

// IsInputActive returns true when input or confirmation is active.
func (v *ListReposView) IsInputActive() bool {
	return v.inputMode || v.confirm.IsActive()
}

// IsExternalList returns true if viewing an external list.
func (v *ListReposView) IsExternalList() bool {
	return v.externalListOwner != ""
}

// Title returns the list name for the header.
func (v *ListReposView) Title() string {
	return "☷  " + v.list.Name
}

// HeaderInfo returns position and total for the header.
func (v *ListReposView) HeaderInfo() (position, total int) {
	if len(v.repos) == 0 {
		return 0, 0
	}
	return v.cursor + 1, len(v.repos)
}
