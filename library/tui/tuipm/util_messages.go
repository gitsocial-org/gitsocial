// messages.go - PM extension TUI messages
package tuipm

import "github.com/gitsocial-org/gitsocial/extensions/pm"

// IssueCreatedMsg signals that an issue was created.
type IssueCreatedMsg struct {
	Issue pm.Issue
	Err   error
}

// IssueRetractedMsg signals that an issue was retracted.
type IssueRetractedMsg struct {
	ID  string
	Err error
}

// MilestoneRetractedMsg signals that a milestone was retracted.
type MilestoneRetractedMsg struct {
	ID  string
	Err error
}

// SprintRetractedMsg signals that a sprint was retracted.
type SprintRetractedMsg struct {
	ID  string
	Err error
}
