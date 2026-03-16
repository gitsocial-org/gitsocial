// util_messages.go - Social extension message types for async operations
package tuisocial

import (
	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/extensions/social"
)

// ThreadLoadedMsg is sent when thread data is loaded
type ThreadLoadedMsg struct {
	Posts []social.Post
	Err   error
}

// RepositoryLoadedMsg is sent when repository posts are loaded
type RepositoryLoadedMsg struct {
	Posts   []social.Post
	HasMore bool
	Append  bool
	Total   int
	Err     error
}

// ListsLoadedMsg is sent when lists are loaded
type ListsLoadedMsg struct {
	Lists []social.List
	Err   error
}

// ListPostsLoadedMsg is sent when posts from a list are loaded
type ListPostsLoadedMsg struct {
	ListID  string
	List    *social.List
	Posts   []social.Post
	HasMore bool
	Append  bool
	Total   int
	Err     error
}

// ListCreatedMsg is sent when a list is created
type ListCreatedMsg struct {
	List social.List
	Err  error
}

// ListDeletedMsg is sent when a list is deleted
type ListDeletedMsg struct {
	ListID string
	Err    error
}

// RepoAddedMsg is sent when a repo is added to a list
type RepoAddedMsg struct {
	ListID   string
	ListName string
	RepoURL  string
	Err      error
}

// RepoRemovedMsg is sent when a repo is removed from a list
type RepoRemovedMsg struct {
	ListID  string
	RepoURL string
	Err     error
}

// HistoryLoadedMsg is sent when edit history has been loaded
type HistoryLoadedMsg struct {
	Versions []social.Post
	Original social.Post
	Err      error
}

// PostCreatedMsg is sent when a post is created
type PostCreatedMsg struct {
	Post social.Post
}

// CommentCreatedMsg is sent when a comment is created
type CommentCreatedMsg struct {
	Post social.Post
}

// RepostCreatedMsg is sent when a repost/quote is created
type RepostCreatedMsg struct {
	Post social.Post
}

// PostEditedMsg is sent when a post has been edited
type PostEditedMsg struct {
	Post social.Post
	Err  error
}

// RetractStartedMsg is sent when retraction begins
type RetractStartedMsg struct{}

// PostRetractedMsg is sent when a post has been retracted
type PostRetractedMsg struct {
	PostID string
	Err    error
}

// TimelineLoadedMsg is sent when timeline posts are loaded (initial or paginated)
type TimelineLoadedMsg struct {
	Posts   []social.Post
	HasMore bool
	Append  bool
	Total   int // total timeline items (only set on initial load)
	Err     error
}

// FetchCompletedMsg is sent when fetch completes
type FetchCompletedMsg struct {
	Stats social.FetchStats
	Err   error
}

// PushCompletedMsg is sent when push completes
type PushCompletedMsg struct {
	Commits int
	Refs    int
	Err     error
}

// RepositoryFetchedMsg is sent when unfollowed repo posts are fetched
type RepositoryFetchedMsg struct {
	Posts  int
	Months []string // Fetched months (e.g., ["2026-01", "2025-12"])
	Err    error
}

// RepoFetchedAfterAddMsg is sent when a newly added repo has been fetched
type RepoFetchedAfterAddMsg struct {
	RepoURL string
	Posts   int
	Err     error
}

// RepoListsLoadedMsg is sent when lists are loaded from cache
type RepoListsLoadedMsg struct {
	Lists []cache.ExternalRepoList
	Err   error
}
