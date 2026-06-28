// messages.go - PM extension TUI messages
package tuipm

import "github.com/gitsocial-org/gitsocial/library/extensions/pm"

// IssueCreatedMsg signals that an issue was created.
type IssueCreatedMsg struct {
	Issue pm.Issue
	Err   error
}

// IssueRetractedMsg signals that an issue was retracted (Proposed = cross-repo proposal).
type IssueRetractedMsg struct {
	ID       string
	Proposed bool
	Err      error
}

// IssueClosedMsg signals that an issue close was committed. Proposed is true
// when the issue is owned by another repo, so the close is a cross-repo
// proposal (inert until the owner accepts) rather than an applied state change.
type IssueClosedMsg struct {
	ID       string
	Proposed bool
	Err      error
}

// MilestoneClosedMsg signals a milestone close (Proposed = cross-repo proposal).
type MilestoneClosedMsg struct {
	ID       string
	Proposed bool
	Err      error
}

// SprintCompletedMsg signals a sprint completion (Proposed = cross-repo proposal).
type SprintCompletedMsg struct {
	ID       string
	Proposed bool
	Err      error
}

// MilestoneRetractedMsg signals that a milestone was retracted (Proposed = cross-repo proposal).
type MilestoneRetractedMsg struct {
	ID       string
	Proposed bool
	Err      error
}

// SprintRetractedMsg signals that a sprint was retracted (Proposed = cross-repo proposal).
type SprintRetractedMsg struct {
	ID       string
	Proposed bool
	Err      error
}
