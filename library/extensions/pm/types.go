// types.go - PM extension data types
package pm

import (
	"time"

	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/result"
)

type Result[T any] = result.Result[T]

type ItemType string

const (
	ItemTypeIssue     ItemType = "issue"
	ItemTypeMilestone ItemType = "milestone"
	ItemTypeSprint    ItemType = "sprint"
)

// Spec-defined field ordering per GITPM.md
var issueFieldOrder = []string{"state", "assignees", "due", "milestone", "sprint", "parent", "root", "blocks", "blocked-by", "related", "labels"}
var milestoneFieldOrder = []string{"state", "due"}
var sprintFieldOrder = []string{"state", "start", "end"}

type State string

const (
	StateOpen      State = "open"
	StateClosed    State = "closed"
	StateCancelled State = "canceled"
)

type SprintState string

const (
	SprintStatePlanned   SprintState = "planned"
	SprintStateActive    SprintState = "active"
	SprintStateCompleted SprintState = "completed"
	SprintStateCancelled SprintState = "canceled"
)

type Author struct {
	Name  string
	Email string
}

type Label struct {
	Scope string
	Value string
}

type IssueRef struct {
	RepoURL string
	Hash    string
	Branch  string
}

type Issue struct {
	ID          string
	Repository  string
	Branch      string
	Author      Author
	Timestamp   time.Time
	Subject     string
	Body        string
	State       State
	Assignees   []string
	Due         *time.Time
	Milestone   *IssueRef
	Sprint      *IssueRef
	Parent      *IssueRef
	Root        *IssueRef
	Blocks      []IssueRef
	BlockedBy   []IssueRef
	Related     []IssueRef
	Labels      []Label
	IsEdited    bool
	IsRetracted bool
	IsUnpushed  bool
	Comments    int
	Origin      *protocol.Origin
}

type Milestone struct {
	ID          string
	Repository  string
	Branch      string
	Author      Author
	Timestamp   time.Time
	Title       string
	Body        string
	State       State
	Due         *time.Time
	IsEdited    bool
	IsRetracted bool
	IsUnpushed  bool
	IssueCount  int
	ClosedCount int
	Origin      *protocol.Origin
}

type Sprint struct {
	ID          string
	Repository  string
	Branch      string
	Author      Author
	Timestamp   time.Time
	Title       string
	Body        string
	State       SprintState
	Start       time.Time
	End         time.Time
	IsEdited    bool
	IsRetracted bool
	IsUnpushed  bool
	IssueCount  int
	ClosedCount int
	Origin      *protocol.Origin
}

type BoardColumn struct {
	Name   string
	Label  string
	WIP    *int
	Issues []Issue
}

type LinkType string

const (
	LinkTypeBlocks    LinkType = "blocks"
	LinkTypeBlockedBy LinkType = "blocked-by"
	LinkTypeRelated   LinkType = "related"
)

type Link struct {
	From IssueRef
	To   IssueRef
	Type LinkType
}
