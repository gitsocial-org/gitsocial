// version_item_release.go - Release-specific VersionItem rendering the real release hero card.
package tuirelease

import (
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/release"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// ReleaseVersionItem reuses the generic list rendering of MessageVersionItem but
// reconstructs the release at that version to render the real hero card in detail.
type ReleaseVersionItem struct {
	tuicore.MessageVersionItem
}

// reconstruct rebuilds the release at this version from its header fields.
func (r ReleaseVersionItem) reconstruct() *release.Release {
	repoURL, hash, branch := r.Ref()
	msg := &protocol.Message{Header: protocol.Header{Ext: "release", Fields: r.Version.Fields}}
	item := release.MessageToReleaseItem(msg, repoURL, hash, branch)
	item.Content = r.Version.Content
	item.AuthorName = r.Version.AuthorName
	item.AuthorEmail = r.Version.AuthorEmail
	item.Timestamp = r.Version.Timestamp
	item.IsRetracted = r.Version.IsRetracted
	rel := release.ReleaseItemToRelease(item)
	// MessageToReleaseItem doesn't carry labels, so restore them from the version.
	rel.Labels = r.Version.Labels
	return &rel
}

// RenderDetail renders this version through the real release hero card in version mode.
func (r ReleaseVersionItem) RenderDetail(width int) string {
	lines := renderReleaseCard(r.reconstruct(), width, false, "", nil, releaseCardOptions{
		version:       true,
		versionAuthor: r.AuthorDisplay(r.ShowEmail),
		versionTime:   r.Version.Timestamp,
	})
	return strings.Join(lines, "\n")
}

// loadReleaseHistory fetches the release edit history and wraps each version so
// the detail render reconstructs the real release hero card (list entries stay
// generic).
func loadReleaseHistory(ctx tuicore.HistoryLoadContext) ([]tuicore.VersionItem, error) {
	return tuicore.LoadMessageHistory(ctx, "release", func(base tuicore.MessageVersionItem) tuicore.VersionItem {
		return ReleaseVersionItem{MessageVersionItem: base}
	})
}
