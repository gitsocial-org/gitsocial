// types.go - Social extension data types
package social

import (
	"time"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/result"
)

// Result type aliases for internal use
type Result[T any] = result.Result[T]

func Success[T any](data T) Result[T]               { return result.Ok(data) }
func Failure[T any](code, message string) Result[T] { return result.Err[T](code, message) }
func FailureWithDetails[T any](code, message string, details interface{}) Result[T] {
	return result.ErrWithDetails[T](code, message, details)
}

type PostSource string

const (
	PostSourceExplicit PostSource = "explicit"
	PostSourceImplicit PostSource = "implicit"
)

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
	CommitURL          string
	TotalReposts       int
	IsEmpty            bool
	IsUnpushed         bool
	IsOrigin           bool
	IsWorkspacePost    bool
	FollowsYou         bool
	IsNotificationRead bool
	Badge              string
	UserEmail          string // Current user's email for own-post detection in rendering
	ShowEmail          bool   // Whether to show email in card header
}

type Post struct {
	ID              string
	Repository      string
	Branch          string
	Author          Author
	Timestamp       time.Time
	Content         string
	Type            PostType
	Source          PostSource
	CleanContent    string
	OriginalPostID  string
	ParentCommentID string
	EditOf          string
	IsRetracted     bool
	IsEdited        bool
	Depth           int
	Interactions    Interactions
	Remote          string
	IsVirtual       bool
	IsStale         bool
	IsWorkspacePost bool
	Display         Display
	// OriginalExtension and OriginalType are populated for comments from the GitMsg-Ref header.
	// Used for cross-extension navigation (e.g., social comment on PM issue).
	OriginalExtension string
	OriginalType      string
	// HeaderExt, HeaderType, HeaderState are the item's own ext/type/state from the GitMsg header.
	// Used for routing to correct detail views (e.g., "pm"/"issue" for PM issues).
	HeaderExt   string
	HeaderType  string
	HeaderState string
	Origin      *protocol.Origin
	Raw         struct {
		Commit git.Commit
		GitMsg *protocol.Message
	}
}

type List struct {
	ID                string
	Name              string
	Version           string
	Repositories      []string
	Source            string
	IsUnpushed        bool
	IsFollowedLocally bool
}

type RepositoryType string

const (
	RepositoryTypeWorkspace RepositoryType = "workspace"
	RepositoryTypeOther     RepositoryType = "other"
)

type Repository struct {
	ID              string
	URL             string
	Name            string
	Path            string
	Branch          string
	DefaultBranch   string
	Type            RepositoryType
	SocialEnabled   bool
	FollowedAt      *time.Time
	LastFetchTime   *time.Time
	LastSyncTime    *time.Time
	FetchedRanges   []FetchedRange
	RemoteName      string
	Lists           []string
	HasOriginRemote bool
	OriginURL       string
}

type FetchedRange struct {
	Start string
	End   string
}

type LogEntryType string

const (
	LogTypePost               LogEntryType = "post"
	LogTypeComment            LogEntryType = "comment"
	LogTypeRepost             LogEntryType = "repost"
	LogTypeQuote              LogEntryType = "quote"
	LogTypeListCreate         LogEntryType = "list-create"
	LogTypeListDelete         LogEntryType = "list-delete"
	LogTypeRepositoryFollow   LogEntryType = "repository-follow"
	LogTypeRepositoryUnfollow LogEntryType = "repository-unfollow"
	LogTypeConfig             LogEntryType = "config"
	LogTypeMetadata           LogEntryType = "metadata"
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
	NotificationTypeComment NotificationType = "comment"
	NotificationTypeRepost  NotificationType = "repost"
	NotificationTypeQuote   NotificationType = "quote"
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

type NotificationFilter struct {
	UnreadOnly bool
	Types      []NotificationType
	Limit      int
}
