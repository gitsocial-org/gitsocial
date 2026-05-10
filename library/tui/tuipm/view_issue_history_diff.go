// view_issue_history_diff.go - History diff view for PM issues
package tuipm

import (
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// NewIssueHistoryDiffView creates a HistoryDiffView wired to issue history.
func NewIssueHistoryDiffView(workdir string) *tuicore.HistoryDiffView {
	return tuicore.NewHistoryDiffView(workdir, tuicore.HistoryDiffConfig{
		Context:   tuicore.PMIssueHistoryDiff,
		TitleIcon: "○",
		Title:     "Issue Diff",
		Load:      loadPMHistoryVersionsKey("issueID"),
	})
}
