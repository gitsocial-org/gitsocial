// view_pr_history_diff.go - History diff view for pull request descriptions
package tuireview

import (
	"fmt"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// NewPRHistoryDiffView creates a HistoryDiffView wired to PR description history.
func NewPRHistoryDiffView(workdir string) *tuicore.HistoryDiffView {
	return tuicore.NewHistoryDiffView(workdir, tuicore.HistoryDiffConfig{
		Context:   tuicore.ReviewPRHistoryDiff,
		TitleIcon: "⑂",
		Title:     "PR Diff",
		Load:      loadPRHistoryVersions,
	})
}

// loadPRHistoryVersions loads all PR versions and projects them to DiffVersions.
// PR Content is "subject\n\nbody" (the description text), so the diff shows how
// the PR description was edited across versions; for code-level diff use `i:interdiff`.
func loadPRHistoryVersions(workdir string, params map[string]string) ([]tuicore.DiffVersion, error) {
	prID := params["prID"]
	if prID == "" {
		return nil, fmt.Errorf("missing prID")
	}
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	res := review.GetPRVersions(prID, workspaceURL)
	if !res.Success {
		return nil, fmt.Errorf("%s", res.Error.Message)
	}
	out := make([]tuicore.DiffVersion, 0, len(res.Data))
	for _, ver := range res.Data {
		var content string
		switch {
		case ver.Subject != "" && ver.Body != "":
			content = ver.Subject + "\n\n" + strings.TrimRight(ver.Body, "\n")
		case ver.Subject != "":
			content = ver.Subject
		case ver.Body != "":
			content = strings.TrimRight(ver.Body, "\n")
		}
		fields := map[string]string{}
		if ver.State != "" {
			fields["state"] = string(ver.State)
		}
		if ver.BaseTip != "" {
			fields["base-tip"] = ver.BaseTip
		}
		if ver.HeadTip != "" {
			fields["head-tip"] = ver.HeadTip
		}
		out = append(out, tuicore.DiffVersion{
			ID:        fmt.Sprintf("v%d", ver.Number),
			Label:     ver.Label,
			Content:   tuicore.DiffContentWithMetadata(fields, ver.IsRetracted, content),
			Author:    ver.AuthorName,
			Email:     ver.AuthorEmail,
			Timestamp: ver.Timestamp,
		})
	}
	return out, nil
}
