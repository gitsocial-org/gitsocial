// version_item_issue.go - Issue-specific VersionItem rendering the real issue hero card.
package tuipm

import (
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// IssueVersionItem reuses the generic list rendering of MessageVersionItem but
// reconstructs the issue at that version to render the real hero card in detail.
type IssueVersionItem struct {
	tuicore.MessageVersionItem
	workdir string
}

// reconstruct rebuilds the issue (and resolves parent/milestone/sprint) at this version.
func (i IssueVersionItem) reconstruct() (*pm.Issue, *pm.Milestone, *pm.Sprint, *pm.Issue) {
	repoURL, hash, branch := i.Ref()
	msg := &protocol.Message{Header: protocol.Header{Ext: "pm", Fields: i.Version.Fields}}
	item := pm.MessageToPMItem(msg, repoURL, hash, branch)
	item.Content = i.Version.Content
	item.AuthorName = i.Version.AuthorName
	item.AuthorEmail = i.Version.AuthorEmail
	item.Timestamp = i.Version.Timestamp
	item.IsRetracted = i.Version.IsRetracted
	issue := pm.PMItemToIssue(item)
	// Links come from the versioned header fields, not the live pm_links table
	// (which holds current state plus reverse edges the message never carried).
	issue.Blocks = pm.ParseRefList(i.Version.Fields["blocks"], repoURL, branch)
	issue.BlockedBy = pm.ParseRefList(i.Version.Fields["blocked-by"], repoURL, branch)
	issue.Related = pm.ParseRefList(i.Version.Fields["related"], repoURL, branch)
	var milestone *pm.Milestone
	if issue.Milestone != nil {
		if it, err := pm.GetPMItem(issue.Milestone.RepoURL, issue.Milestone.Hash, issue.Milestone.Branch); err == nil {
			m := pm.PMItemToMilestone(*it)
			milestone = &m
		}
	}
	var sprint *pm.Sprint
	if issue.Sprint != nil {
		if it, err := pm.GetPMItem(issue.Sprint.RepoURL, issue.Sprint.Hash, issue.Sprint.Branch); err == nil {
			s := pm.PMItemToSprint(*it)
			sprint = &s
		}
	}
	// Parent/root are versioned header fields; resolve to the current issue for
	// a friendly subject (a direct child's root IS its parent, GITPM.md §1.7).
	parent := resolveParentIssue(&issue)
	return &issue, milestone, sprint, parent
}

// RenderDetail renders this version through the real issue hero card in version mode.
func (i IssueVersionItem) RenderDetail(width int) string {
	issue, milestone, sprint, parent := i.reconstruct()
	lines := renderIssueCard(issue, milestone, sprint, parent, buildContributorNameMap(i.workdir), width, false, "", nil, issueCardOptions{
		version:       true,
		versionAuthor: i.AuthorDisplay(i.ShowEmail),
		versionTime:   i.Version.Timestamp,
	})
	return strings.Join(lines, "\n")
}

// loadIssueHistory fetches the issue edit history and wraps each version so the
// detail render reconstructs the real issue hero card (list entries stay generic).
func loadIssueHistory(ctx tuicore.HistoryLoadContext) ([]tuicore.VersionItem, error) {
	return tuicore.LoadMessageHistory(ctx, "pm", func(base tuicore.MessageVersionItem) tuicore.VersionItem {
		return IssueVersionItem{MessageVersionItem: base, workdir: ctx.Workdir}
	})
}
