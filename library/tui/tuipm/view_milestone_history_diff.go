// view_milestone_history_diff.go - History diff view for PM milestones
package tuipm

import (
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// NewMilestoneHistoryDiffView creates a HistoryDiffView wired to milestone history.
func NewMilestoneHistoryDiffView(workdir string) *tuicore.HistoryDiffView {
	return tuicore.NewHistoryDiffView(workdir, tuicore.HistoryDiffConfig{
		Context:   tuicore.PMMilestoneHistoryDiff,
		TitleIcon: "◇",
		Title:     "Milestone Diff",
		Load:      loadPMHistoryVersionsKey("milestoneID"),
	})
}
