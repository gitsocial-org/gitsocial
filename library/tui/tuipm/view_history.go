// view_history.go - Edit-history views for PM issues, milestones, and sprints.
package tuipm

import (
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/library/tui/tuiproposal"
)

// NewIssueHistoryView creates the edit-history view for a PM issue.
func NewIssueHistoryView(workdir string) *tuicore.HistoryView {
	return tuicore.NewHistoryView(workdir, tuicore.HistoryConfig{
		ParamName:  "issueID",
		Context:    tuicore.PMIssueHistory,
		TitleLabel: "History",
		Load:       loadIssueHistory,
		DiffLoc:    tuicore.LocPMIssueHistoryDiff,
		Detail:     tuicore.LocPMIssueDetail,
		Accept:     tuiproposal.Accept,
		Decline:    tuiproposal.Decline,
	})
}

// NewMilestoneHistoryView creates the edit-history view for a PM milestone.
func NewMilestoneHistoryView(workdir string) *tuicore.HistoryView {
	return tuicore.NewHistoryView(workdir, tuicore.HistoryConfig{
		ParamName:  "milestoneID",
		Context:    tuicore.PMMilestoneHistory,
		TitleLabel: "History",
		Load:       loadMilestoneHistory,
		DiffLoc:    tuicore.LocPMMilestoneHistoryDiff,
		Detail:     tuicore.LocPMMilestoneDetail,
		Accept:     tuiproposal.Accept,
		Decline:    tuiproposal.Decline,
	})
}

// NewSprintHistoryView creates the edit-history view for a PM sprint.
func NewSprintHistoryView(workdir string) *tuicore.HistoryView {
	return tuicore.NewHistoryView(workdir, tuicore.HistoryConfig{
		ParamName:  "sprintID",
		Context:    tuicore.PMSprintHistory,
		TitleLabel: "History",
		Load:       loadSprintHistory,
		DiffLoc:    tuicore.LocPMSprintHistoryDiff,
		Detail:     tuicore.LocPMSprintDetail,
		Accept:     tuiproposal.Accept,
		Decline:    tuiproposal.Decline,
	})
}
