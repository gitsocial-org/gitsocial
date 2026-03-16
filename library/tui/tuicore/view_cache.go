// cache.go - Cache management view for displaying and clearing cached data
package tuicore

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
)

// CacheView displays cache statistics and management.
type CacheView struct {
	stats            *cache.CacheStats
	err              string
	confirm          string
	repopulatePrompt bool
	cacheDir         string
	scroll           int
	cursor           int
	cursorLine       int
	workspaceURL     string
	deleteTarget     *cache.RepositoryInfo

	// Callback to clear memory caches in other views
	onCacheCleared func()
}

// Bindings returns keybindings for the cache view.
func (v *CacheView) Bindings() []Binding {
	return []Binding{
		{Key: "x", Label: "delete selected", Contexts: []Context{Cache},
			Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
				return true, nil
			}},
		{Key: "C", Label: "clear all", Contexts: []Context{Cache},
			Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.ClearCacheAll()
			}},
		{Key: "D", Label: "clear db", Contexts: []Context{Cache},
			Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.ClearCacheDB()
			}},
		{Key: "X", Label: "clear repos", Contexts: []Context{Cache},
			Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.ClearCacheRepos()
			}},
		{Key: "F", Label: "clear forks", Contexts: []Context{Cache},
			Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.ClearCacheForks()
			}},
		{Key: "r", Label: "refresh", Contexts: []Context{Cache},
			Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.RefreshCache()
			}},
	}
}

// NewCacheView creates a new cache view.
func NewCacheView() *CacheView {
	return &CacheView{}
}

// SetCacheClearedCallback sets the callback for when cache is cleared.
func (v *CacheView) SetCacheClearedCallback(fn func()) {
	v.onCacheCleared = fn
}

// Activate loads the cache stats when the view becomes active.
func (v *CacheView) Activate(state *State) tea.Cmd {
	v.cacheDir = state.CacheDir
	v.scroll = 0
	v.cursor = 0
	v.workspaceURL = gitmsg.ResolveRepoURL(state.Workdir)
	return v.loadCache(state.CacheDir)
}

// loadCache loads cache statistics from disk.
func (v *CacheView) loadCache(cacheDir string) tea.Cmd {
	return func() tea.Msg {
		stats, err := cache.GetStats(cacheDir)
		return CacheViewLoadedMsg{Stats: stats, Err: err}
	}
}

// CacheViewLoadedMsg is sent when cache stats are loaded.
type CacheViewLoadedMsg struct {
	Stats *cache.CacheStats
	Err   error
}

// CacheViewClearedMsg is sent when cache is cleared.
type CacheViewClearedMsg struct {
	Action string
	Err    error
}

// CacheRepoDeletedMsg is sent when a single repo is deleted.
type CacheRepoDeletedMsg struct {
	Name string
	Err  error
}

// itemCount returns the total number of selectable repo+fork items.
func (v *CacheView) itemCount() int {
	if v.stats == nil {
		return 0
	}
	return len(v.stats.TopRepos) + len(v.stats.TopForks)
}

// itemAt returns the RepositoryInfo at the given cursor index, and whether it's a fork.
func (v *CacheView) itemAt(index int) (*cache.RepositoryInfo, bool) {
	if v.stats == nil {
		return nil, false
	}
	repoCount := len(v.stats.TopRepos)
	if index < repoCount {
		return &v.stats.TopRepos[index], false
	}
	forkIndex := index - repoCount
	if forkIndex < len(v.stats.TopForks) {
		return &v.stats.TopForks[forkIndex], true
	}
	return nil, false
}

// clampCursor ensures the cursor stays within valid bounds.
func (v *CacheView) clampCursor() {
	total := v.itemCount()
	if total == 0 {
		v.cursor = 0
		return
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
	if v.cursor >= total {
		v.cursor = total - 1
	}
}

// handleLoaded updates the view with loaded cache stats.
func (v *CacheView) handleLoaded(msg CacheViewLoadedMsg) tea.Cmd {
	if msg.Err != nil {
		v.err = msg.Err.Error()
		return nil
	}
	v.stats = msg.Stats
	v.err = ""
	v.clampCursor()
	return func() tea.Msg {
		return CacheSizeMsg{Size: cache.FormatBytes(msg.Stats.TotalBytes)}
	}
}

// handleCleared processes cache cleared message and reloads stats.
func (v *CacheView) handleCleared(msg CacheViewClearedMsg, cacheDir string) tea.Cmd {
	if msg.Err != nil {
		v.err = msg.Err.Error()
		return nil
	}
	v.err = ""
	v.repopulatePrompt = true
	if v.onCacheCleared != nil {
		v.onCacheCleared()
	}
	return v.loadCache(cacheDir)
}

// handleRepoDeleted processes single repo deletion and reloads stats.
func (v *CacheView) handleRepoDeleted(msg CacheRepoDeletedMsg, cacheDir string) tea.Cmd {
	if msg.Err != nil {
		v.err = msg.Err.Error()
		return nil
	}
	v.err = ""
	if v.onCacheCleared != nil {
		v.onCacheCleared()
	}
	return v.loadCache(cacheDir)
}

// Update handles messages and returns commands.
func (v *CacheView) Update(msg tea.Msg, state *State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return v.handleKey(msg, state)
	case tea.MouseMsg:
		switch msg.(type) {
		case tea.MouseWheelMsg:
			m := msg.Mouse()
			if m.Button == tea.MouseWheelUp {
				v.scroll -= 3
				if v.scroll < 0 {
					v.scroll = 0
				}
			} else {
				v.scroll += 3
			}
		}
		return nil
	case CacheViewLoadedMsg:
		return v.handleLoaded(msg)
	case CacheViewClearedMsg:
		return v.handleCleared(msg, state.CacheDir)
	case CacheRepoDeletedMsg:
		return v.handleRepoDeleted(msg, state.CacheDir)
	}
	return nil
}

// handleKey processes keyboard input.
func (v *CacheView) handleKey(msg tea.KeyPressMsg, state *State) tea.Cmd {
	if v.confirm != "" {
		switch msg.String() {
		case "y", "Y":
			action := v.confirm
			v.confirm = ""
			return v.clearCache(state.CacheDir, action)
		case "n", "N", "esc":
			v.confirm = ""
			v.deleteTarget = nil
			return nil
		}
		return nil
	}
	if v.repopulatePrompt {
		switch msg.String() {
		case "y", "Y":
			v.repopulatePrompt = false
			return func() tea.Msg { return TriggerFetchMsg{} }
		case "n", "N", "esc":
			v.repopulatePrompt = false
			return nil
		}
		return nil
	}
	switch msg.String() {
	case "x":
		item, _ := v.itemAt(v.cursor)
		if item == nil {
			return nil
		}
		if item.RepoURL != "" && item.RepoURL == v.workspaceURL {
			v.err = "Cannot delete workspace repository"
			return nil
		}
		v.deleteTarget = item
		return v.deleteRepo()
	case "j", "down":
		v.cursor++
		v.clampCursor()
	case "k", "up":
		v.cursor--
		v.clampCursor()
	case "ctrl+d", "pgdown":
		v.cursor += state.InnerHeight() / 2
		v.clampCursor()
	case "ctrl+u", "pgup":
		v.cursor -= state.InnerHeight() / 2
		v.clampCursor()
	case "home", "g":
		v.cursor = 0
	case "end", "G":
		v.cursor = v.itemCount() - 1
		if v.cursor < 0 {
			v.cursor = 0
		}
	}
	return nil
}

// deleteRepo performs the actual repo deletion.
func (v *CacheView) deleteRepo() tea.Cmd {
	target := v.deleteTarget
	v.deleteTarget = nil
	if target == nil {
		return nil
	}
	repoURL := target.RepoURL
	path := target.Path
	name := target.URL
	return func() tea.Msg {
		var firstErr error
		if repoURL != "" {
			if err := cache.DeleteRepository(repoURL); err != nil {
				firstErr = err
			}
		}
		if path != "" {
			if err := os.RemoveAll(path); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return CacheRepoDeletedMsg{Name: name, Err: firstErr}
	}
}

// clearCache performs the actual cache clearing.
func (v *CacheView) clearCache(cacheDir, action string) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch action {
		case "db":
			err = cache.ClearDatabase(cacheDir)
		case "repos":
			err = cache.ClearRepositories(cacheDir)
		case "forks":
			err = cache.ClearForks(cacheDir)
		case "all":
			err = cache.ClearAll(cacheDir)
		}
		return CacheViewClearedMsg{Action: action, Err: err}
	}
}

// ClearCacheDB initiates database clearing with confirmation.
func (v *CacheView) ClearCacheDB() tea.Cmd {
	v.confirm = "db"
	return nil
}

// ClearCacheRepos initiates repository clearing with confirmation.
func (v *CacheView) ClearCacheRepos() tea.Cmd {
	v.confirm = "repos"
	return nil
}

// ClearCacheForks initiates fork clearing with confirmation.
func (v *CacheView) ClearCacheForks() tea.Cmd {
	v.confirm = "forks"
	return nil
}

// ClearCacheAll initiates full cache clearing with confirmation.
func (v *CacheView) ClearCacheAll() tea.Cmd {
	v.confirm = "all"
	return nil
}

// RefreshCache reloads the cache stats.
func (v *CacheView) RefreshCache() tea.Cmd {
	return v.loadCache(v.cacheDir)
}

// ReloadCacheIfActive reloads the cache if currently viewing cache view.
func (v *CacheView) ReloadCacheIfActive() tea.Cmd {
	if v.cacheDir != "" {
		return v.loadCache(v.cacheDir)
	}
	return nil
}

// IsInputActive returns true if the view is handling input.
func (v *CacheView) IsInputActive() bool {
	return v.confirm != "" || v.repopulatePrompt
}

// Render renders the cache view to a string.
func (v *CacheView) Render(state *State) string {
	wrapper := NewViewWrapper(state)

	if v.stats == nil {
		content := Dim.Render("Loading cache stats...")
		footer := RenderFooter(state.Registry, Cache, wrapper.ContentWidth(), nil)
		return wrapper.Render(content, footer)
	}

	rs := RowStylesWithWidths(80, 10)
	dbSize := cache.FormatBytesMB(v.stats.DbSizeBytes)
	repoSize := cache.FormatBytesMB(v.stats.RepoSizeBytes)
	forkSize := cache.FormatBytesMB(v.stats.ForkSizeBytes)
	totalSize := cache.FormatBytesMB(v.stats.TotalBytes)
	memorySize := cache.FormatBytesMB(v.stats.MemoryBytes)

	totalRepoCommits := 0
	for _, repo := range v.stats.TopRepos {
		totalRepoCommits += repo.Commits
	}

	rowBg := lipgloss.NewStyle().Background(lipgloss.Color(BgSelected)).Width(wrapper.ContentWidth())
	cursorIndex := 0

	var b strings.Builder

	// Memory
	memLabel := rs.Header.Width(80).Render("Memory")
	fmt.Fprintf(&b, "%s  %s", memLabel, rs.Value.Render(memorySize))
	b.WriteString("\n\n")

	// Cache header
	cacheLabel := rs.Header.Width(80).Render("Cache")
	fmt.Fprintf(&b, "%s  %s  %s", cacheLabel, rs.Value.Render(totalSize), keyStyle.Render("C")+":"+labelStyle.Render("clear all"))
	b.WriteString("\n")
	b.WriteString(rs.Dim.Render(v.stats.Location))
	b.WriteString("\n\n")

	// Database header
	dbHeader := rs.Header.Width(80).Render("Database")
	fmt.Fprintf(&b, "%s  %s  %s", dbHeader, rs.Value.Render(dbSize), keyStyle.Render("D")+":"+labelStyle.Render("clear"))
	b.WriteString("\n")
	b.WriteString(rs.Dim.Render(fmt.Sprintf("%d repos, %d commits (%s/cache.db)", v.stats.Repositories, v.stats.Items, v.stats.Location)))
	b.WriteString("\n\n")

	// Repositories header
	repoHeader := rs.Header.Width(80).Render("Repositories")
	fmt.Fprintf(&b, "%s  %s  %s", repoHeader, rs.Value.Render(repoSize), keyStyle.Render("X")+":"+labelStyle.Render("clear"))
	b.WriteString("\n")
	b.WriteString(rs.Dim.Render(fmt.Sprintf("%d repos, %d commits (%s/repositories)", v.stats.Repositories, totalRepoCommits, v.stats.Location)))
	b.WriteString("\n")

	if len(v.stats.TopRepos) > 0 {
		for _, repo := range v.stats.TopRepos {
			repoName := repo.URL
			if len(repoName) > 70 {
				repoName = "..." + repoName[len(repoName)-67:]
			}
			label := fmt.Sprintf("%s (%d)", repoName, repo.Commits)
			size := cache.FormatBytesMB(repo.Size)
			isSelected := cursorIndex == v.cursor
			isWorkspace := repo.RepoURL != "" && repo.RepoURL == v.workspaceURL
			var suffix string
			if isWorkspace {
				suffix = "(workspace)"
			} else if !repo.LastFetch.IsZero() {
				suffix = FormatTime(repo.LastFetch)
			}
			if isSelected {
				v.cursorLine = strings.Count(b.String(), "\n")
				raw := fmt.Sprintf("  %-80s  %s", label, size)
				if suffix != "" {
					raw += "  " + suffix
				}
				b.WriteString(rowBg.Render(raw))
			} else {
				row := fmt.Sprintf("  %s  %s", rs.Label.Render(label), rs.Value.Render(size))
				if suffix != "" {
					row += "  " + Dim.Render(suffix)
				}
				b.WriteString(row)
			}
			b.WriteString("\n")
			cursorIndex++
		}
	}
	b.WriteString("\n")

	// Forks header
	forkHeader := rs.Header.Width(80).Render("Forks")
	fmt.Fprintf(&b, "%s  %s  %s", forkHeader, rs.Value.Render(forkSize), keyStyle.Render("F")+":"+labelStyle.Render("clear"))
	b.WriteString("\n")
	b.WriteString(rs.Dim.Render(fmt.Sprintf("%d forks (%s/forks)", v.stats.ForkCount, v.stats.Location)))
	b.WriteString("\n")
	if len(v.stats.TopForks) > 0 {
		for _, fork := range v.stats.TopForks {
			forkName := fork.URL
			if len(forkName) > 70 {
				forkName = "..." + forkName[len(forkName)-67:]
			}
			size := cache.FormatBytesMB(fork.Size)
			isSelected := cursorIndex == v.cursor
			if isSelected {
				v.cursorLine = strings.Count(b.String(), "\n")
				b.WriteString(rowBg.Render(fmt.Sprintf("  %-80s  %s", forkName, size)))
			} else {
				fmt.Fprintf(&b, "  %s  %s", rs.Label.Render(forkName), rs.Value.Render(size))
			}
			b.WriteString("\n")
			cursorIndex++
		}
	}
	b.WriteString("\n")

	if v.confirm != "" {
		b.WriteString("\n")
		confirmStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ConfirmAction)).Bold(true)
		var what string
		switch v.confirm {
		case "db":
			what = "database"
		case "repos":
			what = "repositories"
		case "forks":
			what = "forks"
		case "all":
			what = "all cache"
		}
		b.WriteString(confirmStyle.Render(fmt.Sprintf("Delete %s? ", what)) + keyStyle.Render("y") + labelStyle.Render("/") + keyStyle.Render("n"))
	}

	if v.repopulatePrompt {
		b.WriteString("\n")
		promptStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(StatusInfo)).Bold(true)
		b.WriteString(promptStyle.Render("Repopulate cache? ") + keyStyle.Render("y") + labelStyle.Render("/") + keyStyle.Render("n"))
	}

	if v.err != "" {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(StatusError)).Render("Error: " + v.err))
	}

	// Apply scroll offset, auto-scroll to keep cursor visible
	lines := strings.Split(b.String(), "\n")
	height := wrapper.ContentHeight()
	v.ensureCursorVisible(height)
	maxScroll := len(lines) - height
	if maxScroll < 0 {
		maxScroll = 0
	}
	if v.scroll > maxScroll {
		v.scroll = maxScroll
	}
	if v.scroll < 0 {
		v.scroll = 0
	}
	end := v.scroll + height
	if end > len(lines) {
		end = len(lines)
	}
	visible := strings.Join(lines[v.scroll:end], "\n")

	footer := RenderFooter(state.Registry, Cache, wrapper.ContentWidth(), nil)

	return wrapper.Render(visible, footer)
}

// ensureCursorVisible adjusts scroll so the cursor line stays in view.
func (v *CacheView) ensureCursorVisible(viewHeight int) {
	if v.cursorLine < v.scroll {
		v.scroll = v.cursorLine
	} else if v.cursorLine >= v.scroll+viewHeight {
		v.scroll = v.cursorLine - viewHeight + 1
	}
}
