// view_release_history.go - Edit history view for releases.
package tuirelease

import (
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/library/tui/tuiproposal"
)

// newReleaseHistoryView creates the edit-history view for a release.
func newReleaseHistoryView(workdir string) *tuicore.HistoryView {
	return tuicore.NewHistoryView(workdir, tuicore.HistoryConfig{
		ParamName:  "releaseID",
		Context:    tuicore.ReleaseHistory,
		TitleLabel: "History",
		Load:       loadReleaseHistory,
		DiffLoc:    tuicore.LocReleaseHistoryDiff,
		Detail:     tuicore.LocReleaseDetail,
		Accept:     tuiproposal.Accept,
		Decline:    tuiproposal.Decline,
	})
}
