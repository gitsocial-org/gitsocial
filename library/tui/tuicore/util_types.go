// util_types.go - Message types, function signatures, and DisplayItem interface for views
package tuicore

import (
	"time"

	"charm.land/lipgloss/v2"
)

// ItemType identifies the extension and type of a DisplayItem for navigation routing.
type ItemType struct {
	Extension string // "social", "pm", "review", etc.
	Type      string // "post", "comment", "issue", "milestone", etc.
}

// ItemToCardFunc renders items of a specific ext/type to Cards.
// Extensions register these for their types; unknown types fall back to defaultItemToCard.
type ItemToCardFunc func(data any, resolver ItemResolver) Card

// DimmedCheckFunc checks if an item should be rendered dimmed.
// Extensions register these for their types.
type DimmedCheckFunc func(data any) bool

// Editor modes
type EditorMode int

const (
	EditorModePost EditorMode = iota
	EditorModeComment
	EditorModeRepost
	EditorModeEdit
	EditorModeIssue
)

// Editor messages
type EditorDoneMsg struct {
	Content  string
	Mode     EditorMode
	TargetID string
}

type EditorErrorMsg struct {
	Err error
}

// Navigation messages
type NavigateMsg struct {
	Location    Location
	Action      NavAction
	SourcePath  string // Source view path for detail navigation
	SourceIndex int    // Current index in source list
	SourceTotal int    // Total items in source list
	SearchQuery string // Search query for highlighting in detail view
}

type NavAction int

const (
	NavPush NavAction = iota
	NavReplace
	NavBack
)

type FocusMsg struct {
	Panel int
}

// Editor open message
type OpenEditorMsg struct {
	Mode           string
	TargetID       string
	InitialContent string
}

// Fetch trigger
type TriggerFetchMsg struct{}

// WorkspaceFetchModeMsg carries pre-flight branch info for workspace mode choice.
type WorkspaceFetchModeMsg struct {
	Branches []string
}

type WorkspaceInitializedMsg struct{}

// DisplayItem is the interface for any item that can be displayed in a CardList.
// Extensions implement this to provide items for display.
type DisplayItem interface {
	ItemID() string
	ItemType() ItemType
	ToCard(resolver ItemResolver) Card
	Timestamp() time.Time
	IsDimmed() bool
}

// ItemResolver resolves item IDs to DisplayItems (for nested items like parent posts)
type ItemResolver func(itemID string) (DisplayItem, bool)

// Item is a universal DisplayItem that wraps any extension data.
// It uses registered CardRenderers to convert to Card format.
type Item struct {
	ID           string    // Canonical ref: repo#commit:hash@branch
	Ext          string    // Extension: "social", "pm", etc.
	Type         string    // Type within extension: "post", "issue", etc.
	Time         time.Time // Item timestamp
	Data         any       // Extension-specific data (social.Post, pm.Issue, etc.)
	OriginalExt  string    // For cross-extension refs (e.g., social comment on pm issue)
	OriginalType string
	OriginalID   string // ID of the referenced item for cross-extension navigation
}

// ItemID returns the item's unique identifier.
func (i Item) ItemID() string { return i.ID }

// ItemType returns the extension and type for navigation routing.
// For cross-extension items (e.g., comments on PM issues, or PM items in social cache),
// returns the original's type for correct navigation to detail views.
func (i Item) ItemType() ItemType {
	if i.OriginalExt != "" {
		return ItemType{Extension: i.OriginalExt, Type: i.OriginalType}
	}
	return ItemType{Extension: i.Ext, Type: i.Type}
}

// ToCard converts the item to a Card using the registered renderer.
func (i Item) ToCard(resolver ItemResolver) Card {
	renderer := GetItemToCardFunc(ItemType{Extension: i.Ext, Type: i.Type})
	return renderer(i.Data, resolver)
}

// Timestamp returns the item's creation time.
func (i Item) Timestamp() time.Time { return i.Time }

// IsDimmed returns whether the item should be rendered dimmed.
func (i Item) IsDimmed() bool {
	checker := GetDimmedCheckFunc(ItemType{Extension: i.Ext, Type: i.Type})
	return checker(i.Data)
}

// NewItem creates a universal Item from extension-specific data.
func NewItem(id, ext, itemType string, timestamp time.Time, data any) Item {
	return Item{
		ID:   id,
		Ext:  ext,
		Type: itemType,
		Time: timestamp,
		Data: data,
	}
}

// SearchResult holds search results for the search view.
type SearchResult struct {
	Items         []DisplayItem
	Total         int
	TotalSearched int
	HasMore       bool
}

// SearchFunc searches for items matching a query.
type SearchFunc func(workdir, query, scope string, limit, offset int) (SearchResult, error)

// NotificationMeta holds metadata for a notification (for mark-read operations).
type NotificationMeta struct {
	RepoURL   string
	Hash      string
	Branch    string
	Type      string // "comment", "repost", "quote", "mention", "follow"
	ActorRepo string // for follow notifications
	IsRead    bool
}

// NotificationsResult holds notifications for the notifications view.
type NotificationsResult struct {
	Items []DisplayItem
	Meta  []NotificationMeta // parallel to Items
}

// GetNotificationsFunc loads notifications.
type GetNotificationsFunc func(workdir string, unreadOnly bool) (NotificationsResult, error)

// MarkReadFunc marks a notification as read.
type MarkReadFunc func(repoURL, hash, branch string) error

// MarkUnreadFunc marks a notification as unread.
type MarkUnreadFunc func(repoURL, hash, branch string) error

// MarkAllReadFunc marks all notifications as read in bulk.
type MarkAllReadFunc func(workdir string) error

// MarkAllUnreadFunc marks all notifications as unread in bulk.
type MarkAllUnreadFunc func(workdir string) error

// ResolveItemFunc resolves an item ID to a DisplayItem.
type ResolveItemFunc func(workdir, itemID string) (DisplayItem, bool)

// SourceNavigateMsg is sent when navigating to prev/next item in detail view.
type SourceNavigateMsg struct {
	Offset       int                      // -1 for previous, +1 for next
	MakeLocation func(id string) Location // Builds the detail location from an item ID
}

// InteractionCountsRefreshedMsg is sent when interaction counts are refreshed
type InteractionCountsRefreshedMsg struct {
	PostID   string
	Comments int
	Reposts  int
	Quotes   int
	Err      error
}

// UnreadCountMsg is sent with unread notification count
type UnreadCountMsg struct {
	Count int
}

// UnpushedCountMsg is sent with unpushed posts count
type UnpushedCountMsg struct {
	Count int
}

// UnpushedLFSCountMsg is sent with unpushed LFS objects count
type UnpushedLFSCountMsg struct {
	Count int
}

// LFSPushCompletedMsg is sent when LFS push completes
type LFSPushCompletedMsg struct {
	Count int
	Err   error
}

// CacheSizeMsg is sent with cache size
type CacheSizeMsg struct {
	Size string
}

// NavVisibilityMsg requests showing/hiding the nav panel (e.g., fullscreen diff mode)
type NavVisibilityMsg struct {
	Hidden bool
}

// LogErrorMsg adds an entry to the session error log via the Update loop.
type LogErrorMsg struct {
	Severity LogSeverity
	Message  string
	Context  string
}

// HeaderPart is a segment of a card subtitle, optionally linked to a navigation target.
type HeaderPart struct {
	Text string
	Link *Location // nil = plain text, non-nil = clickable
}

// CardStat is a single stat entry, optionally linked to a navigation target.
type CardStat struct {
	Text string
	Link *Location
}

// CardLink is a resolved link from a card's title, subtitle, or stats.
type CardLink struct {
	Label    string
	Location Location
}

// Card is a generic, extension-agnostic display unit that can represent
// social posts, PM issues, PR reviews, or any other item type.
type Card struct {
	Header       CardHeader
	Content      CardContent
	Stats        []CardStat
	Nested       []NestedCard
	ContentLinks []CardLink // Links extracted from body text (populated during rendering)
}

// AllLinks collects all clickable links from the card's header, stats, and body content.
func (c Card) AllLinks() []CardLink {
	var links []CardLink
	if c.Header.TitleLink != nil {
		links = append(links, CardLink{Label: c.Header.Title, Location: *c.Header.TitleLink})
	}
	for _, p := range c.Header.Subtitle {
		if p.Link != nil {
			links = append(links, CardLink{Label: p.Text, Location: *p.Link})
		}
	}
	for _, s := range c.Stats {
		if s.Link != nil {
			links = append(links, CardLink{Label: s.Text, Location: *s.Link})
		}
	}
	links = append(links, c.ContentLinks...)
	return links
}

// CardHeader contains the title line information
type CardHeader struct {
	Title          string       // "Author Name" or "Issue #123"
	TitleLink      *Location    // Navigation target when title is clicked
	Subtitle       []HeaderPart // Structured subtitle parts joined with " · "
	Badge          string       // Optional: "reposted", "replied", "closed", etc.
	Icon           string       // Type indicator: "○" issue, "◇" milestone, "◷" sprint, "⏏" release, "↩" comment, "↻" repost
	IsMe           bool         // True if authored by current user (cyan title)
	IsMutualFollow bool         // True if mutual follow (gold title)
	IsOwnRepo      bool         // True if item is from the workspace repository (cyan title)
	IsAssigned     bool         // True if assigned to current user (purple title)
	IsEdited       bool         // True if post has been edited
	IsRetracted    bool         // True if post has been deleted
	IsStale        bool         // True if commit no longer exists in live branch
}

// TitleStyle returns the appropriate style for the header title based on flags.
func (h CardHeader) TitleStyle(dimmed bool) lipgloss.Style {
	if h.IsMe {
		if dimmed {
			return MutedMeTitle
		}
		return MeTitle
	}
	if h.IsAssigned {
		if dimmed {
			return MutedAssignedTitle
		}
		return AssignedTitle
	}
	if h.IsOwnRepo {
		if dimmed {
			return MutedOwnRepoTitle
		}
		return OwnRepoTitle
	}
	if h.IsMutualFollow {
		if dimmed {
			return MutedMutualTitle
		}
		return MutualTitle
	}
	if dimmed {
		return MutedTitle
	}
	return Title
}

// CardContent contains the body text and rendering options
type CardContent struct {
	Text string
}

// NestedCard represents an embedded card (parent, original, related, etc.)
type NestedCard struct {
	Card     Card
	Position string // "before" or "after" main content
	Dimmed   bool
	MaxLines int // 0 = use default (5)
}

// CardOptions controls how a card is rendered
type CardOptions struct {
	MaxLines      int              // 0 = unlimited, 1 = single line, 5 = default
	ShowStats     bool             // Whether to show stats line
	Selected      bool             // Whether card is currently selected
	Width         int              // Width for padding/separator
	Separator     bool             // Whether to render separator line at bottom
	Dimmed        bool             // Whether to render with dim styling
	Bold          bool             // Bold content styling
	Retracted     bool             // Whether post is deleted/retracted (dim + background)
	Indent        string           // Prefix for indented threads
	WrapWidth     int              // Word wrap width (0 = no wrap)
	Markdown      bool             // Use glamour markdown rendering
	Raw           bool             // Disable all rendering (markdown and math)
	CommitMessage string           // When set, replaces body with full commit message (raw display)
	HighlightText string           // Text to highlight in content
	Anchors       *AnchorCollector // When set, zone-marks linked parts for mouse interaction
}
