// view_diff.go - Generic commit diff view for workdir commits. Thin
// wrapper over DiffViewCore: adds commit loading, title, bindings.
package tuicore

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

func init() {
	RegisterViewMeta(ViewMeta{Path: "/diff", Context: CommitDiff, Title: "Commit Diff", Icon: "±"})
}

// CommitDiffView displays the diff for a single commit.
type CommitDiffView struct {
	core    *DiffViewCore
	subject string
	loadErr error
}

// commitDiffLoadedMsg carries loaded diff data.
type commitDiffLoadedMsg struct {
	subject string
	diffs   []git.FileDiff
	stats   git.DiffStats
	err     error
}

// NewCommitDiffView creates a new generic commit diff view.
func NewCommitDiffView(workdir string) *CommitDiffView {
	return &CommitDiffView{core: NewDiffViewCore(workdir)}
}

// SetSize sets the view dimensions.
func (v *CommitDiffView) SetSize(w, h int) { v.core.SetSize(w, h) }

// IsInputActive returns true when search input is active.
func (v *CommitDiffView) IsInputActive() bool { return v.core.IsInputActive() }

// Activate loads diff data for the commit.
func (v *CommitDiffView) Activate(state *State) tea.Cmd {
	v.core.Reset()
	v.subject = ""
	v.loadErr = nil
	commit := state.Router.Location().Param("commit")
	workdir := v.core.Workdir()
	return func() tea.Msg {
		if commit == "" {
			return commitDiffLoadedMsg{err: fmt.Errorf("no commit specified")}
		}
		msg, _ := git.GetCommitMessage(workdir, commit)
		subject := strings.SplitN(strings.TrimSpace(msg), "\n", 2)[0]
		diffs, err := git.GetDiff(workdir, commit+"^", commit)
		if err != nil {
			return commitDiffLoadedMsg{err: fmt.Errorf("diff: %w", err)}
		}
		stats, _ := git.GetDiffStats(workdir, commit+"^", commit)
		return commitDiffLoadedMsg{subject: subject, diffs: diffs, stats: stats}
	}
}

// Update handles messages.
func (v *CommitDiffView) Update(msg tea.Msg, state *State) tea.Cmd {
	if m, ok := msg.(commitDiffLoadedMsg); ok {
		v.subject = m.subject
		v.loadErr = m.err
		if m.err == nil {
			v.core.LoadDiffs(m.diffs, m.stats)
		}
		return nil
	}
	return v.core.Update(msg, state)
}

// Render renders the view.
func (v *CommitDiffView) Render(state *State) string {
	wrapper := NewViewWrapper(state)
	var content string
	switch {
	case !v.core.Loaded() && v.loadErr == nil:
		content = "Loading diff..."
	case v.loadErr != nil:
		content = Dim.Render(fmt.Sprintf("Error: %s", v.loadErr.Error()))
	case len(v.core.Diffs()) == 0:
		content = Dim.Render("No file changes found")
	default:
		content = v.core.RenderContent()
	}
	footer := v.core.RenderFooter(state, CommitDiff, wrapper.ContentWidth())
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *CommitDiffView) Title() string {
	if v.subject == "" {
		return "±  Commit Diff"
	}
	s := v.core.Stats()
	return fmt.Sprintf("±  %s · %s", TruncateToWidth(v.subject, 40), RenderDiffStatsBadge(s.Added, s.Removed))
}

// Bindings returns keybindings for this view.
func (v *CommitDiffView) Bindings() []Binding {
	return v.core.SharedBindings(CommitDiff)
}
