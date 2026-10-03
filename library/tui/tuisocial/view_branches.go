// view_branches.go - Branches view: the branches of a repository with their commit counts, to pick one for the Repository view
package tuisocial

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// branchesView lists the branches of a repository; Enter opens the Repository view on the picked branch.
type branchesView struct {
	url           string
	name          string
	isWorkspace   bool
	defaultBranch string
	branches      []cache.BranchSummary
	cursor        int
	lastClickIdx  int
	loading       bool
	zonePrefix    string
}

// Bindings returns keybindings for the branches view.
func (v *branchesView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []tuicore.Binding{
		{Key: "enter", Label: "open", Contexts: []tuicore.Context{tuicore.Branches}, Handler: noop},
		{Key: "c", Label: "code", Contexts: []tuicore.Context{tuicore.Branches}, Handler: noop},
		{Key: "t", Label: "tags", Contexts: []tuicore.Context{tuicore.Branches}, Handler: noop},
		{Key: "j", Label: "down", Contexts: []tuicore.Context{tuicore.Branches}, Handler: noop},
		{Key: "k", Label: "up", Contexts: []tuicore.Context{tuicore.Branches}, Handler: noop},
	}
}

// newBranchesView creates a new branches view.
func newBranchesView() *branchesView {
	return &branchesView{lastClickIdx: -1, zonePrefix: zone.NewPrefix()}
}

// Activate reads the branches of the repository the location names, or of the workspace, from the cache.
func (v *branchesView) Activate(state *tuicore.State) tea.Cmd {
	v.loading = true
	v.cursor = 0
	v.branches = nil
	v.defaultBranch = ""
	url := state.Router.Location().Param("url")
	originURL := git.GetOriginURL(state.Workdir)
	v.isWorkspace = url == "" || protocol.NormalizeURL(url) == protocol.NormalizeURL(originURL)
	v.url = url
	v.name = protocol.GetDisplayName(url)
	repoURL := url
	if v.isWorkspace {
		v.url = ""
		v.name = "My Repository"
		repoURL = gitmsg.ResolveRepoURL(state.Workdir)
	}
	workdir := state.Workdir
	isWorkspace := v.isWorkspace
	return func() tea.Msg {
		branches, err := cache.GetRepositoryBranches(repoURL)
		msg := branchesLoadedMsg{Branches: branches, Err: err}
		if !isWorkspace {
			return msg
		}
		if msg.Default, err = fetch.DefaultBranch(workdir); err != nil {
			log.Debug("read the default branch", "error", err)
		}
		return msg
	}
}

// Update handles messages and returns commands.
func (v *branchesView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if v.loading {
			return nil
		}
		return v.handleMouse(msg)
	case tea.KeyPressMsg:
		return v.handleKey(msg)
	case branchesLoadedMsg:
		v.loading = false
		if msg.Err != nil {
			state.SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
			return nil
		}
		v.branches = msg.Branches
		v.defaultBranch = msg.Default
	}
	return nil
}

// rowCount returns the number of rows: the all-branches row and one per branch.
func (v *branchesView) rowCount() int {
	return len(v.branches) + 1
}

// moveCursor moves the cursor by delta and keeps it on a row.
func (v *branchesView) moveCursor(delta int) {
	if next := v.cursor + delta; next >= 0 && next < v.rowCount() {
		v.cursor = next
	}
}

// handleMouse processes mouse input.
func (v *branchesView) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg:
		idx := tuicore.ZoneClicked(msg, v.rowCount(), v.zonePrefix)
		if idx >= 0 {
			if idx == v.lastClickIdx && idx == v.cursor {
				v.lastClickIdx = -1
				return v.openSelected()
			}
			v.cursor = idx
			v.lastClickIdx = idx
		}
	case tea.MouseWheelMsg:
		if msg.Mouse().Button == tea.MouseWheelUp {
			v.moveCursor(-1)
		} else {
			v.moveCursor(1)
		}
	}
	return nil
}

// handleKey processes keyboard input.
func (v *branchesView) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		v.moveCursor(1)
	case "k", "up":
		v.moveCursor(-1)
	case "enter":
		return v.openSelected()
	case "c":
		return v.openCode()
	case "t":
		url := v.url
		return func() tea.Msg {
			return tuicore.NavigateMsg{Location: tuicore.LocRepoTags(url), Action: tuicore.NavPush}
		}
	}
	return nil
}

// openCode pushes the code view on the selected branch; the first row opens the default branch.
func (v *branchesView) openCode() tea.Cmd {
	if v.loading {
		return nil
	}
	branch := ""
	if v.cursor > 0 && v.cursor <= len(v.branches) {
		branch = v.branches[v.cursor-1].Name
	}
	url := v.url
	return func() tea.Msg {
		return tuicore.NavigateMsg{Location: tuicore.LocRepoCode(url, branch, "", 0, 0), Action: tuicore.NavPush}
	}
}

// openSelected replaces the location with the Repository view on the selected branch; the first row opens every branch.
func (v *branchesView) openSelected() tea.Cmd {
	if v.loading {
		return nil
	}
	branch := ""
	if v.cursor > 0 && v.cursor <= len(v.branches) {
		branch = v.branches[v.cursor-1].Name
	}
	url := v.url
	return func() tea.Msg {
		return tuicore.NavigateMsg{Location: tuicore.LocRepository(url, branch), Action: tuicore.NavReplace}
	}
}

// Render renders the branch rows to a string.
func (v *branchesView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	var b strings.Builder
	if v.loading {
		b.WriteString(tuicore.Dim.Render("Loading branches..."))
	} else {
		total := 0
		for _, branch := range v.branches {
			total += branch.Commits
		}
		b.WriteString(v.row(0, "All branches", commitsLabel(total)))
		for i, branch := range v.branches {
			mark := ""
			if branch.Name == v.defaultBranch {
				mark = "default"
			}
			b.WriteString(v.row(i+1, branch.Name, v.countLabel(branch), tuicore.FormatTime(branch.LastTime), mark))
		}
	}
	footer := tuicore.RenderFooter(state.Registry, tuicore.Branches, nil)
	return wrapper.Render(b.String(), footer)
}

// row renders one selectable line with its dim details.
func (v *branchesView) row(index int, name string, details ...string) string {
	var line strings.Builder
	if index == v.cursor {
		line.WriteString(tuicore.Title.Render("▸ " + name))
	} else {
		line.WriteString("  " + name)
	}
	for _, detail := range details {
		if detail != "" {
			line.WriteString(tuicore.Dim.Render(" · " + detail))
		}
	}
	return tuicore.MarkZone(tuicore.ZoneID(v.zonePrefix, index), line.String()) + "\n"
}

// countLabel words the count of a branch: a feature branch of the workspace holds only the commits ahead of the default branch.
func (v *branchesView) countLabel(branch cache.BranchSummary) string {
	if v.defaultBranch != "" && branch.Name != v.defaultBranch && !strings.HasPrefix(branch.Name, "gitmsg/") {
		return fmt.Sprintf("%d ahead", branch.Commits)
	}
	return commitsLabel(branch.Commits)
}

// commitsLabel words a commit count.
func commitsLabel(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}

// Title returns the view title for the header.
func (v *branchesView) Title() string {
	if v.name == "" {
		return "⎇  Branches"
	}
	return "⎇  " + v.name + " branches"
}
