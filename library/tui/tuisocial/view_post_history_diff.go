// view_post_history_diff.go - Wires the shared HistoryDiffView to post history
package tuisocial

import (
	"fmt"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// NewPostHistoryDiffView creates a HistoryDiffView wired to social post history.
func NewPostHistoryDiffView(workdir string) *tuicore.HistoryDiffView {
	return tuicore.NewHistoryDiffView(workdir, tuicore.HistoryDiffConfig{
		Context:   tuicore.HistoryDiff,
		TitleIcon: "◉",
		Title:     "Post Diff",
		Load:      loadPostHistoryVersions,
	})
}

// loadPostHistoryVersions loads all versions of a post and projects them to DiffVersions.
func loadPostHistoryVersions(workdir string, params map[string]string) ([]tuicore.DiffVersion, error) {
	postID := params["postID"]
	if postID == "" {
		return nil, fmt.Errorf("missing postID")
	}
	parsed := protocol.ParseRef(postID)
	if parsed.Value == "" {
		return nil, fmt.Errorf("invalid post ref: %s", postID)
	}
	branch := parsed.Branch
	if branch == "" {
		branch = gitmsg.GetExtBranch(workdir, "social")
	}
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	posts, err := social.GetEditHistoryPosts(parsed.Repository, parsed.Value, branch, workspaceURL)
	if err != nil {
		return nil, err
	}
	out := make([]tuicore.DiffVersion, 0, len(posts))
	total := len(posts)
	for i, p := range posts {
		fields := map[string]string{}
		if len(p.Labels) > 0 {
			fields["labels"] = strings.Join(p.Labels, ", ")
		}
		out = append(out, tuicore.DiffVersion{
			ID:        p.ID,
			Label:     postVersionLabel(i, total, p.EditOf != ""),
			Content:   tuicore.DiffContentWithMetadata(fields, p.IsRetracted, p.Content),
			Author:    p.Author.Name,
			Email:     p.Author.Email,
			Timestamp: p.Timestamp,
		})
	}
	// social returns DESC (latest first); reverse to ASC so toIdx=last is "latest".
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// postVersionLabel mirrors tuicore.VersionLabel using the DESC index it receives.
func postVersionLabel(descIdx, total int, hasEditOf bool) string {
	if descIdx == 0 {
		return "latest"
	}
	if descIdx == total-1 && !hasEditOf {
		return "original"
	}
	return fmt.Sprintf("v%d", total-descIdx)
}
