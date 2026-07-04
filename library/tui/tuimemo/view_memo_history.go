// view_memo_history.go - Memo edit-history view (versions picker).
package tuimemo

import (
	"fmt"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// MemoVersionItem reuses the generic VersionItem plumbing of MessageVersionItem
// with memo-specific list and detail layouts. Memos have no cross-repo proposal
// flow, so the embedded ProposalTag stays empty.
type MemoVersionItem struct {
	tuicore.MessageVersionItem
}

// RenderListEntry renders a compact summary line for the picker: header on
// line 1 (version, label, hash, author, time), body excerpt on line 2, and
// labels on line 3 (when set).
func (m MemoVersionItem) RenderListEntry(index, total int, label string, selected bool, width int) string {
	hash, _ := protocol.NormalizeHash(protocol.ParseRef(m.Version.ID).Value)
	header := fmt.Sprintf("Version %d (%s) - %s - %s - %s",
		total-index, label, hash, m.AuthorDisplay(m.ShowEmail), m.Version.Timestamp.Format("2006-01-02 15:04:05"))
	var b strings.Builder
	if selected {
		b.WriteString(tuicore.Highlight.Render("▶ " + header))
	} else {
		b.WriteString("  " + header)
	}
	b.WriteString("\n")
	if m.Version.IsRetracted {
		b.WriteString(tuicore.Dim.Render("    [retracted]"))
	} else {
		subj, _ := protocol.SplitSubjectBody(m.Version.Content)
		excerpt := strings.TrimSpace(subj)
		if excerpt == "" {
			excerpt = strings.TrimSpace(m.Version.Content)
		}
		if len(excerpt) > 100 {
			excerpt = excerpt[:100] + "..."
		}
		b.WriteString("    " + excerpt)
	}
	if len(m.Version.Labels) > 0 {
		b.WriteString("\n")
		b.WriteString("    " + tuicore.Dim.Render(strings.Join(m.Version.Labels, " · ")))
	}
	b.WriteString("\n")
	return b.String()
}

// RenderDetail renders the version's full content for the picker's detail
// pane: header (author, timestamp, ref), labels, then the markdown-rendered
// body. Mirrors the layout of the memo detail card so version inspection is
// visually consistent.
func (m MemoVersionItem) RenderDetail(width int) string {
	if m.Version.IsRetracted {
		return tuicore.Dim.Render("[retracted]")
	}
	wrap := width - 5
	if wrap < 20 {
		wrap = 20
	}
	subj, body := protocol.SplitSubjectBody(m.Version.Content)
	var lines []string
	if subj != "" {
		lines = append(lines, tuicore.Bold.Render(subj))
		lines = append(lines, tuicore.Dim.Render(strings.Repeat("─", wrap)))
	}
	meta := fmt.Sprintf("%s · %s", m.AuthorDisplay(m.ShowEmail), m.Version.Timestamp.Format("2006-01-02 15:04:05"))
	hash, _ := protocol.NormalizeHash(protocol.ParseRef(m.Version.ID).Value)
	if hash != "" {
		meta += " · " + hash
	}
	lines = append(lines, tuicore.Dim.Render(meta))
	if len(m.Version.Labels) > 0 {
		lines = append(lines, tuicore.Dim.Render(strings.Join(m.Version.Labels, " · ")))
	}
	if body != "" {
		lines = append(lines, "")
		lines = append(lines, tuicore.RenderMarkdown(body, wrap))
	}
	return strings.Join(lines, "\n")
}

// loadMemoHistory fetches and wraps the edit chain for a memo.
func loadMemoHistory(ctx tuicore.HistoryLoadContext) ([]tuicore.VersionItem, error) {
	versions, err := gitmsg.GetHistory(ctx.Ref, ctx.WorkspaceURL)
	if err != nil {
		return nil, err
	}
	items := make([]tuicore.VersionItem, len(versions))
	for i, ver := range versions {
		items[i] = MemoVersionItem{MessageVersionItem: tuicore.MessageVersionItem{Version: ver, ShowEmail: ctx.ShowEmail}}
	}
	return items, nil
}

// NewMemoHistoryView creates the edit-history view for a memo.
func NewMemoHistoryView(workdir string) *tuicore.HistoryView {
	return tuicore.NewHistoryView(workdir, tuicore.HistoryConfig{
		ParamName:  "memoID",
		Context:    tuicore.MemoHistory,
		TitleLabel: "☞  History",
		Load:       loadMemoHistory,
		DiffLoc:    tuicore.LocMemoHistoryDiff,
	})
}
