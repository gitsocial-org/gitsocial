// view_release_history_diff.go - History diff view for releases
package tuirelease

import (
	"fmt"

	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// NewReleaseHistoryDiffView creates a HistoryDiffView wired to release history.
func NewReleaseHistoryDiffView(workdir string) *tuicore.HistoryDiffView {
	return tuicore.NewHistoryDiffView(workdir, tuicore.HistoryDiffConfig{
		Context:   tuicore.ReleaseHistoryDiff,
		TitleIcon: "⏏",
		Title:     "Release Diff",
		Load:      loadReleaseHistoryVersions,
	})
}

// loadReleaseHistoryVersions reads "releaseID" from route params, loads message
// history, and projects each version to a DiffVersion.
func loadReleaseHistoryVersions(workdir string, params map[string]string) ([]tuicore.DiffVersion, error) {
	id := params["releaseID"]
	if id == "" {
		return nil, fmt.Errorf("missing releaseID")
	}
	parsed := protocol.ParseRef(id)
	if parsed.Value == "" {
		return nil, fmt.Errorf("invalid ref: %s", id)
	}
	branch := parsed.Branch
	if branch == "" {
		branch = gitmsg.GetExtBranch(workdir, "release")
	}
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	ref := protocol.CreateRef(protocol.RefTypeCommit, parsed.Value, parsed.Repository, branch)
	versions, err := gitmsg.GetHistory(ref, workspaceURL)
	if err != nil {
		return nil, err
	}
	out := make([]tuicore.DiffVersion, 0, len(versions))
	total := len(versions)
	for i, ver := range versions {
		out = append(out, tuicore.DiffVersion{
			ID:        ver.ID,
			Label:     releaseVersionLabel(i, total, ver.EditOf != ""),
			Content:   ver.Content,
			Author:    ver.AuthorName,
			Email:     ver.AuthorEmail,
			Timestamp: ver.Timestamp,
		})
	}
	// gitmsg.GetHistory returns DESC; reverse to ASC.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// releaseVersionLabel mirrors tuicore.VersionLabel for DESC-ordered version slices.
func releaseVersionLabel(descIdx, total int, hasEditOf bool) string {
	if descIdx == 0 {
		return "latest"
	}
	if descIdx == total-1 && !hasEditOf {
		return "original"
	}
	return fmt.Sprintf("v%d", total-descIdx)
}
