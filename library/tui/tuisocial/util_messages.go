// util_messages.go - Social extension message types for async operations
package tuisocial

import (
	"github.com/gitsocial-org/gitsocial/library/client"
	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

// threadLoadedMsg is sent when thread data is loaded
type threadLoadedMsg struct {
	Posts []social.Post
	Err   error
}

// repositoryLoadedMsg is sent when repository posts are loaded
type repositoryLoadedMsg struct {
	Posts   []social.Post
	HasMore bool
	Append  bool
	Total   int
	Err     error
}

// repositoryCountLoadedMsg is sent when the repository total count finishes
// loading. Sent independently of repositoryLoadedMsg because COUNT(*) over
// huge repos (e.g. linux kernel) can take many seconds.
type repositoryCountLoadedMsg struct {
	Total int
}

// ListsLoadedMsg is sent when lists are loaded
type ListsLoadedMsg struct {
	Lists []social.List
	Err   error
}

// listPostsLoadedMsg is sent when posts from a list are loaded
type listPostsLoadedMsg struct {
	ListID  string
	List    *social.List
	Posts   []social.Post
	HasMore bool
	Append  bool
	Total   int
	Err     error
}

// listCreatedMsg is sent when a list is created
type listCreatedMsg struct {
	List social.List
	Err  error
}

// listDeletedMsg is sent when a list is deleted
type listDeletedMsg struct {
	ListID string
	Err    error
}

// repoAddedMsg is sent when a repo is added to a list
type repoAddedMsg struct {
	ListID   string
	ListName string
	RepoURL  string
	Err      error
}

// repoRemovedMsg is sent when a repo is removed from a list
type repoRemovedMsg struct {
	ListID  string
	RepoURL string
	Err     error
}

// commentCreatedMsg is sent when a comment is created
type commentCreatedMsg struct {
	Post social.Post
}

// retractStartedMsg is sent when retraction begins
type retractStartedMsg struct{}

// postRetractedMsg is sent when a post has been retracted
type postRetractedMsg struct {
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

// TimelineCountLoadedMsg is sent when the timeline total count finishes
// loading. Sent independently of TimelineLoadedMsg to keep the page render
// off the COUNT path.
type TimelineCountLoadedMsg struct {
	Total int
}

// listPostsCountLoadedMsg is sent when a list's total count finishes loading.
// ListID lets the receiving view ignore counts for stale list selections.
type listPostsCountLoadedMsg struct {
	ListID string
	Total  int
}

// FetchCompletedMsg is sent when fetch completes
type FetchCompletedMsg struct {
	Stats fetch.Stats
	Err   error
	// Breakdown is the per-extension count of newly cached items this fetch
	// (keys: social, pm, review, release, memo, code), used for the summary toast.
	Breakdown map[string]int
	// Auto is true when the periodic auto-fetch timer started this fetch.
	Auto bool
}

// PushCompletedMsg carries one result per remote in push order, and Err names the failures.
type PushCompletedMsg struct {
	Results []client.Result
	Err     error
}

// repositoryFetchedMsg is sent when unfollowed repo posts are fetched
type repositoryFetchedMsg struct {
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

// repoListsLoadedMsg is sent when lists are loaded from cache
type repoListsLoadedMsg struct {
	Lists []cache.ExternalRepoList
	Err   error
}
