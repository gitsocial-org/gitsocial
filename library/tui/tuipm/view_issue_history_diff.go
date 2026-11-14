// view_issue_history_diff.go - History diff view for PM issues
package tuipm

import (
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// newIssueHistoryDiffView creates a HistoryDiffView wired to issue history.
func newIssueHistoryDiffView(workdir string) *tuicore.HistoryDiffView {
	return tuicore.NewHistoryDiffView(workdir, tuicore.HistoryDiffConfig{
		Context:   tuicore.PMIssueHistoryDiff,
		TitleIcon: protocol.TypeGlyph("issue", ""),
		Title:     "Issue Diff",
		Load:      loadPMHistoryVersionsKey("issueID"),
	})
}
