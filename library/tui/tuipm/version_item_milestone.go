// version_item_milestone.go - Milestone-specific VersionItem rendering the real milestone hero card.
package tuipm

import (
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// MilestoneVersionItem reuses the generic list rendering of MessageVersionItem
// but reconstructs the milestone at that version to render the real hero card.
type MilestoneVersionItem struct {
	tuicore.MessageVersionItem
}

// reconstruct rebuilds the milestone at this version from its header fields.
func (m MilestoneVersionItem) reconstruct() *pm.Milestone {
	repoURL, hash, branch := m.Ref()
	msg := &protocol.Message{Header: protocol.Header{Ext: "pm", Fields: m.Version.Fields}}
	item := pm.MessageToPMItem(msg, repoURL, hash, branch)
	item.Content = m.Version.Content
	item.AuthorName = m.Version.AuthorName
	item.AuthorEmail = m.Version.AuthorEmail
	item.Timestamp = m.Version.Timestamp
	item.IsRetracted = m.Version.IsRetracted
	ms := pm.PMItemToMilestone(item)
	return &ms
}

// RenderDetail renders this version through the real milestone hero card in version mode.
func (m MilestoneVersionItem) RenderDetail(width int) string {
	lines := renderMilestoneCard(m.reconstruct(), width, false, "", nil, milestoneCardOptions{
		version:       true,
		versionAuthor: m.AuthorDisplay(m.ShowEmail),
		versionTime:   m.Version.Timestamp,
	})
	return strings.Join(lines, "\n")
}

// loadMilestoneHistory fetches the milestone edit history and wraps each version
// so the detail render reconstructs the real milestone hero card.
func loadMilestoneHistory(ctx tuicore.HistoryLoadContext) ([]tuicore.VersionItem, error) {
	return tuicore.LoadMessageHistory(ctx, "pm", func(base tuicore.MessageVersionItem) tuicore.VersionItem {
		return MilestoneVersionItem{MessageVersionItem: base}
	})
}
