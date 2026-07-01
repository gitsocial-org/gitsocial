// feedback_layer.go - Composable PR-feedback decoration for diff.DisplayPlan.
//
// Inline feedback rows are inserted after the diff line each comment
// anchors to, using the cell model directly (no ANSI-string round-trip).
// The layer reads file paths from plan.Logical.Files rather than parsing
// rendered cell text.
package tuireview

import (
	"strings"

	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore/diff"
)

// feedbackLayer interleaves inline PR feedback rows into a DisplayPlan.
type feedbackLayer struct {
	items     []review.Feedback
	userEmail string
	showEmail bool
}

func newFeedbackLayer(items []review.Feedback, userEmail string, showEmail bool) feedbackLayer {
	return feedbackLayer{items: items, userEmail: userEmail, showEmail: showEmail}
}

// Decorate walks the plan and, after each body row that has matching
// feedback (by file + line), inserts feedback rows built from cells.
func (l feedbackLayer) Decorate(plan diff.DisplayPlan, _ diff.ViewState) diff.DisplayPlan {
	if len(l.items) == 0 {
		return plan
	}
	out := diff.DisplayPlan{Rows: make([]diff.DisplayRow, 0, len(plan.Rows)), Logical: plan.Logical}
	for _, row := range plan.Rows {
		out.Rows = append(out.Rows, row)
		if row.Kind != diff.RowAdded && row.Kind != diff.RowRemoved && row.Kind != diff.RowContext {
			continue
		}
		line := row.Anchor.NewLine
		if line == 0 {
			line = row.Anchor.OldLine
		}
		filePath := filePathFromLogical(plan.Logical, row.Anchor.FileIdx)
		for _, fb := range l.items {
			if fb.File != filePath {
				continue
			}
			fbLine := fb.NewLine
			if fbLine == 0 {
				fbLine = fb.OldLine
			}
			if fbLine != line {
				continue
			}
			anchor := diff.RowAnchor{
				FileIdx: row.Anchor.FileIdx,
				HunkIdx: row.Anchor.HunkIdx,
				OldLine: row.Anchor.OldLine,
				NewLine: row.Anchor.NewLine,
				Tag:     "feedback",
			}
			out.Rows = append(out.Rows, feedbackRows(fb, l.userEmail, l.showEmail, anchor)...)
		}
	}
	return out
}

// filePathFromLogical reads the file path for the given fileIdx from
// LogicalDiff.Files (NewPath, falling back to OldPath).
func filePathFromLogical(ld diff.LogicalDiff, fileIdx int) string {
	if fileIdx < 0 || fileIdx >= len(ld.Files) {
		return ""
	}
	fd := ld.Files[fileIdx]
	if fd.NewPath != "" {
		return fd.NewPath
	}
	return fd.OldPath
}

// feedbackRows builds the DisplayRows for one inline feedback item:
// header (icon + author + timestamp) followed by one row per content line.
// Cells carry style attributes directly — no ANSI round-trip.
func feedbackRows(fb review.Feedback, userEmail string, showEmail bool, anchor diff.RowAnchor) []diff.DisplayRow {
	icon := "↩"
	switch fb.ReviewState {
	case review.ReviewStateApproved:
		icon = "✓"
	case review.ReviewStateChangesRequested:
		icon = "✗"
	}
	name := fb.Author.Name
	if showEmail && fb.Author.Email != "" {
		name += " <" + fb.Author.Email + ">"
	}
	authorFG, authorBold := authorAttrs(fb.Author.Email, userEmail)
	header := diff.DisplayRow{Row: diff.Row{
		Kind: diff.RowFeedback,
		Cells: []diff.Cell{
			{Text: "  "},
			{Text: icon},
			{Text: " "},
			{Text: name, FG: authorFG, Bold: authorBold},
			{Text: "  "},
			{Text: tuicore.FormatTime(fb.Timestamp), FG: tuicore.ThemeString(tuicore.TextSecondary)},
		},
		Anchor: anchor,
	}}
	rows := []diff.DisplayRow{header}
	if fb.Content == "" {
		return rows
	}
	for _, line := range strings.Split(fb.Content, "\n") {
		rows = append(rows, diff.DisplayRow{Row: diff.Row{
			Kind:   diff.RowFeedback,
			Cells:  []diff.Cell{{Text: "    " + line}},
			Anchor: anchor,
		}})
	}
	return rows
}

// authorAttrs picks the cell FG + Bold for an author name, mirroring
// tuicore.AuthorStyle's MeTitle-vs-Dim choice.
func authorAttrs(authorEmail, userEmail string) (fg string, bold bool) {
	if userEmail != "" && authorEmail != "" && strings.EqualFold(authorEmail, userEmail) {
		return tuicore.ThemeString(tuicore.IdentityMe), true
	}
	return tuicore.ThemeString(tuicore.TextSecondary), false
}
