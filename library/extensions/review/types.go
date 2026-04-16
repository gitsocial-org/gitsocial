// types.go - Review extension data types
package review

import (
	"time"

	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/result"
)

type Result[T any] = result.Result[T]

type ItemType string

const (
	ItemTypePullRequest ItemType = "pull-request"
	ItemTypeFeedback    ItemType = "feedback"
)

// Spec-defined field ordering per GITREVIEW.md
var prFieldOrder = []string{"state", "draft", "base", "base-tip", "head", "head-tip", "depends-on", "closes", "merge-base", "merge-head", "reviewers", "labels"}
var feedbackFieldOrder = []string{"pull-request", "commit", "file", "new-line", "new-line-end", "old-line", "old-line-end", "review-state", "suggestion"}

type PRState string

const (
	PRStateOpen   PRState = "open"
	PRStateMerged PRState = "merged"
	PRStateClosed PRState = "closed"
)

type ReviewState string

const (
	ReviewStateApproved         ReviewState = "approved"
	ReviewStateChangesRequested ReviewState = "changes-requested"
)

type MergeStrategy string

const (
	MergeStrategyFF     MergeStrategy = "ff"
	MergeStrategySquash MergeStrategy = "squash"
	MergeStrategyRebase MergeStrategy = "rebase"
	MergeStrategyMerge  MergeStrategy = "merge"
)

type Author struct {
	Name  string
	Email string
}

type Ref struct {
	RepoURL string
	Hash    string
	Branch  string
}

type PullRequest struct {
	ID             string
	Repository     string
	Branch         string
	Author         Author
	Timestamp      time.Time
	Subject        string
	Body           string
	State          PRState
	IsDraft        bool
	Base           string
	BaseTip        string
	Head           string
	HeadTip        string
	DependsOn      []string
	Closes         []string
	Reviewers      []string
	Labels         []string
	IsEdited       bool
	IsRetracted    bool
	IsUnpushed     bool
	Comments       int
	ReviewSummary  ReviewSummary
	MergeBase      string
	MergeHead      string
	MergedBy       *Author
	MergedAt       time.Time
	ClosedBy       *Author
	ClosedAt       time.Time
	OriginalAuthor *Author
	OriginalTime   time.Time
	Origin         *protocol.Origin
}

type Feedback struct {
	ID          string
	Repository  string
	Branch      string
	Author      Author
	Timestamp   time.Time
	Content     string
	PullRequest Ref
	Commit      string
	File        string
	OldLine     int
	NewLine     int
	OldLineEnd  int
	NewLineEnd  int
	ReviewState ReviewState
	Suggestion  bool
	IsEdited    bool
	IsRetracted bool
	Comments    int
}

type ReviewSummary struct {
	Approved         int
	ChangesRequested int
	Pending          int
	IsBlocked        bool
	IsApproved       bool
}
