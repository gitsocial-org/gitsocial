// app.go - Main TUI application model, initialization, and event loop
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/notifications"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/settings"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/extensions/release"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/extensions/social"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/tui/tuipm"
	"github.com/gitsocial-org/gitsocial/tui/tuirelease"
	"github.com/gitsocial-org/gitsocial/tui/tuireview"
	"github.com/gitsocial-org/gitsocial/tui/tuisocial"
)

// FocusedPanel identifies which panel has input focus
type FocusedPanel int

const (
	FocusContent FocusedPanel = iota
	FocusNav
)

// Model is the main TUI model
type Model struct {
	workdir  string
	cacheDir string
	layout   Layout
	ready    bool

	// Router for navigation state
	router *tuicore.Router

	// Panels
	nav   *tuicore.NavPanel
	host  *Host
	focus FocusedPanel

	// Keybinding registry
	registry *tuicore.Registry

	// Fetch/push state
	isFetching bool
	isPushing  bool

	// Workspace fetch mode choice dialog
	fetchChoice tuicore.ChoiceDialog

	// Nav panel hidden (fullscreen diff mode)
	navHidden bool

	// Headless mode skips terminal-dependent commands (SetWindowTitle)
	headless bool

	// View cache — avoids JoinHorizontal + zone.Scan when nothing changed
	vc *viewCache
}

// viewCache stores the last composed output to skip JoinHorizontal + zone.Scan
// when neither panel changed. Pointer field so it's shared across Model value copies.
type viewCache struct {
	navView  string
	hostView string
	output   string
}

// AppContext implementation for message bus

// Workdir returns the working directory.
func (m *Model) Workdir() string { return m.workdir }

// CacheDir returns the cache directory.
func (m *Model) CacheDir() string { return m.cacheDir }

// SetHeadless enables headless mode, skipping terminal-dependent commands.
func (m *Model) SetHeadless(v bool) { m.headless = v }

// IsFetching returns true if currently fetching.
func (m *Model) IsFetching() bool { return m.isFetching }

// SetFetching sets the fetching state.
func (m *Model) SetFetching(v bool) {
	m.isFetching = v
	m.host.SetFetching(v)
}

// IsPushing returns true if currently pushing.
func (m *Model) IsPushing() bool { return m.isPushing }

// SetPushing sets the pushing state.
func (m *Model) SetPushing(v bool) {
	m.isPushing = v
	m.host.SetPushing(v)
}

// Router returns the router for navigation.
func (m *Model) Router() *tuicore.Router { return m.router }

// Host returns the host context for message handlers.
func (m *Model) Host() tuicore.HostContext { return m.host }

// Nav returns the nav context for message handlers.
func (m *Model) Nav() tuicore.NavContext { return m.nav }

// LoadLists returns a command to reload lists.
func (m *Model) LoadLists() tea.Cmd { return m.loadInitialLists() }

// LoadUnreadCount returns a command to reload unread count.
func (m *Model) LoadUnreadCount() tea.Cmd { return m.loadInitialUnreadCount() }

// LoadUnpushedCount returns a command to reload unpushed count.
func (m *Model) LoadUnpushedCount() tea.Cmd { return m.loadInitialUnpushedCount() }

// LoadUnpushedLFSCount returns a command to reload unpushed LFS count.
func (m *Model) LoadUnpushedLFSCount() tea.Cmd { return m.loadInitialUnpushedLFSCount() }

// RefreshTimeline returns a command to refresh the timeline.
func (m *Model) RefreshTimeline() tea.Cmd { return m.refreshTimeline() }

// RefreshCacheSize returns a command to recalculate and update the cache size.
func (m *Model) RefreshCacheSize() tea.Cmd { return m.loadInitialCacheSize() }

// FetchRepo returns a command to fetch a repository.
func (m *Model) FetchRepo(repoURL string) tea.Cmd { return m.fetchAddedRepo(repoURL) }

// NewModel creates a new TUI model with initial state and views.
func NewModel(workdir, cacheDir string) Model {
	// Initialize navigation registry
	navRegistry := tuicore.NewNavRegistry()
	tuicore.RegisterCoreNavItems(navRegistry)
	social.RegisterNavItems(navRegistry)
	pm.RegisterNavItems(navRegistry, workdir)
	release.RegisterNavItems(navRegistry)
	review.RegisterNavItems(navRegistry)

	// Initialize router with timeline as default
	r := tuicore.NewRouter(tuicore.LocTimeline)

	// Initialize keybinding registry
	registry := tuicore.NewRegistry()

	// Initialize shared state
	state := &tuicore.State{
		Workdir:     workdir,
		CacheDir:    cacheDir,
		UserEmail:   git.GetUserEmail(workdir),
		Registry:    registry,
		NavRegistry: navRegistry,
		Router:      r,
		Focused:     true,
	}

	// Initialize Host with shared state
	host := NewHost(state)

	// Load user settings for initial extension visibility
	settingsPath, _ := settings.DefaultPath()
	userSettings, _ := settings.Load(settingsPath)
	if !userSettings.Extensions.Social {
		navRegistry.SetHidden("social", true)
	}
	if !userSettings.Extensions.PM {
		navRegistry.SetHidden("pm", true)
	}
	if !userSettings.Extensions.Release {
		navRegistry.SetHidden("release", true)
	}
	if !userSettings.Extensions.Review {
		navRegistry.SetHidden("review", true)
	}

	// Apply display settings
	state.ShowEmailOnCards = userSettings.Display.ShowEmail

	// Register core views
	settingsView := tuicore.NewSettingsView()
	settingsView.SetDisplayChangeCallback(func(showEmail bool) {
		state.ShowEmailOnCards = showEmail
	})
	settingsView.SetExtensionChangeCallback(func(ext string, enabled bool) {
		navRegistry.SetHidden(ext, !enabled)
	})
	host.AddView("/settings", settingsView)

	configView := tuicore.NewConfigView()
	host.AddView("/config", configView)

	cacheView := tuicore.NewCacheView()
	host.AddView("/cache", cacheView)

	analyticsView := tuicore.NewAnalyticsView()
	host.AddView("/analytics", analyticsView)

	helpView := tuicore.NewHelpView()
	host.AddView("/help", helpView)

	errorLogView := tuicore.NewErrorLogView()
	host.AddView("/errorlog", errorLogView)

	commitDiffView := tuicore.NewCommitDiffView(workdir)
	host.AddView("/diff", commitDiffView)

	// Register social views
	tuisocial.Register(host)

	// Register PM views
	tuipm.Register(host)

	// Register Release views
	tuirelease.Register(host)

	// Register Review views
	tuireview.Register(host)

	// Register global keys (must be last for footer ordering)
	tuicore.RegisterGlobalKeys(registry)

	nav := tuicore.NewNavPanel(workdir, navRegistry, r)

	return Model{
		workdir:  workdir,
		cacheDir: cacheDir,
		router:   r,
		nav:      nav,
		host:     host,
		focus:    FocusContent,
		registry: registry,
		vc:       &viewCache{},
	}
}

// Init initializes the TUI model with startup commands.
func (m Model) Init() tea.Cmd {
	if m.headless {
		return tea.Batch(
			m.loadInitialCacheSize(),
			m.loadInitialLists(),
		)
	}
	return tea.Batch(
		m.host.ActivateView(),
		func() tea.Msg { return startSyncMsg{} },
		m.loadInitialStatus(),
		m.loadInitialCacheSize(),
		m.loadInitialUnreadCount(),
		m.loadInitialUnpushedCount(),
		m.loadInitialUnpushedLFSCount(),
		m.loadInitialLists(),
	)
}

// loadInitialLists loads repository lists at startup.
func (m Model) loadInitialLists() tea.Cmd {
	workdir := m.workdir
	return func() tea.Msg {
		result := social.GetLists(workdir)
		if !result.Success {
			return nil
		}
		return tuisocial.ListsLoadedMsg{Lists: result.Data}
	}
}

// loadInitialUnreadCount loads the unread notification count at startup.
func (m Model) loadInitialUnreadCount() tea.Cmd {
	workdir := m.workdir
	return func() tea.Msg {
		count, _ := notifications.GetUnreadCount(workdir)
		return tuicore.UnreadCountMsg{Count: count}
	}
}

// loadInitialUnpushedCount loads the unpushed commit count across all extension branches.
func (m Model) loadInitialUnpushedCount() tea.Cmd {
	workdir := m.workdir
	return func() tea.Msg {
		total := 0
		for _, branch := range gitmsg.GetExtBranches(workdir) {
			if counts, err := gitmsg.GetUnpushedCounts(workdir, branch); err == nil && counts != nil {
				total += counts.Posts
			}
		}
		return tuicore.UnpushedCountMsg{Count: total}
	}
}

// loadInitialUnpushedLFSCount loads the unpushed LFS objects count at startup.
func (m Model) loadInitialUnpushedLFSCount() tea.Cmd {
	workdir := m.workdir
	return func() tea.Msg {
		count := git.GetUnpushedLFSCount(workdir)
		return tuicore.UnpushedLFSCountMsg{Count: count}
	}
}

// loadInitialStatus loads the fetch status asynchronously at startup.
func (m Model) loadInitialStatus() tea.Cmd {
	workdir := m.workdir
	cacheDir := m.cacheDir
	return func() tea.Msg {
		if status := social.Status(workdir, cacheDir); status.Success {
			return fetchStatusMsg{lastFetch: status.Data.LastFetch}
		}
		return nil
	}
}

// loadInitialCacheSize loads the cache size at startup.
func (m Model) loadInitialCacheSize() tea.Cmd {
	return func() tea.Msg {
		if stats, err := cache.GetStats(m.cacheDir); err == nil {
			return tuicore.CacheSizeMsg{Size: cache.FormatBytes(stats.TotalBytes)}
		}
		return nil
	}
}

// initWorkspace syncs the workspace to cache at startup.
func (m Model) initWorkspace() tea.Cmd {
	return func() tea.Msg {
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			if err := social.SyncWorkspaceToCache(m.workdir); err != nil {
				log.Debug("background social sync failed", "error", err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := pm.SyncWorkspaceToCache(m.workdir); err != nil {
				log.Debug("background pm sync failed", "error", err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := review.SyncWorkspaceToCache(m.workdir); err != nil {
				log.Debug("background review sync failed", "error", err)
			}
		}()
		wg.Wait()
		return tuicore.WorkspaceInitializedMsg{}
	}
}

// Update handles messages and returns updated model with commands.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.layout = NewLayout(msg.Width, msg.Height)
		if m.navHidden {
			m.layout.NavWidth = 0
			m.layout.ContentWidth = m.layout.Width
		}
		m.ready = true
		m.nav.SetSize(m.layout.NavWidth, m.layout.Height)
		m.host.SetSize(m.layout.ContentWidth, m.layout.Height)
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.handleKey(msg)

	case tea.KeyReleaseMsg:
		return m, nil

	case tea.MouseReleaseMsg:
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	// Navigation messages
	case tuicore.NavigateMsg:
		return m.handleNavigate(msg)

	case tuicore.FocusMsg:
		m.setFocus(FocusedPanel(msg.Panel))
		return m, nil

	// Core data messages
	case tuicore.UnreadCountMsg:
		m.nav.SetUnreadCount(msg.Count)
		return m, nil

	case tuicore.UnpushedCountMsg:
		m.nav.SetUnpushedCount(msg.Count)
		return m, nil

	case tuicore.UnpushedLFSCountMsg:
		m.nav.SetUnpushedLFSCount(msg.Count)
		return m, nil

	case tuicore.LFSPushCompletedMsg:
		m.isPushing = false
		m.host.SetPushing(false)
		if msg.Err != nil {
			m.host.SetMessage(fmt.Sprintf("LFS push failed: %s", msg.Err), tuicore.MessageTypeError)
			return m, nil
		}
		var msgCmd tea.Cmd
		if msg.Count == 0 {
			msgCmd = m.host.SetMessageWithTimeout("No LFS objects to push", tuicore.MessageTypeSuccess, 5*time.Second)
		} else {
			msgCmd = m.host.SetMessageWithTimeout(fmt.Sprintf("Pushed %d LFS objects", msg.Count), tuicore.MessageTypeSuccess, 5*time.Second)
		}
		return m, tea.Batch(msgCmd, m.loadInitialUnpushedLFSCount())

	case tuicore.CacheSizeMsg:
		m.nav.SetCacheSize(msg.Size)
		return m, nil

	case tuicore.NavVisibilityMsg:
		m.navHidden = msg.Hidden
		if msg.Hidden {
			m.layout.NavWidth = 0
			m.layout.ContentWidth = m.layout.Width
		} else {
			m.layout = NewLayout(m.layout.Width, m.layout.Height)
		}
		m.nav.SetSize(m.layout.NavWidth, m.layout.Height)
		m.host.SetSize(m.layout.ContentWidth, m.layout.Height)
		return m, nil

	case ClearMessageMsg:
		if msg.ID == m.host.State().MessageID {
			m.host.SetMessage("", tuicore.MessageTypeNone)
		}
		return m, nil

	case fetchStatusMsg:
		m.host.SetFetchStatus(msg.lastFetch, 0)
		return m, nil

	case startSyncMsg:
		m.host.SetSyncing(true)
		return m, m.initWorkspace()

	case tuicore.WorkspaceInitializedMsg:
		m.host.SetSyncing(false)
		return m, m.host.ActivateView()

	case tuicore.TriggerFetchMsg:
		if !m.isFetching {
			return m, m.checkWorkspaceMode()
		}
		return m, nil

	case tuicore.WorkspaceFetchModeMsg:
		branchCount := len(msg.Branches)
		label2 := "ll upstream"
		if branchCount > 0 {
			label2 = fmt.Sprintf("ll upstream (%d branches)", branchCount)
		}
		m.fetchChoice.Show("Workspace fetch mode?", []tuicore.Choice{
			{Key: "d", Label: "efault + gitmsg"},
			{Key: "a", Label: label2},
		}, func(key string) tea.Cmd {
			m.host.State().ChoicePrompt = ""
			mode := "default"
			if key == "a" {
				mode = "*"
			}
			return m.saveWorkspaceModeAndFetch(mode)
		})
		m.host.State().ChoicePrompt = m.fetchChoice.Render()
		return m, nil

	case startFetchMsg:
		if !m.isFetching {
			m.SetFetching(true)
			repos, lists := 0, 0
			if git.GetOriginURL(m.workdir) != "" {
				repos = 1
			}
			if result := social.GetLists(m.workdir); result.Success {
				lists = len(result.Data)
				for _, l := range result.Data {
					repos += len(l.Repositories)
				}
			}
			m.host.SetFetchingInfo(repos, lists)
			return m, m.startFetchWithMode(msg.allBranches)
		}
		return m, nil

	case tuicore.EditorDoneMsg:
		return m.handleEditorDone(msg)

	case tuicore.EditorErrorMsg:
		m.host.State().AddLogEntry(tuicore.LogSeverityError, msg.Err.Error(), "editor")
		m.nav.SetErrorLogCount(m.host.State().ErrorLogCount())
		return m, m.host.SetMessageWithTimeout("Failed to open editor: "+msg.Err.Error(), tuicore.MessageTypeError, 8*time.Second)

	case tuicore.LogErrorMsg:
		if msg.Message != "" {
			m.host.State().AddLogEntry(msg.Severity, msg.Message, msg.Context)
		}
		m.nav.SetErrorLogCount(m.host.State().ErrorLogCount())
		return m, nil

	case tuicore.OpenEditorMsg:
		mode := tuicore.EditorModePost
		switch msg.Mode {
		case "edit":
			mode = tuicore.EditorModeEdit
		case "comment":
			mode = tuicore.EditorModeComment
		case "repost":
			mode = tuicore.EditorModeRepost
		case "issue":
			mode = tuicore.EditorModeIssue
		}
		return m.openEditorWithContent(mode, msg.TargetID, msg.InitialContent)

	}

	// Handle source navigation (detail view left/right)
	if nav, ok := msg.(tuicore.SourceNavigateMsg); ok {
		// Try type-aware navigation first (for mixed-type sources like search)
		if item, newIndex, found := m.host.GetSourceDisplayItem(nav.Offset); found {
			m.host.UpdateSourceIndex(newIndex, m.host.State().DetailSource.Total)
			loc := tuicore.GetNavTarget(item)
			m.host.State().Router.Replace(loc)
			return m, m.host.ActivateView()
		}
		// Fall back to same-type navigation
		id, newIndex, found := m.host.GetSourceItem(nav.Offset)
		if found {
			m.host.UpdateSourceIndex(newIndex, m.host.State().DetailSource.Total)
			m.host.State().Router.Replace(nav.MakeLocation(id))
			return m, m.host.ActivateView()
		}
		return m, nil
	}

	// Dispatch to message bus for extension handlers
	if handled, cmd := tuicore.DispatchMessage(msg, &m); handled {
		return m, cmd
	}

	// Delegate to focused panel for unhandled messages
	var cmd tea.Cmd
	switch m.focus {
	case FocusNav:
		_, cmd = m.nav.Update(msg)
	case FocusContent:
		cmd = m.host.Update(msg)
	}
	return m, cmd
}

// handleKey processes keyboard input and returns updated model.
func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Dismiss status message on any key press
	if m.host.State().Message != "" {
		m.host.SetMessage("", tuicore.MessageTypeNone)
	}

	// Handle fetch choice dialog
	if m.fetchChoice.IsActive() {
		if handled, cmd := m.fetchChoice.HandleKey(key); handled {
			if !m.fetchChoice.IsActive() {
				m.host.State().ChoicePrompt = ""
			}
			return m, cmd
		}
		return m, nil
	}

	// Content panel handles its own input when active
	if m.host.IsInputActive() {
		cmd := m.host.Update(msg)
		return m, cmd
	}

	// Let focused panel try to handle key first (local bindings override global)
	var cmd tea.Cmd
	switch m.focus {
	case FocusNav:
		_, cmd = m.nav.Update(msg)
	case FocusContent:
		cmd = m.host.Update(msg)
	}
	if cmd != nil {
		return m, cmd
	}

	// Fall back to registry handlers
	ctx := m.host.CurrentContext()
	if binding := m.registry.Resolve(ctx, key); binding != nil {
		handlerCtx := m.buildHandlerContext()
		if binding.Handler != nil {
			if handled, cmd := binding.Handler(handlerCtx); handled {
				return m, cmd
			}
		}
	}

	return m, nil
}

// handleMouse routes mouse events to the appropriate panel based on X coordinate.
func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.MouseMotionMsg); ok {
		return m, nil
	}
	mouse := msg.Mouse()
	if m.layout.ShowNav() && mouse.X < m.layout.NavWidth {
		if _, ok := msg.(tea.MouseClickMsg); ok && m.focus != FocusNav {
			m.setFocus(FocusNav)
		}
		_, cmd := m.nav.UpdateMouse(msg)
		return m, cmd
	}
	if m.host.IsInputActive() {
		return m, nil
	}
	if _, ok := msg.(tea.MouseClickMsg); ok && m.focus != FocusContent {
		m.setFocus(FocusContent)
	}
	return m, m.host.Update(msg)
}

// buildHandlerContext creates a context for keybinding handlers.
func (m *Model) buildHandlerContext() *tuicore.HandlerContext {
	// Get selected display item for type-aware actions
	selectedItem, _ := m.host.GetSelectedDisplayItem()
	selectedID := ""
	if selectedItem != nil {
		selectedID = selectedItem.ItemID()
	}
	return &tuicore.HandlerContext{
		Workdir:      m.workdir,
		CacheDir:     m.cacheDir,
		SelectedID:   selectedID,
		SelectedItem: selectedItem,

		// Callbacks requiring app.go state
		ToggleFocus: func() {
			m.toggleFocus()
		},
		Navigate: func(ctx tuicore.Context) tea.Cmd {
			m.setFocus(FocusContent)
			switch ctx {
			case tuicore.Timeline:
				m.router.Replace(tuicore.LocTimeline)
			case tuicore.Search:
				m.router.Push(tuicore.LocSearch)
			case tuicore.Notifications:
				m.router.Push(tuicore.LocNotifications)
			case tuicore.PMBoard:
				m.router.Push(tuicore.LocPMBoard)
			case tuicore.ReleaseList:
				m.router.Push(tuicore.LocReleaseList)
			case tuicore.ReviewPRs:
				m.router.Push(tuicore.LocReviewPRs)
			case tuicore.Analytics:
				m.router.Push(tuicore.LocAnalytics)
			case tuicore.Help:
				m.router.Push(tuicore.LocHelp)
			case tuicore.ErrorLog:
				m.router.Push(tuicore.LocErrorLog)
			}
			return m.host.ActivateView()
		},
		OpenEditor: func(mode, targetID string) tea.Cmd {
			switch mode {
			case "post":
				_, cmd := m.handleNewPost()
				return cmd
			case "comment":
				_, cmd := m.openEditor(true, targetID)
				return cmd
			case "repost":
				_, cmd := m.openEditorWithMode(tuicore.EditorModeRepost, targetID)
				return cmd
			}
			return nil
		},
		StartFetch: func() tea.Cmd {
			if m.isFetching {
				return nil
			}
			return m.checkWorkspaceMode()
		},
		StartPush: func() tea.Cmd {
			if m.isPushing || m.isFetching {
				return nil
			}
			m.isPushing = true
			m.host.SetPushing(true)
			m.host.SetPushingInfo(git.GetOriginURL(m.workdir))
			return m.startPush()
		},
		StartLFSPush: func() tea.Cmd {
			if m.isPushing || m.isFetching {
				return nil
			}
			m.isPushing = true
			m.host.SetPushing(true)
			m.host.SetPushingInfo(git.GetOriginURL(m.workdir))
			return m.startLFSPush()
		},

		// Direct panel access
		Panel: m.host,
	}
}

// handleNavigate processes navigation messages and updates location.
func (m *Model) handleNavigate(msg tuicore.NavigateMsg) (tea.Model, tea.Cmd) {
	// Restore nav panel if hidden (leaving fullscreen diff)
	if m.navHidden {
		m.navHidden = false
		m.layout = NewLayout(m.layout.Width, m.layout.Height)
		m.nav.SetSize(m.layout.NavWidth, m.layout.Height)
		m.host.SetSize(m.layout.ContentWidth, m.layout.Height)
	}
	// External URLs: open in browser instead of routing internally
	if strings.HasPrefix(msg.Location.Path, "http") {
		openBrowser(msg.Location.Path)
		return m, nil
	}
	// Relative file paths: resolve against workdir and open externally
	if !strings.HasPrefix(msg.Location.Path, "/") && strings.Contains(msg.Location.Path, ".") {
		clean := filepath.Clean(msg.Location.Path)
		if !filepath.IsAbs(clean) && !strings.HasPrefix(clean, "..") {
			resolved := filepath.Join(m.workdir, clean)
			if _, err := os.Stat(resolved); err == nil {
				openBrowser(resolved)
				return m, nil
			}
		}
	}
	// File-like paths that couldn't be resolved: silently ignore instead of routing
	if ext := filepath.Ext(msg.Location.Path); ext != "" {
		return m, nil
	}
	switch msg.Action {
	case tuicore.NavPush:
		m.router.Push(msg.Location)
	case tuicore.NavReplace:
		m.router.Replace(msg.Location)
	case tuicore.NavBack:
		if !m.router.Back() {
			// No history, go to timeline
			m.router.Replace(tuicore.LocTimeline)
		}
	}
	// Store source context when navigating to detail
	if msg.SourcePath != "" {
		m.host.State().DetailSource = &tuicore.SourceContext{
			Path:        msg.SourcePath,
			Index:       msg.SourceIndex,
			Total:       msg.SourceTotal,
			SearchQuery: msg.SearchQuery,
		}
	} else {
		m.host.State().DetailSource = nil
	}
	// Always focus content after navigation
	m.setFocus(FocusContent)
	return m, m.host.ActivateView()
}

// setFocus sets the focused panel and updates panel states.
func (m *Model) setFocus(panel FocusedPanel) {
	m.focus = panel
	m.nav.SetFocused(panel == FocusNav)
	m.host.SetFocused(panel == FocusContent)
}

// toggleFocus switches focus between nav and content panels.
func (m *Model) toggleFocus() {
	if m.focus == FocusNav {
		m.setFocus(FocusContent)
	} else {
		m.setFocus(FocusNav)
	}
}

// Registry returns the keybinding registry.
func (m Model) Registry() *tuicore.Registry {
	return m.registry
}

// handleNewPost opens the editor for creating a new post.
func (m Model) handleNewPost() (tea.Model, tea.Cmd) {
	return m.openEditor(false, "")
}

// startFetchWithMode begins fetching with the specified all-branches mode.
func (m Model) startFetchWithMode(allBranches bool) tea.Cmd {
	return func() tea.Msg {
		if err := cache.Open(m.cacheDir); err != nil {
			log.Warn("failed to open cache before fetch", "error", err)
			return tuisocial.FetchCompletedMsg{Err: fmt.Errorf("cache open: %w", err)}
		}
		extraProcessors := append(pm.Processors(), review.Processors()...)
		extraProcessors = append(extraProcessors, notifications.MentionProcessor(), notifications.TrailerProcessor())
		opts := &social.FetchOptions{
			FetchAllBranches: allBranches,
			ExtraProcessors:  extraProcessors,
		}
		result := social.Fetch(m.workdir, m.cacheDir, opts)
		// Fetch fork PRs (review-only)
		review.FetchForks(m.workdir, m.cacheDir)
		// Re-sync all workspace extension branches to cache
		if err := social.SyncWorkspaceToCache(m.workdir); err != nil {
			log.Debug("post-fetch social sync failed", "error", err)
		}
		if err := pm.SyncWorkspaceToCache(m.workdir); err != nil {
			log.Debug("post-fetch pm sync failed", "error", err)
		}
		if err := review.SyncWorkspaceToCache(m.workdir); err != nil {
			log.Debug("post-fetch review sync failed", "error", err)
		}
		if err := release.SyncWorkspaceToCache(m.workdir); err != nil {
			log.Debug("post-fetch release sync failed", "error", err)
		}
		if !result.Success {
			return tuisocial.FetchCompletedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return tuisocial.FetchCompletedMsg{Stats: result.Data}
	}
}

// startPush begins pushing local changes to the remote repository.
func (m Model) startPush() tea.Cmd {
	return func() tea.Msg {
		result, err := gitmsg.Push(m.workdir, false)
		if err != nil {
			return tuisocial.PushCompletedMsg{Err: err}
		}
		return tuisocial.PushCompletedMsg{Commits: result.Commits, Refs: result.Refs}
	}
}

// startLFSPush begins pushing LFS objects to the remote repository.
func (m Model) startLFSPush() tea.Cmd {
	workdir := m.workdir
	return func() tea.Msg {
		count, err := git.PushLFS(workdir)
		return tuicore.LFSPushCompletedMsg{Count: count, Err: err}
	}
}

// checkWorkspaceMode checks if workspace mode is configured and either starts fetch or shows choice.
func (m Model) checkWorkspaceMode() tea.Cmd {
	workdir := m.workdir
	return func() tea.Msg {
		originURL := protocol.NormalizeURL(git.GetOriginURL(workdir))
		if originURL == "" {
			return m.triggerFetchDirectly(false)
		}
		settingsPath, _ := settings.DefaultPath()
		s, _ := settings.Load(settingsPath)
		mode := settings.GetWorkspaceMode(s, originURL)
		if mode != "" {
			return m.triggerFetchDirectly(mode == "*")
		}
		branches, _ := git.ListRemoteBranches(workdir, "origin")
		return tuicore.WorkspaceFetchModeMsg{Branches: branches}
	}
}

// triggerFetchDirectly is a helper that returns a startFetchMsg to begin fetching.
func (m Model) triggerFetchDirectly(allBranches bool) tea.Msg {
	return startFetchMsg{allBranches: allBranches}
}

// fetchStatusMsg delivers the async-loaded fetch status.
type fetchStatusMsg struct {
	lastFetch time.Time
}

// startSyncMsg triggers workspace sync after the first render.
type startSyncMsg struct{}

// startFetchMsg is an internal message to trigger fetch with a resolved mode.
type startFetchMsg struct {
	allBranches bool
}

// saveWorkspaceModeAndFetch saves the mode and starts fetching.
func (m Model) saveWorkspaceModeAndFetch(mode string) tea.Cmd {
	workdir := m.workdir
	return func() tea.Msg {
		originURL := protocol.NormalizeURL(git.GetOriginURL(workdir))
		if originURL != "" {
			settingsPath, _ := settings.DefaultPath()
			s, _ := settings.Load(settingsPath)
			settings.SetWorkspaceMode(s, originURL, mode)
			if err := settings.Save(settingsPath, s); err != nil {
				log.Warn("failed to save workspace fetch mode", "error", err)
			}
		}
		return startFetchMsg{allBranches: mode == "*"}
	}
}

// refreshTimeline reloads timeline posts from the database.
func (m Model) refreshTimeline() tea.Cmd {
	workdir := m.workdir
	return func() tea.Msg {
		result := social.GetPosts(workdir, "timeline", &social.GetPostsOptions{Limit: tuicore.PageSize + 1})
		if !result.Success {
			return tuisocial.TimelineLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		posts, hasMore := tuicore.TrimPage(result.Data, tuicore.PageSize)
		total := social.CountTimeline(workdir)
		return tuisocial.TimelineLoadedMsg{Posts: posts, HasMore: hasMore, Total: total}
	}
}

// fetchAddedRepo fetches a repository that was just added to a list (full history).
func (m Model) fetchAddedRepo(repoRef string) tea.Cmd {
	cacheDir := m.cacheDir
	workspaceURL := gitmsg.ResolveRepoURL(m.workdir)
	return func() tea.Msg {
		if err := cache.Open(cacheDir); err != nil {
			log.Warn("failed to open cache before repo fetch", "error", err)
			return tuisocial.RepoFetchedAfterAddMsg{RepoURL: repoRef, Err: fmt.Errorf("cache open: %w", err)}
		}
		// Parse repo URL to extract base URL and branch
		id := protocol.ParseRepositoryID(repoRef)
		// Fetch complete history (regardless of what's already cached)
		result := social.FetchRepository(cacheDir, id.Repository, id.Branch, workspaceURL)
		if !result.Success {
			return tuisocial.RepoFetchedAfterAddMsg{RepoURL: repoRef, Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return tuisocial.RepoFetchedAfterAddMsg{RepoURL: repoRef, Posts: result.Data.Posts}
	}
}

// openEditor opens the external editor for post or comment creation.
func (m Model) openEditor(isComment bool, targetID string) (tea.Model, tea.Cmd) {
	mode := tuicore.EditorModePost
	if isComment {
		mode = tuicore.EditorModeComment
	}
	return m.openEditorWithMode(mode, targetID)
}

// openEditorWithMode opens the editor with a specific mode.
func (m Model) openEditorWithMode(mode tuicore.EditorMode, targetID string) (tea.Model, tea.Cmd) {
	return m.openEditorWithContent(mode, targetID, "")
}

// openEditorWithContent opens the editor with initial content.
func (m Model) openEditorWithContent(mode tuicore.EditorMode, targetID, initialContent string) (tea.Model, tea.Cmd) {
	f, err := os.CreateTemp("", "gitmsg-*.md")
	if err != nil {
		return m, nil
	}
	tmpFile := f.Name()
	f.Close()
	if err := os.WriteFile(tmpFile, []byte(initialContent), 0600); err != nil {
		return m, nil
	}
	editor := getEditor()
	c := exec.Command(editor, tmpFile)
	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.Remove(tmpFile)
		if err != nil {
			return tuicore.EditorErrorMsg{Err: err}
		}
		content, readErr := os.ReadFile(tmpFile)
		if readErr != nil {
			return tuicore.EditorErrorMsg{Err: readErr}
		}
		return tuicore.EditorDoneMsg{
			Content:  strings.TrimSpace(string(content)),
			Mode:     mode,
			TargetID: targetID,
		}
	})
}

// handleEditorDone processes content returned from the external editor.
func (m Model) handleEditorDone(msg tuicore.EditorDoneMsg) (tea.Model, tea.Cmd) {
	targetID := msg.TargetID
	switch msg.Mode {
	case tuicore.EditorModeComment:
		if msg.Content == "" {
			return m, nil
		}
		result := social.CreateComment(m.workdir, targetID, msg.Content, nil)
		if !result.Success {
			return m, nil
		}
		return m, func() tea.Msg { return tuisocial.CommentCreatedMsg{Post: result.Data} }
	case tuicore.EditorModeRepost:
		if msg.Content == "" {
			result := social.CreateRepost(m.workdir, targetID)
			if !result.Success {
				return m, nil
			}
			return m, func() tea.Msg { return tuisocial.RepostCreatedMsg{Post: result.Data} }
		}
		result := social.CreateQuote(m.workdir, targetID, msg.Content)
		if !result.Success {
			return m, nil
		}
		return m, func() tea.Msg { return tuisocial.RepostCreatedMsg{Post: result.Data} }
	case tuicore.EditorModeEdit:
		if msg.Content == "" {
			return m, nil
		}
		m.host.SetSaving(true)
		workdir := m.workdir
		content := msg.Content
		return m, func() tea.Msg {
			result := social.EditPost(workdir, targetID, content)
			if !result.Success {
				return tuisocial.PostEditedMsg{Post: social.Post{}, Err: fmt.Errorf("%s", result.Error.Message)}
			}
			return tuisocial.PostEditedMsg{Post: result.Data}
		}
	case tuicore.EditorModeIssue:
		if msg.Content == "" {
			return m, nil
		}
		lines := strings.SplitN(msg.Content, "\n", 2)
		subject := strings.TrimSpace(lines[0])
		var body string
		if len(lines) > 1 {
			body = strings.TrimSpace(lines[1])
		}
		if subject == "" {
			return m, nil
		}
		workdir := m.workdir
		return m, func() tea.Msg {
			result := pm.CreateIssue(workdir, subject, body, pm.CreateIssueOptions{})
			if !result.Success {
				return tuipm.IssueCreatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
			}
			return tuipm.IssueCreatedMsg{Issue: result.Data}
		}
	default:
		if msg.Content == "" {
			return m, nil
		}
		result := social.CreatePost(m.workdir, msg.Content, nil)
		if !result.Success {
			return m, nil
		}
		return m, func() tea.Msg { return tuisocial.PostCreatedMsg{Post: result.Data} }
	}
}

// getEditor returns the user's preferred editor from environment.
func getEditor() string {
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	return "vim"
}

// View renders the TUI as a tea.View with alt screen and mouse mode.
func (m Model) View() tea.View {
	var output string
	if !m.ready {
		output = "Loading..."
	} else {
		hostView := m.host.View()
		navView := ""
		if m.layout.ShowNav() {
			navView = m.nav.View()
		}
		if navView == m.vc.navView && hostView == m.vc.hostView && m.vc.output != "" {
			output = m.vc.output
		} else {
			if navView != "" {
				output = lipgloss.JoinHorizontal(lipgloss.Top, navView, hostView)
			} else {
				output = hostView
			}
			output = zone.Scan(output)
			m.vc.navView = navView
			m.vc.hostView = hostView
			m.vc.output = output
		}
	}
	view := tea.NewView(output)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	if !m.headless {
		view.WindowTitle = "※ GitSocial · " + filepath.Base(m.workdir)
	}
	return view
}

// openBrowser opens a URL in the user's default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Warn("failed to open browser", "url", url, "error", err)
	}
}

// Run starts the TUI application.
func Run(workdir, cacheDir string) error {
	initTUILogging(cacheDir)
	zone.NewGlobal()
	defer zone.Close()

	// Terminal.app renders East Asian Ambiguous-width Unicode characters as
	// double-width, mismatching go-runewidth calculations and causing line
	// wrapping that breaks the two-panel layout.
	tuicore.NeedsWidthMargin = os.Getenv("TERM_PROGRAM") == "Apple_Terminal"

	p := tea.NewProgram(
		NewModel(workdir, cacheDir),
		tea.WithFPS(120),
	)
	_, err := p.Run()
	return err
}

// initTUILogging initializes logging for the TUI session.
func initTUILogging(cacheDir string) {
	settingsPath, _ := settings.DefaultPath()
	s, _ := settings.Load(settingsPath)

	level := log.LevelInfo
	if s != nil {
		if lvl, ok := settings.Get(s, "log.level"); ok {
			level = log.ParseLevel(lvl)
		}
	}

	logPath := filepath.Join(cacheDir, "gitsocial.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		log.Init(log.Config{Level: level, Mode: log.ModeSilent})
		return
	}

	log.Init(log.Config{
		Level:  level,
		Mode:   log.ModeJSON,
		Output: logFile,
	})
}
