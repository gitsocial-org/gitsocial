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
}

// Result holds search results with metadata.
type Result struct {
	Query           string       `json:"query"`
	Results         []ScoredItem `json:"results"`
	Total           int          `json:"total"`
	TotalSearched   int          `json:"total_searched"`
	HasMore         bool         `json:"has_more"`
	ExecutionTimeMs int64        `json:"execution_time_ms"`
}

// Item is an extension-agnostic search result from the database.
type Item struct {
	RepoURL     string
	Hash        string
	Branch      string
	AuthorName  string
	AuthorEmail string
	Content     string // resolved message body (raw, including headers)
	Timestamp   time.Time
	Type        string // "post", "comment", "issue", "pull-request", "release", etc.
	Extension   string // "social", "pm", "review", "release"
	IsVirtual   bool
	IsStale     bool
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
