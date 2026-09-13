// types.go - Social extension data types
package social

import (
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
)

// Result names the result type every social API returns.
type Result[T any] = result.Result[T]

// success wraps data in a successful result.
func success[T any](data T) Result[T] { return result.Ok(data) }

// failure builds a failed result from an error code and message.
func failure[T any](code, message string) Result[T] { return result.Err[T](code, message) }

// failureWithDetails builds a failed result carrying the underlying error.
func failureWithDetails[T any](code, message string, details interface{}) Result[T] {
	return result.ErrWithDetails[T](code, message, details)
}

type Author struct {
	Name  string
	Email string
}

type Interactions struct {
	Comments int
	Reposts  int
	Quotes   int
}

type Display struct {
	RepositoryName     string
	CommitHash         string
	TotalReposts       int
	IsUnpushed         bool
	IsWorkspacePost    bool
	FollowsYou         bool
	IsNotificationRead bool
	IsVerified         bool
	// IsEditorVerified marks a latest edit signed by a distinct editor's verified key.
	IsEditorVerified bool
	Badge            string
	UserEmail        string // Current user's email for own-post detection in rendering
	ShowEmail        bool   // Whether to show email in card header
	Workdir          string // For cross-extension lookups (e.g., PR head-pushed check)
}

type Post struct {
	ID              string
	Repository      string
	Branch          string
	Author          Author
	Timestamp       time.Time
	Content         string
	Type            PostType
	CleanContent    string
	OriginalPostID  string
	ParentCommentID string
	EditOf          string
	EditorName      string
	EditorEmail     string
	// Latest edit commit's ref, which annotateVerified verifies against the editor or the author.
	EditRepoURL      string
	EditHash         string
	EditBranch       string
	IsRetracted      bool
	IsEdited         bool
	HasProposedEdits bool
	Depth            int
	Interactions     Interactions
	Remote           string
	IsVirtual        bool
	IsStale          bool
	IsWorkspacePost  bool
	Display          Display
	// OriginalExtension and OriginalType come from the GitMsg-Ref, for cross-extension navigation.
	OriginalExtension string
	OriginalType      string
	// HeaderExt, HeaderType and HeaderState are the item's own header fields, which route its detail view.
	HeaderExt   string
	HeaderType  string
	HeaderState string
	// Labels carries scoped tags parsed from the GitMsg `labels` header field.
	Labels []string
	Origin *protocol.Origin
}

type List struct {
	ID                string
	Name              string
	Version           string
	Repositories      []string
	IsUnpushed        bool
	IsFollowedLocally bool
}

type RepositoryType string

const repositoryTypeOther RepositoryType = "other"

type Repository struct {
	ID            string
	URL           string
	Name          string
	Path          string
	Branch        string
	DefaultBranch string
	Type          RepositoryType
	LastFetchTime *time.Time
	FetchedRanges []FetchedRange
	RemoteName    string
	Lists         []string
}

type FetchedRange struct {
	Start string
	End   string
}

type LogEntryType string

const (
	logTypePost       LogEntryType = "post"
	logTypeComment    LogEntryType = "comment"
	logTypeRepost     LogEntryType = "repost"
	logTypeQuote      LogEntryType = "quote"
	logTypeListCreate LogEntryType = "list-create"
	logTypeListDelete LogEntryType = "list-delete"
	logTypeConfig     LogEntryType = "config"
	logTypeMetadata   LogEntryType = "metadata"
)

type LogEntry struct {
	Hash       string
	Timestamp  time.Time
	Author     Author
	Type       LogEntryType
	Details    string
	Repository string
	PostID     string
}

type RelatedRepository struct {
	Repository
	Relationships RelationshipInfo
}

type RelationshipInfo struct {
	SharedLists   []string
	SharedAuthors []string
}

type NotificationType string

const (
	notificationTypeComment NotificationType = "comment"
	notificationTypeRepost  NotificationType = "repost"
	notificationTypeQuote   NotificationType = "quote"
	NotificationTypeFollow  NotificationType = "follow"
)

type Notification struct {
	ID         string
	Type       NotificationType
	Item       *Post
	TargetID   string
	Actor      Author
	ActorRepo  string
	Branch     string
	ListID     string
	CommitHash string
	Timestamp  time.Time
	IsRead     bool
}

type notificationFilter struct {
	UnreadOnly bool
	Types      []NotificationType
	Limit      int
}
