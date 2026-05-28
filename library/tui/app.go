// app.go - Main TUI application model, initialization, and event loop
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/identity"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
	"github.com/gitsocial-org/gitsocial/library/core/storage"
	"github.com/gitsocial-org/gitsocial/library/extensions/memo"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/extensions/release"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	importpkg "github.com/gitsocial-org/gitsocial/library/import"
	ghimport "github.com/gitsocial-org/gitsocial/library/import/github"
	glimport "github.com/gitsocial-org/gitsocial/library/import/gitlab"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/library/tui/tuimemo"
	"github.com/gitsocial-org/gitsocial/library/tui/tuipm"
	"github.com/gitsocial-org/gitsocial/library/tui/tuirelease"
	"github.com/gitsocial-org/gitsocial/library/tui/tuireview"
	"github.com/gitsocial-org/gitsocial/library/tui/tuisocial"
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
	isFetching  bool
	isPushing   bool
	isImporting bool

	// Workspace fetch mode choice dialog
	fetchChoice tuicore.ChoiceDialog

	// Import confirmation choice dialog
	importChoice tuicore.ChoiceDialog

	// bgImportCh streams ImportProgressMsg + ImportCompletedMsg from the
	// background import goroutine. Created when an import starts, closed when
	// it ends.
	bgImportCh chan tea.Msg

	// Nav panel hidden (fullscreen diff mode)
	navHidden bool

	// Headless mode skips terminal-dependent commands (SetWindowTitle)
	headless bool

	// View cache — avoids JoinHorizontal + zone.Scan when nothing changed
	vc *viewCache

	// bgSyncCh streams progress + completion messages from the background
	// workspace history sync started by initWorkspace. Drained by drainBgSyncCmd.
	bgSyncCh chan tea.Msg
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

// CurrentView returns the view at the router's current path. Test accessor.
func (m *Model) CurrentView() tuicore.View { return m.host.CurrentView() }

// Nav returns the nav context for message handlers.
func (m *Model) Nav() tuicore.NavContext { return m.nav }

// LoadLists returns a command to reload lists.
func (m *Model) LoadLists() tea.Cmd { return m.loadInitialLists() }

// LoadUnreadCount returns a command to reload unread count.
func (m *Model) LoadUnreadCount() tea.Cmd { return m.loadInitialUnreadCount() }

// LoadUnpushedCount returns a command to reload unpushed count.
func (m *Model) LoadUnpushedCount() tea.Cmd { return m.loadInitialUnpushedCount() }

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
	memo.RegisterNavItems(navRegistry)

	// Initialize router with timeline as default
	r := tuicore.NewRouter(tuicore.LocTimeline)

	// Initialize keybinding registry
	registry := tuicore.NewRegistry()

	// Pre-compute git root once to avoid repeated subprocess calls
	gitRoot, _ := git.GetRootDir(workdir)
	if gitRoot == "" {
		gitRoot = workdir
	}

	// Initialize shared state
	state := &tuicore.State{
		Workdir:     workdir,
		CacheDir:    cacheDir,
		GitRoot:     gitRoot,
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
	if !userSettings.Extensions.Memo {
		navRegistry.SetHidden("memo", true)
	}

	// Apply display settings
	state.ShowEmailOnCards = userSettings.Display.ShowEmail
	identity.SetDNSVerificationEnabled(userSettings.Identity.DNSVerification)

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

	forksView := tuicore.NewForksView(workdir)
	host.AddView("/config/forks", forksView)

	identityView := tuicore.NewIdentityView(workdir)
	host.AddView("/config/identity", identityView)

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

	// Register Memo views
	tuimemo.Register(host)

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
		bgSyncCh: make(chan tea.Msg, 32),
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
	m.host.State().LastInputAt = time.Now()
	return tea.Batch(
		m.host.ActivateView(),
		func() tea.Msg { return startSyncMsg{} },
		m.loadInitialCacheSize(),
		m.loadInitialUnreadCount(),
		m.loadInitialLists(),
		scheduleAutoFetch(autoFetchIdlePoll),
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

// loadUnpushedLFSCount runs `git lfs push --dry-run` against origin (network)
// and emits an UnpushedLFSCountMsg. Triggered after a successful LFS push to
// refresh the nav badge — not at startup, because the dry-run hits the network.
func (m Model) loadUnpushedLFSCount() tea.Cmd {
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

// bgSyncProgressMsg is emitted from the background history sync goroutine
// after each chunk of older commits is processed.
type bgSyncProgressMsg struct {
	Processed int
	Total     int
}

// bgSyncCompletedMsg signals the background history sync has finished.
type bgSyncCompletedMsg struct{}

// initWorkspace syncs the workspace to cache at startup. The quick pass
// (most-recent quickPassLimit commits + extension refs) runs synchronously so
// the UI has data to render. The remaining history is processed in a
// background goroutine, followed by identity backfill (signer-key extraction
// + binding verification — slow on a fresh cache because of forge HTTPS).
// All bg work streams progress + completion via m.bgSyncCh.
func (m Model) initWorkspace() tea.Cmd {
	workdir := m.workdir
	ch := m.bgSyncCh
	return func() tea.Msg {
		if err := fetch.SyncWorkspaceQuick(workdir); err != nil {
			log.Debug("workspace quick sync failed", "error", err)
		}
		go func() {
			defer close(ch)
			err := fetch.SyncWorkspaceContinue(workdir, func(p fetch.SyncProgress) {
				select {
				case ch <- bgSyncProgressMsg{Processed: p.Processed, Total: p.Total}:
				default:
					// Channel full — drop progress update; final completion still arrives.
				}
			})
			if err != nil {
				log.Debug("workspace background sync failed", "error", err)
			}
			fetch.BackfillWorkspaceIdentity(workdir)
			ch <- bgSyncCompletedMsg{}
		}()
		return tuicore.WorkspaceInitializedMsg{}
	}
}

// drainBgSyncCmd reads one message from the background sync channel and
// returns it. Re-dispatched by Update after each bg message until the
// channel is closed.
func drainBgSyncCmd(ch chan tea.Msg) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
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
		m.host.State().LastInputAt = time.Now()
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.handleKey(msg)

	case tea.KeyReleaseMsg:
		return m, nil

	case tea.MouseReleaseMsg:
		return m, nil

	case tea.MouseMsg:
		m.host.State().LastInputAt = time.Now()
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
		return m, tea.Batch(msgCmd, m.loadUnpushedLFSCount())

	case lfsCheckCompletedMsg:
		m.nav.SetUnpushedLFSCount(msg.count)
		var toast string
		if msg.count == 0 {
			toast = "No unpushed LFS objects"
		} else {
			toast = fmt.Sprintf("%d LFS objects unpushed", msg.count)
		}
		return m, m.host.SetMessageWithTimeout(toast, tuicore.MessageTypeSuccess, 5*time.Second)

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
		// Quick pass is done; the timeline can render. Background goroutine
		// continues with older history + identity verification — flag that so
		// the footer shows a subtle indicator instead of going silent.
		m.host.State().BackgroundSyncing = true
		return m, tea.Batch(
			m.host.RefreshView(),
			m.loadInitialStatus(),
			m.loadInitialUnpushedCount(),
			drainBgSyncCmd(m.bgSyncCh),
		)

	case bgSyncProgressMsg:
		// Each progress message means a chunk of older commits just landed.
		// Bump IdentityGeneration so card lists re-query against the larger
		// cache, then keep draining.
		m.host.State().IdentityGeneration++
		return m, tea.Batch(m.host.RefreshView(), drainBgSyncCmd(m.bgSyncCh))

	case bgSyncCompletedMsg:
		// Background history sync + identity backfill done. Final refresh so
		// verification badges populate; clear the BackgroundSyncing footer flag.
		m.host.State().IdentityGeneration++
		m.host.State().BackgroundSyncing = false
		return m, m.host.RefreshView()

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
			return m, m.startFetchWithMode(msg.allBranches, msg.auto)
		}
		return m, nil

	case autoFetchTickMsg:
		return m, m.handleAutoFetchTick()

	case tuicore.LogErrorMsg:
		if msg.Message != "" {
			m.host.State().AddLogEntry(msg.Severity, msg.Message, msg.Context)
		}
		m.nav.SetErrorLogCount(m.host.State().ErrorLogCount())
		return m, nil

	case importCountedMsg:
		// Count phase finished — drop counting state, then either error,
		// short-circuit on "nothing to import", or open the confirm dialog
		// with concrete numbers.
		m.isImporting = false
		m.host.SetImporting(false)
		m.bgImportCh = nil
		if msg.Err != nil {
			return m, m.host.SetMessageWithTimeout("Import: "+msg.Err.Error(), tuicore.MessageTypeError, 10*time.Second)
		}
		pending := pendingImportCounts(msg.Counts, msg.Mapped)
		if pending == 0 {
			return m, m.host.SetMessageWithTimeout("Already up to date — nothing new to import", tuicore.MessageTypeSuccess, 5*time.Second)
		}
		prompt := buildImportConfirmPrompt(msg.RepoURL, msg.Counts, msg.Mapped)
		ad, url, counts, mapping := msg.Adapter, msg.RepoURL, msg.Counts, msg.Mapping
		host := m.host
		m.importChoice.Show(prompt, []tuicore.Choice{
			{Key: "y", Label: "es"},
			{Key: "n", Label: "o"},
		}, func(key string) tea.Cmd {
			host.State().ChoicePrompt = ""
			if key != "y" {
				return nil
			}
			return func() tea.Msg {
				return startImportMsg{Adapter: ad, RepoURL: url, Counts: &counts, Mapping: mapping}
			}
		})
		m.host.State().ChoicePrompt = m.importChoice.Render()
		return m, nil

	case startImportMsg:
		if m.isImporting || m.isFetching {
			return m, nil
		}
		return m, m.startImport(msg.Adapter, msg.RepoURL, msg.Counts, msg.Mapping)

	case importTickMsg:
		// Spinner tick — the glyph itself derives from time.Now() in
		// RenderImportingFooter, so the tick exists only to trigger a
		// re-render. Keep draining; once the goroutine closes the channel,
		// drainBgImportCmd returns nil and the loop ends.
		if !m.isImporting {
			return m, nil
		}
		return m, drainBgImportCmd(m.bgImportCh)

	case importProgressMsg:
		phase, detail := formatImportProgress(msg.Event)
		m.host.SetImportProgress(phase, detail)
		return m, drainBgImportCmd(m.bgImportCh)

	case importCompletedMsg:
		m.isImporting = false
		m.host.SetImporting(false)
		m.bgImportCh = nil
		if msg.Err != nil {
			errMsg := fmt.Sprintf("Import failed: %s", msg.Err)
			m.host.State().AddLogEntry(tuicore.LogSeverityError, errMsg, "import")
			m.nav.SetErrorLogCount(m.host.State().ErrorLogCount())
			return m, m.host.SetMessageWithTimeout(errMsg, tuicore.MessageTypeError, 10*time.Second)
		}
		for _, e := range msg.Stats.Errors {
			label := e.Type
			if e.ExternalID != "" {
				label += " " + e.ExternalID
			}
			m.host.State().AddLogEntry(tuicore.LogSeverityWarn, label+": "+e.Message, "import")
		}
		m.nav.SetErrorLogCount(m.host.State().ErrorLogCount())
		// Refresh counts + views since import wrote new commits.
		m.host.State().IdentityGeneration++
		summary := formatImportSummary(msg.Stats)
		msgType := tuicore.MessageTypeSuccess
		if len(msg.Stats.Errors) > 0 {
			summary += fmt.Sprintf(" (%d errors)", len(msg.Stats.Errors))
			msgType = tuicore.MessageTypeWarning
		}
		return m, tea.Batch(
			m.host.RefreshView(),
			m.loadInitialUnreadCount(),
			m.loadInitialUnpushedCount(),
			m.host.SetMessageWithTimeout(summary, msgType, 10*time.Second),
		)

	case tuicore.ExportArtifactMsg:
		return m, m.exportArtifact(msg)

	case exportArtifactDoneMsg:
		if msg.err != nil {
			return m, m.host.SetMessageWithTimeout(msg.err.Error(), tuicore.MessageTypeError, 5*time.Second)
		}
		openBrowser(filepath.Dir(msg.path))
		return m, m.host.SetMessageWithTimeout("Saved to "+msg.path, tuicore.MessageTypeSuccess, 5*time.Second)

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

	// Delegate to focused panel for unhandled messages.
	// Non-key/mouse messages (async load results, etc.) always go to the host
	// so right-panel views update during nav-cursor preview.
	var cmd tea.Cmd
	switch msg.(type) {
	case tea.KeyPressMsg, tea.MouseMsg:
		switch m.focus {
		case FocusNav:
			_, cmd = m.nav.Update(msg)
		case FocusContent:
			cmd = m.host.Update(msg)
		}
	default:
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

	// Handle import choice dialog
	if m.importChoice.IsActive() {
		if handled, cmd := m.importChoice.HandleKey(key); handled {
			if !m.importChoice.IsActive() {
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

	// Fall back to registry handlers. Resolve returns all matching bindings
	// (e.g. a view's label-only entry plus a global handler); try each until
	// one reports it handled the key so label-only noops don't shadow globals.
	ctx := m.host.CurrentContext()
	bindings := m.registry.Resolve(ctx, key)
	if len(bindings) > 0 {
		handlerCtx := m.buildHandlerContext()
		for _, binding := range bindings {
			if binding.Handler == nil {
				continue
			}
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
			case tuicore.MemoList:
				m.router.Push(tuicore.LocMemoProject)
			case tuicore.Analytics:
				m.router.Push(tuicore.LocAnalytics)
			case tuicore.Help:
				m.router.Push(tuicore.LocHelp)
			case tuicore.ErrorLog:
				m.router.Push(tuicore.LocErrorLog)
			}
			return m.host.ActivateView()
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
		CheckLFS: func() tea.Cmd {
			return m.checkLFSCount()
		},
		StartImport: func() tea.Cmd {
			if m.isImporting || m.isFetching {
				return nil
			}
			return m.beginImport()
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
	// Artifact export: download to ~/Downloads and open
	if msg.Location.Path == "/export-artifact" {
		return m, m.exportArtifact(tuicore.ExportArtifactMsg{
			RepoURL:  msg.Location.Param("repoURL"),
			Version:  msg.Location.Param("version"),
			Filename: msg.Location.Param("filename"),
		})
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
	} else if msg.Action != tuicore.NavBack {
		m.host.State().DetailSource = nil
	}
	// Focus content after navigation, unless caller asked to keep focus (nav preview)
	if !msg.KeepFocus {
		m.setFocus(FocusContent)
	}
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

// startFetchWithMode begins fetching with the specified all-branches mode.
func (m Model) startFetchWithMode(allBranches, auto bool) tea.Cmd {
	return func() tea.Msg {
		if err := cache.Open(m.cacheDir); err != nil {
			log.Warn("failed to open cache before fetch", "error", err)
			return tuisocial.FetchCompletedMsg{Err: fmt.Errorf("cache open: %w", err), Auto: auto}
		}
		extraProcessors := append(pm.Processors(), review.Processors()...)
		extraProcessors = append(extraProcessors, notifications.MentionProcessor(), notifications.TrailerProcessor())
		opts := &social.FetchOptions{
			FetchAllBranches: allBranches,
			ExtraProcessors:  extraProcessors,
		}
		result := social.Fetch(m.workdir, m.cacheDir, opts)
		// Fetch all gitmsg data from registered forks
		forkProcessors := append(review.Processors(), pm.Processors()...)
		forkProcessors = append(forkProcessors, notifications.MentionProcessor(), notifications.TrailerProcessor())
		fetch.FetchForks(m.workdir, m.cacheDir, forkProcessors)
		// Re-sync all workspace extension branches to cache
		if err := fetch.SyncWorkspace(m.workdir); err != nil {
			log.Debug("post-fetch workspace sync failed", "error", err)
		}
		if !result.Success {
			return tuisocial.FetchCompletedMsg{Err: fmt.Errorf("%s", result.Error.Message), Auto: auto}
		}
		return tuisocial.FetchCompletedMsg{Stats: result.Data, Auto: auto}
	}
}

// startPush runs gitmsg.Push, which auto-merges divergent gitmsg/* branches
// (empty-tree append-only → conflict-free) instead of failing non-fast-forward.
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

// checkLFSCount counts unpushed LFS objects against origin. Hits the network
// (git lfs push --dry-run), so it's user-triggered, not run at startup.
func (m Model) checkLFSCount() tea.Cmd {
	workdir := m.workdir
	return func() tea.Msg {
		return lfsCheckCompletedMsg{count: git.GetUnpushedLFSCount(workdir)}
	}
}

// lfsCheckCompletedMsg carries the result of a manual LFS unpushed-count check.
type lfsCheckCompletedMsg struct{ count int }

// checkWorkspaceMode checks if workspace mode is configured and either starts fetch or shows choice.
func (m Model) checkWorkspaceMode() tea.Cmd {
	workdir := m.workdir
	return func() tea.Msg {
		originURL := protocol.NormalizeURL(git.GetOriginURL(workdir))
		if originURL == "" {
			return m.triggerFetchDirectly(false)
		}
		mode := settings.GetWorkspaceMode(originURL)
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

// exportArtifactDoneMsg is sent when artifact export completes.
type exportArtifactDoneMsg struct {
	path string
	err  error
}

// exportArtifact exports a release artifact to the downloads directory.
func (m Model) exportArtifact(msg tuicore.ExportArtifactMsg) tea.Cmd {
	workdir := m.workdir
	cacheDir := m.cacheDir
	return func() tea.Msg {
		repoDir := workdir
		repoURL := msg.RepoURL
		if repoURL == "" {
			repoURL = gitmsg.ResolveRepoURL(workdir)
		}
		wsURL := gitmsg.ResolveRepoURL(workdir)
		if repoURL != wsURL {
			repoDir = storage.GetStorageDir(cacheDir, repoURL)
		}
		destDir := git.DownloadsDir()
		destPath := filepath.Join(destDir, msg.Filename)
		res := release.ExportArtifact(repoDir, repoURL, msg.Version, msg.Filename, destPath)
		if !res.Success {
			return exportArtifactDoneMsg{err: fmt.Errorf("%s", res.Error.Message)}
		}
		return exportArtifactDoneMsg{path: res.Data}
	}
}

// fetchStatusMsg delivers the async-loaded fetch status.
type fetchStatusMsg struct {
	lastFetch time.Time
}

// startSyncMsg triggers workspace sync after the first render.
type startSyncMsg struct{}

// startFetchMsg is an internal message to trigger fetch with a resolved mode.
// auto marks a fetch started by the periodic auto-fetch timer (vs. a manual
// trigger) so completion handling can soften toasts and drive back-off.
type startFetchMsg struct {
	allBranches bool
	auto        bool
}

// saveWorkspaceModeAndFetch saves the mode and starts fetching.
func (m Model) saveWorkspaceModeAndFetch(mode string) tea.Cmd {
	workdir := m.workdir
	return func() tea.Msg {
		originURL := protocol.NormalizeURL(git.GetOriginURL(workdir))
		if originURL != "" {
			if err := settings.WriteWorkspaceMode(originURL, mode); err != nil {
				log.Warn("failed to save workspace fetch mode", "error", err)
			}
		}
		return startFetchMsg{allBranches: mode == "*"}
	}
}

// --- Periodic auto-fetch (opt-in via fetch.auto.* settings) ---

// autoFetchTickMsg fires on the periodic auto-fetch heartbeat. Each tick re-arms
// the next one, so the cadence is decided at fire time from current settings
// rather than baked into a fixed ticker — the user can toggle it live.
type autoFetchTickMsg struct{}

const (
	// autoFetchIdlePoll is the re-arm delay when auto-fetch is disabled or
	// momentarily blocked: cheap to repeat, yet short enough that enabling the
	// feature takes effect promptly.
	autoFetchIdlePoll = 30 * time.Second
	// autoFetchCeiling caps the exponential back-off interval.
	autoFetchCeiling = 30 * time.Minute
	// autoFetchIdleAfter pauses the heartbeat when there's been no keypress or
	// mouse input for this long, so an unattended TUI stops polling. Resumes on
	// the next input event.
	autoFetchIdleAfter = 1 * time.Hour
)

// scheduleAutoFetch arms a single auto-fetch tick after d. The tick handler
// re-arms it, making the timer a self-perpetuating heartbeat for the session.
func scheduleAutoFetch(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return autoFetchTickMsg{} })
}

// backoffInterval returns the effective delay for an idle streak: the base
// interval doubled per consecutive empty fetch, capped at autoFetchCeiling. A
// loop (not a shift) keeps it overflow-safe for any streak.
func backoffInterval(baseSeconds, streak int) time.Duration {
	d := time.Duration(baseSeconds) * time.Second
	if d <= 0 {
		return autoFetchCeiling
	}
	for i := 0; i < streak && d < autoFetchCeiling; i++ {
		d *= 2
	}
	if d > autoFetchCeiling {
		d = autoFetchCeiling
	}
	return d
}

// autoFetchCmd starts a background fetch from the auto-fetch timer. Unlike the
// manual path it never opens the workspace-mode dialog: an unset mode falls back
// to default (current-branch) fetching, never all-branches.
func (m Model) autoFetchCmd() tea.Cmd {
	workdir := m.workdir
	return func() tea.Msg {
		allBranches := false
		if originURL := protocol.NormalizeURL(git.GetOriginURL(workdir)); originURL != "" {
			allBranches = settings.GetWorkspaceMode(originURL) == "*"
		}
		return startFetchMsg{allBranches: allBranches, auto: true}
	}
}

// handleAutoFetchTick decides what the periodic heartbeat does this cycle and
// always re-arms the next tick. Settings are read each fire so toggling the
// feature or its interval takes effect without restarting the TUI.
func (m Model) handleAutoFetchTick() tea.Cmd {
	s, _ := settings.Load("")
	if !s.Fetch.AutoEnabled {
		return scheduleAutoFetch(autoFetchIdlePoll)
	}
	// Pause when the workstation looks unattended (no keypress/mouse for
	// autoFetchIdleAfter). LastInputAt refreshes in Update's KeyPressMsg /
	// MouseMsg cases, so the heartbeat resumes on the next input. A zero value
	// is treated as active so a freshly opened session polls before any input.
	if li := m.host.State().LastInputAt; !li.IsZero() && time.Since(li) > autoFetchIdleAfter {
		return scheduleAutoFetch(autoFetchIdlePoll)
	}
	// Never start an auto-fetch on top of another operation, while the user is
	// mid-input, or while a dialog is open — re-check shortly instead.
	if m.isFetching || m.isPushing || m.isImporting ||
		m.host.IsInputActive() || m.host.State().ChoicePrompt != "" {
		return scheduleAutoFetch(autoFetchIdlePoll)
	}
	eff := time.Duration(s.Fetch.AutoInterval) * time.Second
	if s.Fetch.AutoBackoff {
		eff = backoffInterval(s.Fetch.AutoInterval, m.host.State().AutoFetchIdleStreak)
	}
	if remaining := eff - time.Since(m.host.State().LastFetchTime); remaining > 0 {
		return scheduleAutoFetch(remaining)
	}
	return tea.Batch(m.autoFetchCmd(), scheduleAutoFetch(eff))
}

// refreshTimeline reloads timeline posts from the database. The total count
// is dispatched as a separate message so the page render isn't blocked on
// COUNT(*).
func (m Model) refreshTimeline() tea.Cmd {
	workdir := m.workdir
	gitRoot := m.host.State().GitRoot
	pageCmd := func() tea.Msg {
		result := social.GetPosts(workdir, "timeline", &social.GetPostsOptions{Limit: tuicore.PageSize + 1, GitRoot: gitRoot})
		if !result.Success {
			return tuisocial.TimelineLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		posts, hasMore := tuicore.TrimPage(result.Data, tuicore.PageSize)
		return tuisocial.TimelineLoadedMsg{Posts: posts, HasMore: hasMore}
	}
	countCmd := func() tea.Msg {
		return tuisocial.TimelineCountLoadedMsg{Total: social.CountTimeline(workdir, gitRoot)}
	}
	return tea.Batch(pageCmd, countCmd)
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

// importTickInterval drives the spinner glyph next to the import footer
// header. 150ms is fast enough to look animated, slow enough to not waste
// renders.
const importTickInterval = 150 * time.Millisecond

// importTickMsg fires periodically while an import is running so the footer
// repaints its spinner glyph even when no progress events arrive (large
// fetches only emit OnFetchProgress once they finish). Pumped by a dedicated
// goroutine into the same channel as progress/completion messages — that way
// the tick rides on the existing drain loop and stops automatically when the
// channel closes.
type importTickMsg struct{}

// startImportMsg is dispatched from the import-confirm dialog's onChoice
// callback. It carries everything Update needs to launch the import on the
// CURRENT Model — the dialog's onChoice lambda captures a stale `m` from the
// Update iteration that opened the dialog, so any state mutation it does
// directly would land on a dead Model copy and never reach the runtime's
// active model. Counts + Mapping are pre-computed during the count phase so
// importpkg.Run can skip recounting and re-reading the mapping file.
type startImportMsg struct {
	Adapter importpkg.SourceAdapter
	RepoURL string
	Counts  *importpkg.ItemCounts
	Mapping *importpkg.MappingFile
}

// importCountedMsg fires when the pre-import count goroutine finishes. Carries
// what would be imported so Update can build a concrete confirm prompt and
// pass the same counts/mapping into startImport without redoing the work.
type importCountedMsg struct {
	Adapter importpkg.SourceAdapter
	RepoURL string
	Counts  importpkg.ItemCounts
	Mapped  importpkg.ItemCounts
	Mapping *importpkg.MappingFile
	Err     error
}

// importProgressMsg streams progress events from the background import goroutine.
type importProgressMsg struct {
	Event importpkg.ProgressEvent
}

// importCompletedMsg signals the background import goroutine has finished.
type importCompletedMsg struct {
	Stats   importpkg.Stats
	RepoURL string
	Err     error
}

// importExtensionLabels maps extension names to footer-friendly phase labels.
var importExtensionLabels = map[string]string{
	"pm":      "PM",
	"release": "releases",
	"review":  "PRs",
	"social":  "discussions",
}

// beginImport resolves the origin remote, builds an adapter, and kicks off
// the count phase so the user can see exactly how many items would be
// imported before confirming. Counting runs in a goroutine and reports via
// importCountedMsg; while it's running the same Importing footer + spinner
// the actual import uses is shown with phase="counting".
func (m *Model) beginImport() tea.Cmd {
	rawURL := git.GetOriginURL(m.workdir)
	if rawURL == "" {
		return m.host.SetMessageWithTimeout("Import needs an origin remote — none found", tuicore.MessageTypeError, 5*time.Second)
	}
	if !strings.Contains(rawURL, "://") && !strings.HasPrefix(rawURL, "git@") {
		rawURL = "https://" + rawURL
	}
	repoURL := protocol.NormalizeURL(rawURL)
	repoInfo := protocol.ParseRepo(repoURL)
	if repoInfo == nil {
		return m.host.SetMessageWithTimeout("Import: could not parse owner/repo from "+rawURL, tuicore.MessageTypeError, 5*time.Second)
	}
	hostType, err := importpkg.ResolveHost(repoURL, "")
	if err != nil {
		return m.host.SetMessageWithTimeout("Import: "+err.Error(), tuicore.MessageTypeError, 5*time.Second)
	}
	adapter, err := createImportAdapter(hostType, repoInfo.Owner, repoInfo.Repo)
	if err != nil {
		return m.host.SetMessageWithTimeout("Import: "+err.Error(), tuicore.MessageTypeError, 5*time.Second)
	}
	m.isImporting = true
	m.host.SetImporting(true)
	m.host.SetImportingInfo(repoURL)
	m.host.SetImportProgress("counting", "")
	m.bgImportCh = make(chan tea.Msg, 64)
	ch := m.bgImportCh
	cacheDir := m.cacheDir
	workdir := m.workdir
	tickStopped := startImportTicker(ch)
	go func() {
		fetchOpts := importpkg.FetchOptions{
			RepoURL: repoURL,
			Owner:   repoInfo.Owner,
			Repo:    repoInfo.Repo,
		}
		counts, countErr := adapter.CountItems(fetchOpts)
		var mapping *importpkg.MappingFile
		var mapped importpkg.ItemCounts
		if countErr == nil {
			mapping = importpkg.ReadMapping(cacheDir, repoURL, "")
			if len(mapping.Items) == 0 {
				importpkg.RebuildMapping(workdir, mapping)
			}
			mapped = importpkg.CountMapped(mapping, counts)
		}
		stopImportTicker(tickStopped)
		ch <- importCountedMsg{
			Adapter: adapter,
			RepoURL: repoURL,
			Counts:  counts,
			Mapped:  mapped,
			Mapping: mapping,
			Err:     countErr,
		}
		close(ch)
	}()
	return drainBgImportCmd(ch)
}

// startImportTicker spawns the spinner-tick pump. Returns a (stop, stopped)
// pair: close stop[0] to signal the ticker to exit, then receive from
// stop[1] to wait for it to actually finish before closing the import
// channel (so the ticker never sends on a closed channel). Encapsulated as
// a helper because both the count phase and the import phase need it.
func startImportTicker(ch chan tea.Msg) [2]chan struct{} {
	tickDone := make(chan struct{})
	tickStopped := make(chan struct{})
	go func() {
		defer close(tickStopped)
		t := time.NewTicker(importTickInterval)
		defer t.Stop()
		for {
			select {
			case <-tickDone:
				return
			case <-t.C:
				select {
				case ch <- importTickMsg{}:
				default:
				}
			}
		}
	}()
	return [2]chan struct{}{tickDone, tickStopped}
}

// stopImportTicker signals the ticker goroutine to exit and waits for it.
func stopImportTicker(pair [2]chan struct{}) {
	close(pair[0])
	<-pair[1]
}

// createImportAdapter builds a SourceAdapter for the resolved host. Mirrors
// cli/gitsocial/import.go's createAdapter — kept local to avoid pulling the
// CLI's main package into the TUI.
func createImportAdapter(host protocol.HostingService, owner, repo string) (importpkg.SourceAdapter, error) {
	switch host {
	case protocol.HostGitHub:
		if err := ghimport.CheckGH(); err != nil {
			return nil, err
		}
		return ghimport.New(owner, repo), nil
	case protocol.HostGitLab:
		return glimport.New(owner, repo, glimport.AdapterOptions{}), nil
	case protocol.HostGitea:
		return nil, fmt.Errorf("gitea import not yet implemented")
	case protocol.HostBitbucket:
		return nil, fmt.Errorf("bitbucket import not yet supported")
	default:
		return nil, fmt.Errorf("unsupported platform")
	}
}

// startImport spawns the background import goroutine. Progress + completion
// stream back through m.bgImportCh; Update re-issues drainBgImportCmd after
// each message until the channel closes. Counts + Mapping are reused from
// the prior count phase so importpkg.Run skips redoing them.
func (m *Model) startImport(adapter importpkg.SourceAdapter, repoURL string, counts *importpkg.ItemCounts, mapping *importpkg.MappingFile) tea.Cmd {
	m.isImporting = true
	m.host.SetImporting(true)
	m.host.SetImportingInfo(repoURL)
	m.host.SetImportProgress("starting", "")
	m.bgImportCh = make(chan tea.Msg, 64)
	ch := m.bgImportCh
	workdir := m.workdir
	cacheDir := m.cacheDir
	repoInfo := protocol.ParseRepo(repoURL)
	if repoInfo == nil {
		// Should have been caught in beginImport, but guard anyway.
		go func() {
			ch <- importCompletedMsg{RepoURL: repoURL, Err: fmt.Errorf("could not parse owner/repo")}
			close(ch)
		}()
		return drainBgImportCmd(ch)
	}
	ticker := startImportTicker(ch)
	go func() {
		fetchOpts := importpkg.FetchOptions{
			RepoURL:  repoURL,
			Owner:    repoInfo.Owner,
			Repo:     repoInfo.Repo,
			SkipBots: true,
			State:    "all",
		}
		opts := importpkg.Options{
			WorkDir:    workdir,
			RepoURL:    repoURL,
			CacheDir:   cacheDir,
			Extensions: []string{"pm", "release", "review", "social"},
			LabelMode:  "auto",
			FetchOpts:  fetchOpts,
			Counts:     counts,
			Mapping:    mapping,
			OnProgress: func(ev importpkg.ProgressEvent) {
				select {
				case ch <- importProgressMsg{Event: ev}:
				default:
					// Channel full — drop progress update. Completion message
					// will still be delivered via the blocking send below.
				}
			},
		}
		stats, err := importpkg.Run(adapter, opts)
		// Re-sync workspace extension branches so newly imported commits show
		// up in the cache-backed views without waiting for a manual fetch.
		if err == nil {
			if syncErr := fetch.SyncWorkspace(workdir); syncErr != nil {
				log.Debug("post-import workspace sync failed", "error", syncErr)
			}
		}
		// Stop the spinner ticker and wait for it to exit before closing
		// the channel — prevents the ticker from sending on a closed chan.
		stopImportTicker(ticker)
		ch <- importCompletedMsg{Stats: stats, RepoURL: repoURL, Err: err}
		close(ch)
	}()
	return drainBgImportCmd(ch)
}

// drainBgImportCmd reads one message from the background import channel and
// returns it. Re-dispatched by Update after each message until the channel
// is closed.
func drainBgImportCmd(ch chan tea.Msg) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// pendingImportCounts returns the number of items that would actually be
// written by an import — i.e. found minus already-imported, summed across
// types. Negative (unknown) counts contribute zero rather than confusing the
// total.
func pendingImportCounts(found, mapped importpkg.ItemCounts) int {
	diff := func(f, m int) int {
		if f < 0 {
			return 0
		}
		if m < 0 {
			m = 0
		}
		if d := f - m; d > 0 {
			return d
		}
		return 0
	}
	return diff(found.Issues, mapped.Issues) +
		diff(found.PRs, mapped.PRs) +
		diff(found.Releases, mapped.Releases) +
		diff(found.Discussions, mapped.Discussions)
}

// buildImportConfirmPrompt builds the single-line confirmation prompt shown
// in the footer, listing only the types that have pending items.
func buildImportConfirmPrompt(repoURL string, found, mapped importpkg.ItemCounts) string {
	pending := func(f, m int) int {
		if f < 0 {
			return 0
		}
		if m < 0 {
			m = 0
		}
		if d := f - m; d > 0 {
			return d
		}
		return 0
	}
	var parts []string
	if n := pending(found.Issues, mapped.Issues); n > 0 {
		parts = append(parts, fmt.Sprintf("%d issues", n))
	}
	if n := pending(found.PRs, mapped.PRs); n > 0 {
		parts = append(parts, fmt.Sprintf("%d PRs", n))
	}
	if n := pending(found.Releases, mapped.Releases); n > 0 {
		parts = append(parts, fmt.Sprintf("%d releases", n))
	}
	if n := pending(found.Discussions, mapped.Discussions); n > 0 {
		parts = append(parts, fmt.Sprintf("%d discussions", n))
	}
	if len(parts) == 0 {
		return "Import from " + repoURL + "?"
	}
	return "Import " + strings.Join(parts, ", ") + " from " + repoURL + "?"
}

// formatImportProgress turns a ProgressEvent into footer phase + detail.
func formatImportProgress(ev importpkg.ProgressEvent) (phase, detail string) {
	switch ev.Phase {
	case importpkg.PhaseCount:
		return "counting", ev.Detail
	case importpkg.PhaseFetch:
		label := importExtensionLabels[ev.Extension]
		if label == "" {
			label = ev.Extension
		}
		phase = "fetching " + label
		if ev.ItemCount > 0 {
			if ev.ItemTotal > 0 {
				detail = fmt.Sprintf("%d/%d", ev.ItemCount, ev.ItemTotal)
			} else {
				detail = fmt.Sprintf("%d so far", ev.ItemCount)
			}
		}
	case importpkg.PhaseCommit:
		label := importExtensionLabels[ev.Extension]
		if label == "" {
			label = ev.Extension
		}
		phase = "writing " + label
	case importpkg.PhaseDone:
		label := importExtensionLabels[ev.Extension]
		if label == "" {
			label = ev.Extension
		}
		phase = label + " done"
	}
	return phase, detail
}

// formatImportSummary builds a single-line summary of what got imported.
func formatImportSummary(s importpkg.Stats) string {
	var parts []string
	if s.Issues > 0 {
		parts = append(parts, fmt.Sprintf("%d issues", s.Issues))
	}
	if s.Milestones > 0 {
		parts = append(parts, fmt.Sprintf("%d milestones", s.Milestones))
	}
	if s.PRs > 0 {
		parts = append(parts, fmt.Sprintf("%d PRs", s.PRs))
	}
	if s.Releases > 0 {
		parts = append(parts, fmt.Sprintf("%d releases", s.Releases))
	}
	if s.Posts > 0 {
		parts = append(parts, fmt.Sprintf("%d posts", s.Posts))
	}
	if s.Comments > 0 {
		parts = append(parts, fmt.Sprintf("%d comments", s.Comments))
	}
	if len(parts) == 0 {
		return "Nothing to import"
	}
	return "Imported " + strings.Join(parts, ", ")
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
