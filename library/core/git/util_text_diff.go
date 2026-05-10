// util_text_diff.go - Line-based unified diff between two strings
package git

import (
	"fmt"
	"strings"
)

// TextDiffLineKind classifies a row in a unified text diff.
type TextDiffLineKind int

const (
	// TextDiffContext is an unchanged line shown for context.
	TextDiffContext TextDiffLineKind = iota
	// TextDiffAdded is a line present only in the new text.
	TextDiffAdded
	// TextDiffRemoved is a line present only in the old text.
	TextDiffRemoved
	// TextDiffHunkHeader is the synthesized "@@ -a,b +c,d @@" row.
	TextDiffHunkHeader
	// TextDiffCollapsed is a placeholder for an auto-collapsed run of context.
	TextDiffCollapsed
)

// TextDiffRow is one row of a structured unified diff.
type TextDiffRow struct {
	Kind    TextDiffLineKind
	Text    string
	OldLine int    // 1-based; 0 if not applicable
	NewLine int    // 1-based; 0 if not applicable
	Hidden  int    // for TextDiffCollapsed: number of hidden context lines
	HunkID  int    // 0-based hunk index this row belongs to
	Key     string // stable identity used for scroll anchoring across pair toggles
}

// TextDiffOptions controls UnifiedTextDiff output.
type TextDiffOptions struct {
	ContextLines int // unchanged lines around each change (default 3)
	CollapseAt   int // collapse runs of unchanged lines longer than this (0 disables)
}

// UnifiedTextDiff returns a structured row-list unified diff between two strings.
// Output rows have stable Keys suitable for cross-render anchoring.
func UnifiedTextDiff(from, to string, opts TextDiffOptions) []TextDiffRow {
	if opts.ContextLines <= 0 {
		opts.ContextLines = 3
	}
	oldLines := splitLines(from)
	newLines := splitLines(to)
	lcs := lcsLines(oldLines, newLines)
	edits := walkEdits(oldLines, newLines, lcs)
	return buildHunks(edits, opts)
}

// splitLines splits s on newlines without producing a trailing empty element.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// lcsLines computes the LCS table for two line slices and returns it.
func lcsLines(a, b []string) [][]int {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	return dp
}

// editOp records one row of the raw edit script (pre-hunking, pre-collapse).
type editOp struct {
	kind    TextDiffLineKind
	text    string
	oldLine int
	newLine int
}

// walkEdits backtracks the LCS table into a forward edit sequence.
func walkEdits(a, b []string, dp [][]int) []editOp {
	i, j := len(a), len(b)
	var rev []editOp
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && a[i-1] == b[j-1]:
			rev = append(rev, editOp{kind: TextDiffContext, text: a[i-1], oldLine: i, newLine: j})
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]):
			rev = append(rev, editOp{kind: TextDiffAdded, text: b[j-1], newLine: j})
			j--
		default:
			rev = append(rev, editOp{kind: TextDiffRemoved, text: a[i-1], oldLine: i})
			i--
		}
	}
	out := make([]editOp, len(rev))
	for k, op := range rev {
		out[len(rev)-1-k] = op
	}
	return out
}

// buildHunks groups the edit script into hunks with the requested context, optionally
// auto-collapsing long unchanged runs between hunks into a placeholder row.
func buildHunks(edits []editOp, opts TextDiffOptions) []TextDiffRow {
	if len(edits) == 0 {
		return nil
	}
	changed := make([]bool, len(edits))
	anyChange := false
	for i, e := range edits {
		if e.kind != TextDiffContext {
			changed[i] = true
			anyChange = true
		}
	}
	if !anyChange {
		return nil
	}
	type span struct{ start, end int }
	var spans []span
	i := 0
	for i < len(edits) {
		if !changed[i] {
			i++
			continue
		}
		s := i - opts.ContextLines
		if s < 0 {
			s = 0
		}
		j := i
		for j < len(edits) {
			if changed[j] {
				j++
				continue
			}
			gap := 0
			k := j
			for k < len(edits) && !changed[k] {
				gap++
				k++
			}
			if k < len(edits) && gap <= opts.ContextLines*2 {
				j = k
				continue
			}
			break
		}
		e := j + opts.ContextLines
		if e > len(edits) {
			e = len(edits)
		}
		if len(spans) > 0 && spans[len(spans)-1].end >= s {
			spans[len(spans)-1].end = e
		} else {
			spans = append(spans, span{start: s, end: e})
		}
		i = j
	}
	var rows []TextDiffRow
	prevEnd := 0
	for hunkIdx, sp := range spans {
		if opts.CollapseAt > 0 && sp.start > prevEnd {
			gap := sp.start - prevEnd
			if gap > opts.CollapseAt {
				rows = append(rows, TextDiffRow{
					Kind:   TextDiffCollapsed,
					Hidden: gap,
					HunkID: hunkIdx,
					Key:    fmt.Sprintf("collapsed:%d", prevEnd),
				})
			}
		}
		oldStart, oldCount := 0, 0
		newStart, newCount := 0, 0
		for k := sp.start; k < sp.end; k++ {
			op := edits[k]
			if op.oldLine > 0 {
				if oldStart == 0 {
					oldStart = op.oldLine
				}
				if op.kind != TextDiffAdded {
					oldCount++
				}
			}
			if op.newLine > 0 {
				if newStart == 0 {
					newStart = op.newLine
				}
				if op.kind != TextDiffRemoved {
					newCount++
				}
			}
		}
		if oldStart == 0 {
			oldStart = 1
		}
		if newStart == 0 {
			newStart = 1
		}
		header := fmt.Sprintf("@@ -%d,%d +%d,%d @@", oldStart, oldCount, newStart, newCount)
		rows = append(rows, TextDiffRow{
			Kind:   TextDiffHunkHeader,
			Text:   header,
			HunkID: hunkIdx,
			Key:    fmt.Sprintf("hunk:%d:%d:%d", hunkIdx, oldStart, newStart),
		})
		for k := sp.start; k < sp.end; k++ {
			op := edits[k]
			rows = append(rows, TextDiffRow{
				Kind:    op.kind,
				Text:    op.text,
				OldLine: op.oldLine,
				NewLine: op.newLine,
				HunkID:  hunkIdx,
				Key:     rowKey(hunkIdx, op),
			})
		}
		prevEnd = sp.end
	}
	return rows
}

// rowKey produces a stable identity for a diff row that survives pair toggles.
func rowKey(hunkIdx int, op editOp) string {
	switch op.kind {
	case TextDiffAdded:
		return fmt.Sprintf("row:%d:add:%d", hunkIdx, op.newLine)
	case TextDiffRemoved:
		return fmt.Sprintf("row:%d:rem:%d", hunkIdx, op.oldLine)
	default:
		return fmt.Sprintf("row:%d:ctx:%d:%d", hunkIdx, op.oldLine, op.newLine)
	}
}

// FormatTextDiff renders rows as a plain unified-diff string (no ANSI). Useful for tests
// and CLI output.
func FormatTextDiff(rows []TextDiffRow) string {
	var b strings.Builder
	for _, r := range rows {
		switch r.Kind {
		case TextDiffHunkHeader:
			b.WriteString(r.Text)
			b.WriteByte('\n')
		case TextDiffAdded:
			b.WriteString("+")
			b.WriteString(r.Text)
			b.WriteByte('\n')
		case TextDiffRemoved:
			b.WriteString("-")
			b.WriteString(r.Text)
			b.WriteByte('\n')
		case TextDiffContext:
			b.WriteString(" ")
			b.WriteString(r.Text)
			b.WriteByte('\n')
		case TextDiffCollapsed:
			fmt.Fprintf(&b, "··· %d unchanged lines ···\n", r.Hidden)
		}
	}
	return b.String()
}
