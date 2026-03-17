// util_keys.go - Keybinding registry, handlers, and global shortcuts
package tuicore

import (
	tea "charm.land/bubbletea/v2"
)

// RawViewHandler is a shared binding handler that toggles raw view on the current panel.
var RawViewHandler = func(ctx *HandlerContext) (bool, tea.Cmd) {
	if ctx.Panel == nil {
		return false, nil
	}
	return true, ctx.Panel.ShowRawView()
}

// SelectedList contains info about the currently selected list
type SelectedList struct {
	ID   string
	Name string
}

// PanelActions defines actions that handlers can invoke on the content panel
type PanelActions interface {
	// State
	GetSelectedDisplayItem() (DisplayItem, bool)
	GetSelectedList() *SelectedList
	IsExternalList() bool
	// Navigation
	GoBack() tea.Cmd
	OpenRepository(url, branch string) tea.Cmd
	OpenMyRepo() tea.Cmd
	OpenLists() tea.Cmd
	// Post actions
	EditPost() tea.Cmd
	RetractPost() tea.Cmd
	ShowHistory() tea.Cmd
	ShowRawView() tea.Cmd
	// List actions
	CreateList() tea.Cmd
	DeleteList() tea.Cmd
	LoadMorePosts() tea.Cmd
	ToggleListView() tea.Cmd
	// Notification actions
	MarkNotificationRead() tea.Cmd
	MarkAllNotificationsRead() tea.Cmd
	MarkNotificationUnread() tea.Cmd
	MarkAllNotificationsUnread() tea.Cmd
	ToggleNotificationFilter() tea.Cmd
	// Cache actions
	RefreshCache() tea.Cmd
	ClearCacheDB() tea.Cmd
	ClearCacheRepos() tea.Cmd
	ClearCacheForks() tea.Cmd
	ClearCacheAll() tea.Cmd
	// Repository actions
	FollowRepository() tea.Cmd
	OpenRepoLists() tea.Cmd
	SearchInRepository() tea.Cmd
	// List actions (search)
	SearchInList() tea.Cmd
}

// HandlerContext provides access to TUI state for handlers
type HandlerContext struct {
	Workdir    string
	CacheDir   string
	SelectedID string

	// SelectedItem provides the full item for type-aware actions.
	// Use this to get item type info and invoke item-specific actions.
	SelectedItem DisplayItem

	// Callbacks requiring app.go state
	ToggleFocus  func()
	Navigate     func(Context) tea.Cmd
	OpenEditor   func(mode, targetID string) tea.Cmd
	StartFetch   func() tea.Cmd
	StartPush    func() tea.Cmd
	StartLFSPush func() tea.Cmd

	// Panel for direct action calls
	Panel PanelActions
}

// Handler processes a keybinding, returns whether it handled the key and any command
type Handler func(ctx *HandlerContext) (handled bool, cmd tea.Cmd)

// Binding defines a single keybinding
type Binding struct {
	Key      string
	Label    string
	Contexts []Context
	Handler  Handler
}

// BindingProvider is implemented by views that define keybindings
type BindingProvider interface {
	Bindings() []Binding
}

// Registry manages keybindings with context-based resolution
type Registry struct {
	bindings []Binding
}

// NewRegistry creates a new keybinding registry.
func NewRegistry() *Registry {
	return &Registry{
		bindings: make([]Binding, 0),
	}
}

// Register adds a binding to the registry.
func (r *Registry) Register(b Binding) {
	if b.Handler == nil {
		panic("keybinding " + b.Key + " must have a handler")
	}
	r.bindings = append(r.bindings, b)
}

// RegisterView adds all bindings from a view that implements BindingProvider.
func (r *Registry) RegisterView(v BindingProvider) {
	for _, b := range v.Bindings() {
		r.Register(b)
	}
}

// Resolve finds the binding for a key in the given context.
func (r *Registry) Resolve(ctx Context, key string) *Binding {
	for i := range r.bindings {
		b := &r.bindings[i]
		if b.Key != key {
			continue
		}
		if containsContext(b.Contexts, ctx) {
			return b
		}
	}
	return nil
}

// ForContext returns all bindings active in the given context.
func (r *Registry) ForContext(ctx Context) []Binding {
	result := make([]Binding, 0)
	seen := make(map[string]bool)
	for _, b := range r.bindings {
		if containsContext(b.Contexts, ctx) {
			if !seen[b.Key] {
				result = append(result, b)
				seen[b.Key] = true
			}
		}
	}
	return result
}

// containsContext checks if a context is in the list.
// Global context matches any context.
func containsContext(contexts []Context, ctx Context) bool {
	for _, c := range contexts {
		if c == ctx || c == Global {
			return true
		}
	}
	return false
}

// AllContextsExcept returns all contexts except the specified ones.
func AllContextsExcept(exclude ...Context) []Context {
	all := AllContexts()
	result := make([]Context, 0, len(all))
	for _, ctx := range all {
		excluded := false
		for _, ex := range exclude {
			if ctx == ex {
				excluded = true
				break
			}
		}
		if !excluded {
			result = append(result, ctx)
		}
	}
	return result
}

// GlobalKey defines a global shortcut key
type GlobalKey struct {
	Key    string  // Shortcut key
	Domain string  // Domain/extension ID (or "_core" for core features)
	Target Context // Target context to navigate to
	Label  string  // Label for the key binding
}

// CoreKeys defines global shortcuts for core features (shown in footer).
// "/" for search is registered separately with special handling.
var CoreKeys = []GlobalKey{
	{Key: "@", Domain: "_core", Target: Notifications, Label: "notifications"},
	{Key: "%", Domain: "_core", Target: Analytics, Label: "analytics"},
	{Key: "!", Domain: "_core", Target: ErrorLog, Label: "errors"},
}

// ExtensionKeys defines global extension shortcuts (uppercase, highlighted in sidebar).
// See KEYS.md for design rationale.
var ExtensionKeys = []GlobalKey{
	{Key: "T", Domain: DomainSocial, Target: Timeline, Label: "timeline"},
	{Key: "B", Domain: DomainPM, Target: PMBoard, Label: "boards"},
	{Key: "P", Domain: DomainReview, Target: ReviewPRs, Label: "reviews"},
	{Key: "R", Domain: DomainRelease, Target: ReleaseList, Label: "releases"},
	{Key: "C", Domain: DomainCICD, Target: Global, Label: "actions"},         // placeholder
	{Key: "I", Domain: DomainInfra, Target: Global, Label: "infrastructure"}, // placeholder
	{Key: "O", Domain: DomainOps, Target: Global, Label: "operations"},       // placeholder
	{Key: "S", Domain: DomainSecurity, Target: Global, Label: "security"},    // placeholder
	{Key: ">", Domain: DomainDM, Target: Global, Label: "dm"},                // placeholder
	{Key: "F", Domain: DomainPortfolio, Target: Global, Label: "overview"},   // placeholder
}

// GetExtensionKey returns the GlobalKey for a domain, or nil if not found.
func GetExtensionKey(domain string) *GlobalKey {
	for i := range ExtensionKeys {
		if ExtensionKeys[i].Domain == domain {
			return &ExtensionKeys[i]
		}
	}
	return nil
}

// RegisterGlobalKeys adds global keybindings that appear at end of all footers.
func RegisterGlobalKeys(r *Registry) {
	// esc:back - available everywhere except Timeline
	backHandler := func(ctx *HandlerContext) (bool, tea.Cmd) {
		if ctx.Panel != nil {
			return true, ctx.Panel.GoBack()
		}
		return false, nil
	}
	backContexts := AllContextsExcept(Timeline)
	r.Register(Binding{
		Key:      "esc",
		Label:    "back",
		Contexts: backContexts,
		Handler:  backHandler,
	})

	// Register core keys (@:notifications - shown in footer)
	for _, ck := range CoreKeys {
		ck := ck // capture for closure
		r.Register(Binding{
			Key:      ck.Key,
			Label:    ck.Label,
			Contexts: AllContextsExcept(ck.Target),
			Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
				if ctx.Navigate != nil {
					return true, ctx.Navigate(ck.Target)
				}
				return true, nil
			},
		})
	}

	// Register extension keys (T, B, P, R, A, S, D, O - uppercase, highlighted in sidebar)
	for _, ek := range ExtensionKeys {
		ek := ek // capture for closure
		// Skip placeholders (Target == Global means not implemented)
		if ek.Target == Global {
			continue
		}
		r.Register(Binding{
			Key:      ek.Key,
			Label:    ek.Label,
			Contexts: AllContextsExcept(ek.Target),
			Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
				if ctx.Navigate != nil {
					return true, ctx.Navigate(ek.Target)
				}
				return true, nil
			},
		})
	}

	// f:fetch - available everywhere except Detail/Thread/History
	r.Register(Binding{
		Key:      "f",
		Label:    "fetch",
		Contexts: AllContextsExcept(Detail, Thread, History),
		Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
			if ctx.StartFetch != nil {
				return true, ctx.StartFetch()
			}
			return false, nil
		},
	})

	// /:search - available everywhere except Search itself
	r.Register(Binding{
		Key:      "/",
		Label:    "search",
		Contexts: AllContextsExcept(Search),
		Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
			if ctx.Navigate != nil {
				return true, ctx.Navigate(Search)
			}
			return false, nil
		},
	})

	r.Register(Binding{
		Key:      "`",
		Label:    "focus",
		Contexts: []Context{Global},
		Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
			if ctx.ToggleFocus != nil {
				ctx.ToggleFocus()
			}
			return true, nil
		},
	})

	r.Register(Binding{
		Key:      "q",
		Label:    "quit",
		Contexts: []Context{Global},
		Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
			return true, tea.Quit
		},
	})

	r.Register(Binding{
		Key:      "?",
		Label:    "help",
		Contexts: AllContextsExcept(Help),
		Handler: func(ctx *HandlerContext) (bool, tea.Cmd) {
			if ctx.Navigate != nil {
				return true, ctx.Navigate(Help)
			}
			return false, nil
		},
	})
}
