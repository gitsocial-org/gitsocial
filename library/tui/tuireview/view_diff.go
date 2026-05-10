// view_diff.go - Files changed diff view for pull request review. Thin
// wrapper over tuicore.DiffViewCore: adds PR loading, feedback layer + nav,
// and the "c" (inline comment) keybinding.
package tuireview

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/tui/tuicore/diff"
)

// DiffView displays files changed in a pull request.
type DiffView struct {
	core       *tuicore.DiffViewCore
	pr         *review.PullRequest
	headCommit string
	diffError  string
	feedbacks  []review.Feedback
	userEmail  string
	showEmail  bool
}

// NewDiffView creates a new diff view.
func NewDiffView(workdir string) *DiffView {
	v := &DiffView{core: tuicore.NewDiffViewCore(workdir)}
	v.core.SetExtraKey(v.extraKey)
	return v
}

// SetSize sets the view dimensions.
func (v *DiffView) SetSize(w, h int) { v.core.SetSize(w, h) }

// IsInputActive returns true when search input is active.
func (v *DiffView) IsInputActive() bool { return v.core.IsInputActive() }

// Activate loads diff data for the pull request.
func (v *DiffView) Activate(state *tuicore.State) tea.Cmd {
	v.core.Reset()
	v.pr = nil
	v.headCommit = ""
	v.diffError = ""
	v.feedbacks = nil
	v.userEmail = state.UserEmail
	v.showEmail = state.ShowEmailOnCards
	workdir := v.core.Workdir()
	prID := state.Router.Location().Param("prID")
	commit := state.Router.Location().Param("commit")
	cacheDir := state.CacheDir
	return func() tea.Msg {
		if err := review.SyncWorkspaceToCache(workdir); err != nil {
			log.Debug("review sync before diff load failed", "error", err)
		}
		res := review.GetPR(prID)
		if !res.Success {
			return diffLoadedMsg{}
		}
		pr := res.Data
		diffCtx := review.ResolvePRDiff(workdir, cacheDir, &pr, commit)
		forkFetched := diffCtx.Workdir != workdir
		if diffCtx.Base == "" || diffCtx.Head == "" {
			return diffLoadedMsg{pr: &pr, diffError: diffCtx.Error}
		}
		diffs, _ := git.GetDiff(diffCtx.Workdir, diffCtx.Base, diffCtx.Head)
		stats, _ := git.GetDiffStats(diffCtx.Workdir, diffCtx.Base, diffCtx.Head)
		headCommit, _ := git.ReadRef(diffCtx.Workdir, diffCtx.Head)
		hash := extractHashFromID(pr.ID)
		var feedbacks []review.Feedback
		fbRes := review.GetFeedbackForPR(pr.Repository, hash, pr.Branch)
		if fbRes.Success {
			feedbacks = fbRes.Data
		}
		return diffLoadedMsg{pr: &pr, headCommit: headCommit, diffs: diffs, stats: stats, feedbacks: feedbacks, forkFetched: forkFetched}
	}
}

// Update handles messages.
func (v *DiffView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if m, ok := msg.(diffLoadedMsg); ok {
		v.pr = m.pr
		v.headCommit = m.headCommit
		v.feedbacks = m.feedbacks
		v.diffError = m.diffError
		v.core.SetLayers([]diff.Layer{newFeedbackLayer(v.feedbacks, v.userEmail, v.showEmail)})
		if m.diffError == "" && m.pr != nil {
			v.core.LoadDiffs(m.diffs, m.stats)
		}
		if m.forkFetched {
			return refreshCacheSize(state.CacheDir)
		}
		return nil
	}
	return v.core.Update(msg, state)
}

// extraKey handles the PR-specific "c" key: opens the inline comment form
// at the cursor's file/line.
func (v *DiffView) extraKey(key string, _ *tuicore.State) (bool, tea.Cmd) {
	if key != "c" {
		return false, nil
	}
	if v.pr == nil {
		return true, nil
	}
	a := v.core.CursorAnchor()
	path := v.core.FilePath(a.FileIdx)
	if path == "" || (a.OldLine == 0 && a.NewLine == 0) {
		return true, nil
	}
	prID := v.pr.ID
	head := v.headCommit
	return true, func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocReviewFeedbackInline(prID, path, a.OldLine, a.NewLine, head),
			Action:   tuicore.NavPush,
		}
	}
}

// Render renders the view.
func (v *DiffView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	var content string
	switch {
	case !v.core.Loaded() && v.pr == nil:
		content = "Loading diff..."
	case v.pr == nil:
		content = "Pull request not found"
	case v.diffError != "":
		content = tuicore.Dim.Render(v.diffError)
	case len(v.core.Diffs()) == 0:
		content = tuicore.Dim.Render("No file changes found")
	default:
		content = v.core.RenderContent()
	}
	footer := v.core.RenderFooter(state, tuicore.ReviewDiff, wrapper.ContentWidth())
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *DiffView) Title() string {
	if v.pr == nil {
		return "⑂  Files Changed"
	}
	s := v.core.Stats()
	return fmt.Sprintf("⑂  Files Changed · %s · %s", tuicore.TruncateToWidth(v.pr.Subject, 30), tuicore.RenderDiffStatsBadge(s.Added, s.Removed))
}

// Bindings returns keybindings for this view: the shared diff bindings
// plus the PR-specific inline comment key.
func (v *DiffView) Bindings() []tuicore.Binding {
	noop := func(_ *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	ctx := []tuicore.Context{tuicore.ReviewDiff}
	extras := []tuicore.Binding{
		{Key: "c", Label: "comment", Contexts: ctx, Handler: noop},
	}
	return append(v.core.SharedBindings(tuicore.ReviewDiff), extras...)
}

type diffLoadedMsg struct {
	pr          *review.PullRequest
	headCommit  string
	diffs       []git.FileDiff
	stats       git.DiffStats
	feedbacks   []review.Feedback
	forkFetched bool
	diffError   string
}
