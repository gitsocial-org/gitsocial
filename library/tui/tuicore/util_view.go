// util_view.go - View interface, context definitions, and lifecycle definitions
package tuicore

import (
	"sync"

	tea "charm.land/bubbletea/v2"
)

// View is the interface that all views must implement.
// Views are responsible for handling input and rendering output.
type View interface {
	// Update handles messages and returns any commands to execute.
	// The state parameter provides access to shared state.
	Update(msg tea.Msg, state *State) tea.Cmd

	// Render returns the view's content as a string.
	// The state parameter provides access to shared state for rendering.
	Render(state *State) string
}

// ViewActivator is an optional interface for views that need initialization
// when they become active (e.g., loading data).
type ViewActivator interface {
	// Activate is called when the view becomes active.
	// Returns a command to load initial data.
	Activate(state *State) tea.Cmd
}

// ViewRefresher is an optional interface for views that can reload data
// without resetting view state (cursor position, scroll, etc.).
type ViewRefresher interface {
	// Refresh reloads data in place, preserving cursor and scroll state.
	Refresh(state *State) tea.Cmd
}

// InputHandler is an optional interface for views with text input.
type InputHandler interface {
	// IsInputActive returns true if the view is handling text input.
	IsInputActive() bool
}

// Sizable is an optional interface for views that need size updates.
type Sizable interface {
	// SetSize updates the view's dimensions.
	SetSize(width, height int)
}

// SourceListProvider is an optional interface for views that provide a list
// of posts and support navigating between them from the detail view.
type SourceListProvider interface {
	// GetItemAt returns the post ID at the given index.
	GetItemAt(index int) (id string, ok bool)
	// GetItemCount returns the total number of items in the list.
	GetItemCount() int
}

// DisplayItemProvider is an optional interface for source views that can
// return full DisplayItems (not just IDs). This enables type-aware left/right
// navigation when the source list contains mixed item types (e.g., search results).
type DisplayItemProvider interface {
	GetDisplayItemAt(index int) (DisplayItem, bool)
}

// ViewHost is the interface for registering views with a host.
// Extensions use this to register their views during initialization.
type ViewHost interface {
	AddView(pattern string, view View)
	State() *State
}

// Context identifies the current TUI state for keybindings, footer, and views.
// Uses string type for hierarchical naming (e.g., "social.timeline" groups under "social").
type Context string

// Context registry for dynamic registration
var (
	registeredContexts   = make(map[Context]struct{})
	registeredContextsMu sync.RWMutex
)

// RegisterContext registers a context and returns it.
// Extensions call this to declare their contexts.
func RegisterContext(ctx Context) Context {
	registeredContextsMu.Lock()
	defer registeredContextsMu.Unlock()
	registeredContexts[ctx] = struct{}{}
	return ctx
}

// AllContexts returns all registered non-global contexts.
func AllContexts() []Context {
	registeredContextsMu.RLock()
	defer registeredContextsMu.RUnlock()
	result := make([]Context, 0, len(registeredContexts))
	for ctx := range registeredContexts {
		result = append(result, ctx)
	}
	return result
}

// Global context (always active)
const Global Context = "global"

// Core contexts - registered via init()
var (
	Settings   = RegisterContext("core.settings")
	Config     = RegisterContext("core.config")
	Cache      = RegisterContext("core.cache")
	Analytics  = RegisterContext("core.analytics")
	Help       = RegisterContext("core.help")
	CommitDiff = RegisterContext("core.diff")
	ErrorLog   = RegisterContext("core.errorlog")
)

// Social extension contexts - registered via init()
var (
	Timeline      = RegisterContext("social.timeline")
	Detail        = RegisterContext("social.detail")
	Thread        = RegisterContext("social.thread")
	Search        = RegisterContext("social.search")
	SearchHelp    = RegisterContext("social.search_help")
	Repository    = RegisterContext("social.repository")
	MyRepository  = RegisterContext("social.my_repository")
	ListPicker    = RegisterContext("social.list_picker")
	ListPosts     = RegisterContext("social.list_posts")
	ListRepos     = RegisterContext("social.list_repos")
	Notifications = RegisterContext("social.notifications")
	History       = RegisterContext("social.history")
	RepoLists     = RegisterContext("social.repo_lists")
)

// PM extension contexts - registered via init()
var (
	PMBoard            = RegisterContext("pm.board")
	PMIssues           = RegisterContext("pm.issues")
	PMIssueDetail      = RegisterContext("pm.issue_detail")
	PMIssueHistory     = RegisterContext("pm.issue_history")
	PMConfig           = RegisterContext("pm.config")
	PMMilestones       = RegisterContext("pm.milestones")
	PMMilestoneDetail  = RegisterContext("pm.milestone_detail")
	PMMilestoneHistory = RegisterContext("pm.milestone_history")
	PMSprints          = RegisterContext("pm.sprints")
	PMSprintDetail     = RegisterContext("pm.sprint_detail")
	PMSprintHistory    = RegisterContext("pm.sprint_history")
)

// Release extension contexts - registered via init()
var (
	ReleaseList   = RegisterContext("release.list")
	ReleaseDetail = RegisterContext("release.detail")
	ReleaseSBOM   = RegisterContext("release.sbom")
)

// Review extension contexts - registered via init()
var (
	ReviewPRs       = RegisterContext("review.prs")
	ReviewPRDetail  = RegisterContext("review.pr_detail")
	ReviewPRHistory = RegisterContext("review.pr_history")
	ReviewDiff      = RegisterContext("review.diff")
	ReviewInterdiff = RegisterContext("review.interdiff")
	ReviewForks     = RegisterContext("review.forks")
)

// Domain IDs for top-level navigation domains
const (
	DomainSocial    = "social"
	DomainCache     = "cache"
	DomainConfig    = "config"
	DomainSettings  = "settings"
	DomainPM        = "pm"
	DomainReview    = "review"
	DomainRelease   = "release"
	DomainCICD      = "cicd"
	DomainInfra     = "infra"
	DomainOps       = "ops"
	DomainSecurity  = "security"
	DomainDM        = "dm"
	DomainPortfolio = "portfolio"
)

// DomainOf returns the top-level domain (e.g., "social" from "social.timeline")
func DomainOf(ctx Context) string {
	s := string(ctx)
	for i, c := range s {
		if c == '.' {
			return s[:i]
		}
	}
	return s
}
