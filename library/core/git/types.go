// types.go - Git data types for commits, remotes, and configuration
package git

import "time"

type Commit struct {
	Hash      string
	Message   string
	Author    string
	Email     string
	Timestamp time.Time
	Refname   string
}

type CommitOptions struct {
	Message    string
	AllowEmpty bool
	Parent     string
}

type ExecResult struct {
	Stdout string
	Stderr string
}

type FetchOptions struct {
	ShallowSince string
	Depth        int
	Branch       string
	Jobs         int
}

type Remote struct {
	Name string
	URL  string
}

// Diff types for unified/side-by-side diff rendering

type DiffStatus string

const (
	DiffStatusAdded    DiffStatus = "added"
	DiffStatusModified DiffStatus = "modified"
	DiffStatusDeleted  DiffStatus = "deleted"
	DiffStatusRenamed  DiffStatus = "renamed"
)

type LineType int

const (
	LineContext LineType = 0
	LineAdded   LineType = 1
	LineRemoved LineType = 2
)

type DiffLine struct {
	Type    LineType
	Content string
	OldNum  int
	NewNum  int
}

type Hunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Header   string
	Lines    []DiffLine
}

type FileDiff struct {
	OldPath string
	NewPath string
	Status  DiffStatus
	Hunks   []Hunk
	Binary  bool
}

type DiffStats struct {
	Files   int
	Added   int
	Removed int
}
