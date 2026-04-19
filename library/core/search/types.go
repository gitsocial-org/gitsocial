// types.go - Search types for cross-extension search
package search

import "time"

// Params configures a search query.
type Params struct {
	Query  string
	Author string
	Repo   string
	Type   string // "post", "issue", "pr", "release", etc.
	Hash   string
	After  *time.Time
	Before *time.Time
	Limit  int
	Offset int
	Scope  string // "timeline", "list:<id>", "repository:<url>", "repos:<csv>"
	Sort   string // "score", "date"

	// Extension-specific filters
	State      string // "open", "closed", "merged", "canceled" (pm/review)
	Labels     string // comma-separated labels to match (pm/review)
	Assignee   string // filter by assignee email (pm)
	Reviewer   string // filter by reviewer email (review)
	Milestone  string // filter by milestone name (pm)
	Sprint     string // filter by sprint name (pm)
	Draft      bool   // filter draft PRs only (review)
	Prerelease bool   // filter pre-releases only (release)
	Tag        string // filter by release tag (release)
	Base       string // filter by PR base branch (review)

	// Grouping
	GroupBy   string // field to group by: state, author, type, extension, repo, label, assignee, reviewer, milestone, base
	Top       int    // max items per group (0 = unlimited)
	CountOnly bool   // only return counts per group, no items
}

// Result holds search results with metadata.
type Result struct {
	Query           string       `json:"query"`
	Results         []ScoredItem `json:"results,omitempty"`
	Total           int          `json:"total"`
	TotalSearched   int          `json:"total_searched"`
	HasMore         bool         `json:"has_more,omitempty"`
	ExecutionTimeMs int64        `json:"execution_time_ms"`

	// Grouped output (populated when --group-by is used, Results will be nil)
	GroupBy string  `json:"group_by,omitempty"`
	Groups  []Group `json:"groups,omitempty"`
}

// Group is a single group within a grouped result.
type Group struct {
	Key   string        `json:"key"`
	Count int           `json:"count"`
	Items []GroupedItem `json:"items,omitempty"`
}

// GroupedItem is a compact item representation within a group.
type GroupedItem struct {
	Hash      string `json:"hash"`
	Author    string `json:"author,omitempty"`
	Subject   string `json:"subject"`
	Timestamp string `json:"timestamp"`
	State     string `json:"state,omitempty"`
	Labels    string `json:"labels,omitempty"`
	RepoURL   string `json:"repo_url,omitempty"`
}

// Item is an extension-agnostic search result from the database.
type Item struct {
	RepoURL     string    `json:"repo_url"`
	Hash        string    `json:"hash"`
	Branch      string    `json:"branch"`
	AuthorName  string    `json:"author_name"`
	AuthorEmail string    `json:"author_email"`
	Content     string    `json:"content"`
	Timestamp   time.Time `json:"timestamp"`
	Type        string    `json:"type"`
	Extension   string    `json:"extension"`
	IsVirtual   bool      `json:"is_virtual,omitempty"`
	IsStale     bool      `json:"is_stale,omitempty"`

	// Extension-specific fields (populated from LEFT JOIN)
	State      string `json:"state,omitempty"`
	Labels     string `json:"labels,omitempty"`
	Assignees  string `json:"assignees,omitempty"`
	Reviewers  string `json:"reviewers,omitempty"`
	Base       string `json:"base,omitempty"`
	Head       string `json:"head,omitempty"`
	Draft      bool   `json:"draft,omitempty"`
	Tag        string `json:"tag,omitempty"`
	Version    string `json:"version,omitempty"`
	Prerelease bool   `json:"prerelease,omitempty"`
	Due        string `json:"due,omitempty"`
	Comments   int    `json:"comments,omitempty"`

	// Internal fields for grouping (not serialized, populated by enrichForGrouping)
	groupState     string
	groupLabels    string
	groupAssignees string
	groupReviewers string
	groupBase      string
	groupMilestone string
}

// ScoredItem wraps an Item with a relevance score.
type ScoredItem struct {
	Item
	Score float64 `json:"score"`
}

// ExtFilter filters by existence in an extension table.
type ExtFilter struct {
	Table string // "pm_items", "review_items", "release_items", "social_items"
	Type  string // optional type within table
}
