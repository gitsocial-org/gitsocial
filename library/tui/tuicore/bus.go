// bus.go - Message bus for decoupled extension message handling
package tuicore

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// AppContext provides access to app state for message handlers.
// Extensions use this interface to interact with the app without depending on it.
type AppContext interface {
	// State
	Workdir() string
	CacheDir() string
	IsFetching() bool
	SetFetching(bool)
	IsPushing() bool
	SetPushing(bool)
	// Navigation
	Router() *Router
	// Host access
	Host() HostContext
	// Nav panel
	Nav() NavContext
	// Async commands
	LoadLists() tea.Cmd
	LoadUnreadCount() tea.Cmd
	LoadUnpushedCount() tea.Cmd
	LoadUnpushedLFSCount() tea.Cmd
	RefreshTimeline() tea.Cmd
	RefreshCacheSize() tea.Cmd
	FetchRepo(repoURL string) tea.Cmd
}

// HostContext provides host-level operations for message handlers.
type HostContext interface {
	// Messages
	SetMessage(msg string, msgType MessageType)
	SetMessageWithTimeout(msg string, msgType MessageType, d time.Duration) tea.Cmd
	// State flags
	SetFetchStatus(fetchTime time.Time, newItems int)
	SetFetchingInfo(repos, lists int)
	SetSaving(bool)
	SetRetracting(bool)
	// View operations
	Update(msg tea.Msg) tea.Cmd
	ActivateView() tea.Cmd
	// Source navigation
	GetSourceItem(offset int) (postID string, newIndex int, ok bool)
	UpdateSourceIndex(index, total int)
	// State access
	State() *State
}

// NavContext provides nav panel operations for message handlers.
type NavContext interface {
	SetUnreadCount(count int)
	SetUnpushedLFSCount(count int)
	SetCacheSize(size string)
	SetErrorLogCount(count int)
	Registry() *NavRegistry
}

// MessageHandler handles a specific message type.
// Returns (handled, cmd) - if handled is false, the bus tries the next handler.
type MessageHandler func(msg tea.Msg, ctx AppContext) (handled bool, cmd tea.Cmd)

var (
	messageHandlers   []MessageHandler
	messageHandlersMu sync.RWMutex
)

// RegisterMessageHandler registers a handler for messages.
// Handlers are called in registration order until one returns handled=true.
func RegisterMessageHandler(handler MessageHandler) {
	messageHandlersMu.Lock()
	defer messageHandlersMu.Unlock()
	messageHandlers = append(messageHandlers, handler)
}

// DispatchMessage sends a message to registered handlers.
// Returns (handled, cmd) - if no handler handles it, returns (false, nil).
func DispatchMessage(msg tea.Msg, ctx AppContext) (bool, tea.Cmd) {
	messageHandlersMu.RLock()
	defer messageHandlersMu.RUnlock()
	for _, handler := range messageHandlers {
		if handled, cmd := handler(msg, ctx); handled {
			return true, cmd
		}
	}
	return false, nil
}
