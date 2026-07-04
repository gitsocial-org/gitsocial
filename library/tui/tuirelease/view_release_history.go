// view_release_history.go - Edit history view for releases.
package tuirelease

import (
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/library/tui/tuiproposal"
)

// NewReleaseHistoryView creates the edit-history view for a release.
func NewReleaseHistoryView(workdir string) *tuicore.HistoryView {
	return tuicore.NewHistoryView(workdir, tuicore.HistoryConfig{
		ParamName:  "releaseID",
		Context:    tuicore.ReleaseHistory,
		TitleLabel: "History",
		Load:       tuicore.MessageHistoryLoader("release"),
		DiffLoc:    tuicore.LocReleaseHistoryDiff,
		Detail:     tuicore.LocReleaseDetail,
		Accept:     tuiproposal.Accept,
		Decline:    tuiproposal.Decline,
	})
}
