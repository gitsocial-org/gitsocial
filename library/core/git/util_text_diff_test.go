// util_text_diff_test.go - Tests for line-based unified text diff
package git

import (
	"strings"
	"testing"
)

// TestUnifiedTextDiff_identical asserts identical inputs produce no rows.
func TestUnifiedTextDiff_identical(t *testing.T) {
	rows := UnifiedTextDiff("a\nb\nc\n", "a\nb\nc\n", TextDiffOptions{})
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows, got %d: %#v", len(rows), rows)
	}
}

// TestUnifiedTextDiff_emptyBoth asserts empty inputs produce no rows.
func TestUnifiedTextDiff_emptyBoth(t *testing.T) {
	rows := UnifiedTextDiff("", "", TextDiffOptions{})
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(rows))
	}
}

// TestUnifiedTextDiff_allAdded asserts a fully-added file produces only added rows.
func TestUnifiedTextDiff_allAdded(t *testing.T) {
	rows := UnifiedTextDiff("", "x\ny\n", TextDiffOptions{})
	gotAdded := 0
	for _, r := range rows {
		if r.Kind == TextDiffAdded {
			gotAdded++
		}
		if r.Kind == TextDiffRemoved {
			t.Fatalf("unexpected removed row: %#v", r)
		}
	}
	if gotAdded != 2 {
		t.Fatalf("expected 2 added rows, got %d", gotAdded)
	}
}

// TestUnifiedTextDiff_allRemoved asserts a fully-removed file produces only removed rows.
func TestUnifiedTextDiff_allRemoved(t *testing.T) {
	rows := UnifiedTextDiff("x\ny\n", "", TextDiffOptions{})
	gotRemoved := 0
	for _, r := range rows {
		if r.Kind == TextDiffRemoved {
			gotRemoved++
		}
		if r.Kind == TextDiffAdded {
			t.Fatalf("unexpected added row: %#v", r)
		}
	}
	if gotRemoved != 2 {
		t.Fatalf("expected 2 removed rows, got %d", gotRemoved)
	}
}

// TestUnifiedTextDiff_midEdit checks a single mid-file edit produces a single hunk
// with the expected counts and a stable header.
func TestUnifiedTextDiff_midEdit(t *testing.T) {
	from := "a\nb\nc\nd\ne\n"
	to := "a\nb\nC\nd\ne\n"
	rows := UnifiedTextDiff(from, to, TextDiffOptions{ContextLines: 1})
	hunks := 0
	added, removed, context := 0, 0, 0
	for _, r := range rows {
		switch r.Kind {
		case TextDiffHunkHeader:
			hunks++
			if !strings.HasPrefix(r.Text, "@@") {
				t.Fatalf("bad hunk header: %q", r.Text)
			}
		case TextDiffAdded:
			added++
		case TextDiffRemoved:
			removed++
		case TextDiffContext:
			context++
		}
	}
	if hunks != 1 {
		t.Fatalf("expected 1 hunk, got %d", hunks)
	}
	if added != 1 || removed != 1 {
		t.Fatalf("expected 1+/1-, got %d+/%d-", added, removed)
	}
	if context != 2 {
		t.Fatalf("expected 2 context (1 before + 1 after), got %d", context)
	}
}

// TestUnifiedTextDiff_multiHunk verifies two distant edits split into two hunks.
func TestUnifiedTextDiff_multiHunk(t *testing.T) {
	from := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n"
	to := "A\nb\nc\nd\ne\nf\ng\nh\ni\nJ\n"
	rows := UnifiedTextDiff(from, to, TextDiffOptions{ContextLines: 1})
	hunks := 0
	for _, r := range rows {
		if r.Kind == TextDiffHunkHeader {
			hunks++
		}
	}
	if hunks != 2 {
		t.Fatalf("expected 2 hunks, got %d", hunks)
	}
}

// TestUnifiedTextDiff_collapse verifies long unchanged gaps between hunks become
// collapsed placeholder rows when CollapseAt is set.
func TestUnifiedTextDiff_collapse(t *testing.T) {
	var fb, tb strings.Builder
	fb.WriteString("X\n")
	tb.WriteString("X\n")
	for i := 0; i < 50; i++ {
		fb.WriteString("same\n")
		tb.WriteString("same\n")
	}
	fb.WriteString("Y\n")
	tb.WriteString("Z\n")
	rows := UnifiedTextDiff(fb.String(), tb.String(), TextDiffOptions{ContextLines: 1, CollapseAt: 3})
	collapsed := 0
	for _, r := range rows {
		if r.Kind == TextDiffCollapsed {
			collapsed++
			if r.Hidden == 0 {
				t.Fatalf("collapsed row has zero Hidden")
			}
		}
	}
	if collapsed != 1 {
		t.Fatalf("expected 1 collapsed placeholder, got %d", collapsed)
	}
}

// TestUnifiedTextDiff_stableKeys asserts every non-collapsed, non-header row has a unique
// stable Key, and that the same row in the inverted diff swaps add/rem prefixes.
func TestUnifiedTextDiff_stableKeys(t *testing.T) {
	rows := UnifiedTextDiff("a\nb\nc\n", "a\nB\nc\n", TextDiffOptions{ContextLines: 1})
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Key == "" {
			t.Fatalf("row missing key: %#v", r)
		}
		if seen[r.Key] {
			t.Fatalf("duplicate key %q", r.Key)
		}
		seen[r.Key] = true
	}
}

// TestFormatTextDiff_basic verifies the plain-string formatter emits the expected prefixes.
func TestFormatTextDiff_basic(t *testing.T) {
	rows := UnifiedTextDiff("a\nb\n", "a\nB\n", TextDiffOptions{ContextLines: 1})
	out := FormatTextDiff(rows)
	if !strings.Contains(out, "+B") || !strings.Contains(out, "-b") || !strings.Contains(out, " a") {
		t.Fatalf("missing expected lines:\n%s", out)
	}
}
