// view_pr_history_diff.go - History diff view for pull request descriptions
package tuireview

import (
	"fmt"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// NewPRHistoryDiffView creates a HistoryDiffView wired to PR description history.
func NewPRHistoryDiffView(workdir string) *tuicore.HistoryDiffView {
	return tuicore.NewHistoryDiffView(workdir, tuicore.HistoryDiffConfig{
		Context:    tuicore.ReviewPRHistoryDiff,
		TitleIcon:  "⑂",
		Title:      "PR Diff",
		Load:       loadPRHistoryVersions,
		EnablePush: true,
	})
}

// loadPRHistoryVersions reads "prID" from route params, loads message history,
// and projects each version to a DiffVersion. Uses the same gitmsg.GetHistory
// path as every other history-diff loader (issue, release, memo) so the diff
// renders the full metadata block (base, head, tips, state, reviewers, ...)
// from ver.Fields rather than a hand-picked subset.
func loadPRHistoryVersions(workdir string, params map[string]string) ([]tuicore.DiffVersion, error) {
	prID := params["prID"]
	if prID == "" {
		return nil, fmt.Errorf("missing prID")
	}
	parsed := protocol.ParseRef(prID)
	if parsed.Value == "" {
		return nil, fmt.Errorf("invalid ref: %s", prID)
	}
	branch := parsed.Branch
	if branch == "" {
		branch = gitmsg.GetExtBranch(workdir, "review")
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
			Label:     prVersionLabel(i, total, ver.EditOf != ""),
			Content:   tuicore.DiffContentWithMetadata(ver.Fields, ver.IsRetracted, ver.Content),
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

// prVersionLabel mirrors tuicore.VersionLabel for DESC-ordered version slices.
func prVersionLabel(descIdx, total int, hasEditOf bool) string {
	if descIdx == 0 {
		return "latest"
	}
	if descIdx == total-1 && !hasEditOf {
		return "original"
	}
	return fmt.Sprintf("v%d", total-descIdx)
}
