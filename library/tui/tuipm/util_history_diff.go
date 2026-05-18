// util_history_diff.go - Shared loader for PM history diff views
package tuipm

import (
	"fmt"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// loadPMHistoryVersionsKey returns a loader that reads the route param under paramKey,
// loads the message history, and projects each version to a DiffVersion.
func loadPMHistoryVersionsKey(paramKey string) tuicore.LoadDiffVersionsFunc {
	return func(workdir string, params map[string]string) ([]tuicore.DiffVersion, error) {
		id := params[paramKey]
		if id == "" {
			return nil, fmt.Errorf("missing %s", paramKey)
		}
		parsed := protocol.ParseRef(id)
		if parsed.Value == "" {
			return nil, fmt.Errorf("invalid ref: %s", id)
		}
		branch := parsed.Branch
		if branch == "" {
			branch = gitmsg.GetExtBranch(workdir, "pm")
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
				Label:     pmVersionLabel(i, total, ver.EditOf != ""),
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
}

// pmVersionLabel mirrors tuicore.VersionLabel for DESC-ordered version slices.
func pmVersionLabel(descIdx, total int, hasEditOf bool) string {
	if descIdx == 0 {
		return "latest"
	}
	if descIdx == total-1 && !hasEditOf {
		return "original"
	}
	return fmt.Sprintf("v%d", total-descIdx)
}
