// util_messages.go - PM extension TUI messages
package tuipm

import "github.com/gitsocial-org/gitsocial/library/extensions/pm"

// issueCreatedMsg signals that an issue was created.
type issueCreatedMsg struct {
	Issue pm.Issue
	Err   error
}

// issueRetractedMsg signals that an issue was retracted (Proposed = cross-repo proposal).
type issueRetractedMsg struct {
	ID       string
	Proposed bool
	Err      error
}

// issueClosedMsg signals that an issue close was committed. Proposed is true
// when the issue is owned by another repo, so the close is a cross-repo
// proposal (inert until the owner accepts) rather than an applied state change.
type issueClosedMsg struct {
	ID       string
	Proposed bool
	Err      error
}

// milestoneClosedMsg signals a milestone close (Proposed = cross-repo proposal).
type milestoneClosedMsg struct {
	ID       string
	Proposed bool
	Err      error
}

// sprintCompletedMsg signals a sprint completion (Proposed = cross-repo proposal).
type sprintCompletedMsg struct {
	ID       string
	Proposed bool
	Err      error
}

// milestoneRetractedMsg signals that a milestone was retracted (Proposed = cross-repo proposal).
type milestoneRetractedMsg struct {
	ID       string
	Proposed bool
	Err      error
}

// sprintRetractedMsg signals that a sprint was retracted (Proposed = cross-repo proposal).
type sprintRetractedMsg struct {
	ID       string
	Proposed bool
	Err      error
}
