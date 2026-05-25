// view_memo_history_diff.go - Wires the shared HistoryDiffView to memo history
package tuimemo

import (
	"fmt"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// NewMemoHistoryDiffView creates a HistoryDiffView wired to memo history.
func NewMemoHistoryDiffView(workdir string) *tuicore.HistoryDiffView {
	return tuicore.NewHistoryDiffView(workdir, tuicore.HistoryDiffConfig{
		Context:   tuicore.MemoHistoryDiff,
		TitleIcon: "☞",
		Title:     "Memo Diff",
		Load:      loadMemoHistoryVersions,
	})
}

// loadMemoHistoryVersions loads all versions of a memo and projects them to DiffVersions.
func loadMemoHistoryVersions(workdir string, params map[string]string) ([]tuicore.DiffVersion, error) {
	memoID := params["memoID"]
	if memoID == "" {
		return nil, fmt.Errorf("missing memoID")
	}
	parsed := protocol.ParseRef(memoID)
	if parsed.Value == "" {
		return nil, fmt.Errorf("invalid ref: %s", memoID)
	}
	branch := parsed.Branch
	if branch == "" {
		branch = gitmsg.GetExtBranch(workdir, "memo")
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
			Label:     memoVersionLabel(i, total, ver.EditOf != ""),
			Content:   ver.Content,
			Author:    ver.AuthorName,
			Email:     ver.AuthorEmail,
			Timestamp: ver.Timestamp,
		})
	}
	// gitmsg.GetHistory returns DESC; reverse to ASC so toIdx=last is "latest".
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// memoVersionLabel mirrors tuicore.VersionLabel for DESC-ordered version slices.
func memoVersionLabel(descIdx, total int, hasEditOf bool) string {
	if descIdx == 0 {
		return "latest"
	}
	if descIdx == total-1 && !hasEditOf {
		return "original"
	}
	return fmt.Sprintf("v%d", total-descIdx)
}
