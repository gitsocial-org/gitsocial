// view_sprint_history_diff.go - History diff view for PM sprints
package tuipm

import (
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// NewSprintHistoryDiffView creates a HistoryDiffView wired to sprint history.
func NewSprintHistoryDiffView(workdir string) *tuicore.HistoryDiffView {
	return tuicore.NewHistoryDiffView(workdir, tuicore.HistoryDiffConfig{
		Context:   tuicore.PMSprintHistoryDiff,
		TitleIcon: "⟳",
		Title:     "Sprint Diff",
		Load:      loadPMHistoryVersionsKey("sprintID"),
	})
}
