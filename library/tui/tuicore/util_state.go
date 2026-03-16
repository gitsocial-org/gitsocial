// util_state.go - Shared TUI state including dimensions, focus, fetch status, and view wrapper
package tuicore

import (
	"strings"
	"time"
)

// LogSeverity defines the severity of a log entry.
type LogSeverity int

const (
	LogSeverityWarn LogSeverity = iota
	LogSeverityError
)

// LogEntry is a single entry in the TUI error/warning log.
type LogEntry struct {
	Time     time.Time
	Severity LogSeverity
	Message  string
	Context  string // e.g. "fetch", "push", "settings", "editor"
}

// MessageType defines the type of status message for styling
type MessageType int

const (
	MessageTypeNone    MessageType = iota
	MessageTypeSuccess             // Green
	MessageTypeWarning             // Orange
	MessageTypeError               // Red
)

// SourceContext stores info about the source list when navigating to detail view.
type SourceContext struct {
	Path        string // Source view path (e.g., "/social/timeline")
	Index       int    // Current position in source list
	Total       int    // Total items in source
	SearchQuery string // Search query for highlighting in detail view
}

// State holds shared state accessible to all views.
// Views can read this state but should not mutate it directly.
type State struct {
	// Core paths
	Workdir  string
	CacheDir string

	// User info
	UserEmail string

	// Display dimensions
	Width  int
	Height int

	// Focus state
	Focused bool

	// Loading/progress state
	Loading    bool
	Syncing    bool
	Fetching   bool
	Pushing    bool
	Saving     bool
	Retracting bool

	// Fetch info (for progress display)
	FetchRepos int
	FetchLists int

	// Push info (for progress display)
	PushRemote string

	// Status info
	LastFetchTime time.Time
	NewItemCount  int

	// Messages
	Message     string
	MessageType MessageType
	MessageID   int
	Err         error

	// Choice prompt (app-level choice dialog rendered in footer)
	ChoicePrompt string

	// UI preferences
	ShowEmailOnCards bool
	ShowHelp         bool

	// Error log (session-level warnings and errors for the error log panel)
	ErrorLog []LogEntry

	// Border styling (set by views, cleared on navigation)
	BorderVariant string // "", "error", "warning"

	// Registries
	Registry    *Registry
	NavRegistry *NavRegistry

	// Router for navigation
	Router *Router

	// Detail view source context (for left/right navigation)
	DetailSource *SourceContext
}

// InnerWidth returns the content width inside the frame (borders + padding).
func (s *State) InnerWidth() int {
	return s.Width - 2 - ContentPaddingLeft - ContentPaddingRight
}

// InnerHeight returns the content height inside the frame (borders + top padding).
func (s *State) InnerHeight() int {
	return s.Height - 2 - ContentPaddingTop
}

// ClearMessage clears the current message and error.
func (s *State) ClearMessage() {
	s.Message = ""
	s.MessageType = MessageTypeNone
	s.Err = nil
}

// SetMessage sets a status message with type.
func (s *State) SetMessage(msg string, msgType MessageType) {
	s.Message = msg
	s.MessageType = msgType
	s.Err = nil
}

// SetError sets an error message.
func (s *State) SetError(err error) {
	s.Err = err
	s.Message = ""
	s.MessageType = MessageTypeNone
}

// AddLogEntry appends a warning or error entry to the session error log.
func (s *State) AddLogEntry(severity LogSeverity, message, context string) {
	s.ErrorLog = append(s.ErrorLog, LogEntry{
		Time:     time.Now(),
		Severity: severity,
		Message:  message,
		Context:  context,
	})
}

// ErrorLogCount returns the number of entries in the error log.
func (s *State) ErrorLogCount() int {
	return len(s.ErrorLog)
}

// ClearErrorLog removes all entries from the error log.
func (s *State) ClearErrorLog() {
	s.ErrorLog = nil
}

// ViewWrapper handles vertical padding and footer rendering consistently across views.
type ViewWrapper struct {
	state *State
}

// NewViewWrapper creates a new view wrapper.
func NewViewWrapper(state *State) *ViewWrapper {
	return &ViewWrapper{state: state}
}

// Render wraps content with vertical padding and appends the footer.
// Status messages and choice prompts override the view-provided footer.
func (w *ViewWrapper) Render(content, footer string) string {
	if w.state.ChoicePrompt != "" {
		footer = RenderChoicePromptFooter(w.state.ChoicePrompt, w.ContentWidth())
	} else if w.state.Syncing {
		footer = RenderSyncingFooter(w.ContentWidth())
	} else if w.state.Fetching {
		footer = RenderFetchingFooter(w.state.FetchRepos, w.state.FetchLists, w.ContentWidth())
	} else if w.state.Pushing {
		footer = RenderPushingFooter(w.state.PushRemote, w.ContentWidth())
	} else if w.state.Saving {
		footer = RenderSavingFooter(w.ContentWidth())
	} else if w.state.Retracting {
		footer = RenderRetractingFooter(w.ContentWidth())
	} else if w.state.Message != "" {
		footer = RenderMessageFooter(w.state.Message, w.state.MessageType, w.ContentWidth())
	}
	var b strings.Builder
	b.WriteString(content)

	contentLines := strings.Count(content, "\n") + 1
	footerLines := strings.Count(footer, "\n") + 1
	targetHeight := w.state.InnerHeight()

	for contentLines < targetHeight-footerLines+1 {
		b.WriteString("\n")
		contentLines++
	}

	b.WriteString(footer)
	return b.String()
}

// ContentHeight returns available height for main content (excluding footer).
func (w *ViewWrapper) ContentHeight() int {
	return w.state.InnerHeight() - 3
}

// ContentWidth returns available width for content.
func (w *ViewWrapper) ContentWidth() int {
	return w.state.InnerWidth()
}
