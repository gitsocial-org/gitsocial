// version_item_sprint.go - Sprint-specific VersionItem rendering the real sprint hero card.
package tuipm

import (
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// SprintVersionItem reuses the generic list rendering of MessageVersionItem but
// reconstructs the sprint at that version to render the real hero card.
type SprintVersionItem struct {
	tuicore.MessageVersionItem
}

// reconstruct rebuilds the sprint at this version from its header fields.
func (s SprintVersionItem) reconstruct() *pm.Sprint {
	repoURL, hash, branch := s.Ref()
	msg := &protocol.Message{Header: protocol.Header{Ext: "pm", Fields: s.Version.Fields}}
	item := pm.MessageToPMItem(msg, repoURL, hash, branch)
	item.Content = s.Version.Content
	item.AuthorName = s.Version.AuthorName
	item.AuthorEmail = s.Version.AuthorEmail
	item.Timestamp = s.Version.Timestamp
	item.IsRetracted = s.Version.IsRetracted
	sprint := pm.PMItemToSprint(item)
	return &sprint
}

// RenderDetail renders this version through the real sprint hero card in version mode.
func (s SprintVersionItem) RenderDetail(width int) string {
	lines := renderSprintCard(s.reconstruct(), width, false, "", nil, sprintCardOptions{
		version:       true,
		versionAuthor: s.AuthorDisplay(s.ShowEmail),
		versionTime:   s.Version.Timestamp,
	})
	return strings.Join(lines, "\n")
}

// loadSprintHistory fetches the sprint edit history and wraps each version so the
// detail render reconstructs the real sprint hero card.
func loadSprintHistory(ctx tuicore.HistoryLoadContext) ([]tuicore.VersionItem, error) {
	return tuicore.LoadMessageHistory(ctx, "pm", func(base tuicore.MessageVersionItem) tuicore.VersionItem {
		return SprintVersionItem{MessageVersionItem: base}
	})
}
