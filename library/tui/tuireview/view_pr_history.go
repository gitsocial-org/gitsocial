// view_pr_history.go - Version history view for pull requests.
package tuireview

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/text"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/library/tui/tuiproposal"
)

// PRVersionItem wraps review.PRVersion to implement tuicore.VersionItem.
type PRVersionItem struct {
	Version     review.PRVersion
	ShowEmail   bool
	ProposalTag string
}

// GetID returns the version's commit ref, matching the IDs the history-diff
// loader emits via gitmsg.GetHistory so the diff route resolves the pair.
func (v PRVersionItem) GetID() string {
	return protocol.CreateRef(protocol.RefTypeCommit, v.Version.CommitHash, v.Version.RepoURL, v.Version.Branch)
}

// GetTimestamp returns the version's creation time.
func (v PRVersionItem) GetTimestamp() time.Time { return v.Version.Timestamp }

// GetEditOf returns empty since PRVersions are a flat list.
func (v PRVersionItem) GetEditOf() string { return "" }

// IsRetracted returns true if this version has been retracted.
func (v PRVersionItem) IsRetracted() bool { return v.Version.IsRetracted }

// AuthorDisplay returns the author name, optionally with email.
func (v PRVersionItem) AuthorDisplay(showEmail bool) string {
	name := v.Version.AuthorName
	if name == "" {
		name = "Anonymous"
	}
	if showEmail && v.Version.AuthorEmail != "" {
		name += " <" + v.Version.AuthorEmail + ">"
	}
	return name
}

// Ref returns the version's repo URL, commit hash, and branch.
func (v PRVersionItem) Ref() (string, string, string) {
	return v.Version.RepoURL, v.Version.CommitHash, v.Version.Branch
}

// IsOpenProposal reports whether this version is an open cross-repo proposal.
func (v PRVersionItem) IsOpenProposal() bool { return tuicore.IsOpenProposalTag(v.ProposalTag) }

// RenderListEntry renders a compact table row for this version.
func (v PRVersionItem) RenderListEntry(index, total int, label string, selected bool, width int) string {
	baseTip := v.Version.BaseTip
	if baseTip == "" {
		baseTip = "—"
	}
	headTip := v.Version.HeadTip
	if headTip == "" {
		headTip = "—"
	}
	stateStr := ""
	if v.Version.State != "" && v.Version.State != review.PRStateOpen {
		stateStr = "  " + string(v.Version.State)
	}
	header := fmt.Sprintf("#%d  %s  %s  %s  %s  %s%s",
		v.Version.Number, label, baseTip, headTip, v.AuthorDisplay(v.ShowEmail),
		v.Version.Timestamp.Format("2006-01-02 15:04"), stateStr)
	var b strings.Builder
	if selected {
		b.WriteString(tuicore.Highlight.Render("▶ " + header))
	} else {
		b.WriteString("  " + header)
	}
	b.WriteString(tuicore.RenderProposalTag(v.ProposalTag))
	b.WriteString("\n")
	if v.Version.IsRetracted {
		b.WriteString(tuicore.Dim.Render("    [deleted]"))
		b.WriteString("\n")
	}
	return b.String()
}

// reconstruct rebuilds the pull request at this version from its header fields.
func (v PRVersionItem) reconstruct() *review.PullRequest {
	repoURL, hash, branch := v.Ref()
	msg := &protocol.Message{Header: protocol.Header{Ext: "review", Fields: v.Version.Fields}}
	item := review.MessageToReviewItem(msg, repoURL, hash, branch)
	item.AuthorName = v.Version.AuthorName
	item.AuthorEmail = v.Version.AuthorEmail
	item.Timestamp = v.Version.Timestamp
	item.IsRetracted = v.Version.IsRetracted
	pr := review.ReviewItemToPullRequest(item)
	// Versions carry their subject/body pre-split; ReviewItemToPullRequest reads
	// them from an (empty) Content, so restore them from the version directly.
	pr.Subject = v.Version.Subject
	pr.Body = v.Version.Body
	// MessageToReviewItem doesn't carry labels, so restore them from the version header.
	pr.Labels = text.SplitCSV(v.Version.Fields["labels"])
	return &pr
}

// RenderDetail renders this version through the real PR hero card in version mode.
func (v PRVersionItem) RenderDetail(width int) string {
	lines := renderPRCard(v.reconstruct(), width, false, "", nil, prCardOptions{
		version:       true,
		versionAuthor: v.AuthorDisplay(v.ShowEmail),
		versionTime:   v.Version.Timestamp,
	})
	return strings.Join(lines, "\n")
}

// loadPRHistory fetches and wraps a PR's version history. GetPRVersions returns
// ASC (oldest first), but the picker's labels and diff navigation assume DESC
// (newest first) like every other history view, so the newest goes to index 0.
func loadPRHistory(ctx tuicore.HistoryLoadContext) ([]tuicore.VersionItem, error) {
	res := review.GetPRVersions(ctx.Ref, ctx.WorkspaceURL)
	if !res.Success {
		return nil, fmt.Errorf("%s", res.Error.Message)
	}
	items := make([]tuicore.VersionItem, len(res.Data))
	for i, version := range res.Data {
		items[len(res.Data)-1-i] = PRVersionItem{
			Version:     version,
			ShowEmail:   ctx.ShowEmail,
			ProposalTag: tuicore.ProposalTag(ctx.Owned, ctx.WorkspaceURL, version.RepoURL, version.CommitHash, version.Branch),
		}
	}
	return items, nil
}

// NewPRHistoryView creates the version-history view for a pull request.
func NewPRHistoryView(workdir string) *tuicore.HistoryView {
	return tuicore.NewHistoryView(workdir, tuicore.HistoryConfig{
		ParamName:  "prID",
		Context:    tuicore.ReviewPRHistory,
		TitleLabel: "Version History",
		Load:       loadPRHistory,
		DiffLoc:    tuicore.LocReviewPRHistoryDiff,
		Detail:     tuicore.LocReviewPRDetail,
		Accept:     tuiproposal.Accept,
		Decline:    tuiproposal.Decline,
		ExtraKeys: []tuicore.HistoryExtraKey{
			{Key: "i", Label: "interdiff", OnPress: openPRInterdiff},
			{Key: "p", Label: "push", Handler: pushPRHandler},
		},
	})
}

// openPRInterdiff navigates to the existing PR range-diff (interdiff) view.
func openPRInterdiff(_ *tuicore.HistoryView, state *tuicore.State) tea.Cmd {
	prID := state.Router.Location().Param("prID")
	return func() tea.Msg {
		return tuicore.NavigateMsg{Location: tuicore.LocReviewInterdiff(prID), Action: tuicore.NavPush}
	}
}

// pushPRHandler triggers a workspace push from the history footer.
func pushPRHandler(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
	if ctx.StartPush == nil {
		return false, nil
	}
	return true, ctx.StartPush()
}
