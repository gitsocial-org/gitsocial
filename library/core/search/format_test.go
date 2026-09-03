// format_test.go - Tests for the CLI text rendering of search results
package search

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// fiveMinutesAgo returns a timestamp that formatDate renders as "5m ago", so a
// rendered block can be asserted whole.
func fiveMinutesAgo() time.Time {
	return time.Now().Add(-5 * time.Minute)
}

// TestFormatResultNoResults pins the empty-result line, which names the query.
func TestFormatResultNoResults(t *testing.T) {
	got := FormatResult(Result{Query: "parser bug"})
	want := "No results for 'parser bug'."
	if got != want {
		t.Errorf("FormatResult() = %q, want %q", got, want)
	}

	t.Run("empty query", func(t *testing.T) {
		if got := FormatResult(Result{}); got != "No results for ''." {
			t.Errorf("FormatResult() = %q", got)
		}
	})
}

// TestFormatResultSingleItem pins the whole rendered block: header, content and
// the trailing count line with its separator.
func TestFormatResultSingleItem(t *testing.T) {
	result := Result{
		Query: "parser",
		Results: []ScoredItem{{Item: Item{
			AuthorName: "Alice",
			Content:    "Fix the parser",
			Timestamp:  fiveMinutesAgo(),
			Extension:  "pm",
			Type:       "issue",
		}}},
		Total:           1,
		ExecutionTimeMs: 12,
	}
	want := "Alice · 5m ago [pm/issue]\nFix the parser\n\n---\n\n\n1 results (12.00ms)"
	if got := FormatResult(result); got != want {
		t.Errorf("FormatResult() =\n%q\nwant\n%q", got, want)
	}
}

// TestFormatResultMultipleItems checks that items are separated by the rule and
// that the count line reports Total, not the number rendered.
func TestFormatResultMultipleItems(t *testing.T) {
	result := Result{
		Query: "parser",
		Results: []ScoredItem{
			{Item: Item{AuthorName: "Alice", Content: "First", Timestamp: fiveMinutesAgo(), Extension: "social"}},
			{Item: Item{AuthorName: "Bob", Content: "Second", Timestamp: fiveMinutesAgo(), Extension: "social"}},
		},
		Total:           7,
		ExecutionTimeMs: 3,
	}
	got := FormatResult(result)
	if strings.Count(got, "\n---\n") != 2 {
		t.Errorf("got %d separators, want 2 (one between items, one before the count line):\n%s", strings.Count(got, "\n---\n"), got)
	}
	if !strings.HasSuffix(got, "\n7 results (3.00ms)") {
		t.Errorf("output does not end with the total line:\n%s", got)
	}
	if !strings.Contains(got, "Alice · 5m ago\nFirst") {
		t.Errorf("social items must not carry an [ext/type] tag:\n%s", got)
	}
}

// TestFormatResultPrefersGroups checks that a grouped result never falls through
// to the flat renderer.
func TestFormatResultPrefersGroups(t *testing.T) {
	result := Result{
		Query:   "parser",
		GroupBy: "state",
		Groups:  []Group{{Key: "open", Count: 2}},
		Results: []ScoredItem{{Item: Item{AuthorName: "Alice", Content: "First", Timestamp: fiveMinutesAgo()}}},
		Total:   2,
	}
	got := FormatResult(result)
	if !strings.HasPrefix(got, "## open (2)") {
		t.Errorf("grouped result did not render as groups:\n%s", got)
	}
	if strings.Contains(got, "First") {
		t.Errorf("grouped result leaked flat item content:\n%s", got)
	}
}

// TestFormatItem checks the header tag rule and the content truncation bound.
func TestFormatItem(t *testing.T) {
	tests := []struct {
		name      string
		item      ScoredItem
		wantFirst string
	}{
		{
			name:      "social has no tag",
			item:      ScoredItem{Item: Item{AuthorName: "Alice", Extension: "social", Type: "post", Timestamp: fiveMinutesAgo()}},
			wantFirst: "Alice · 5m ago",
		},
		{
			name:      "unknown extension has no tag",
			item:      ScoredItem{Item: Item{AuthorName: "Alice", Extension: "unknown", Type: "unknown", Timestamp: fiveMinutesAgo()}},
			wantFirst: "Alice · 5m ago",
		},
		{
			name:      "pm issue is tagged",
			item:      ScoredItem{Item: Item{AuthorName: "Alice", Extension: "pm", Type: "issue", Timestamp: fiveMinutesAgo()}},
			wantFirst: "Alice · 5m ago [pm/issue]",
		},
		{
			name:      "review pr is tagged",
			item:      ScoredItem{Item: Item{AuthorName: "Bob", Extension: "review", Type: "pr", Timestamp: fiveMinutesAgo()}},
			wantFirst: "Bob · 5m ago [review/pr]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first := strings.Split(formatItem(tt.item), "\n")[0]
			if first != tt.wantFirst {
				t.Errorf("header = %q, want %q", first, tt.wantFirst)
			}
		})
	}

	t.Run("content is trimmed", func(t *testing.T) {
		item := ScoredItem{Item: Item{AuthorName: "Alice", Extension: "social", Timestamp: fiveMinutesAgo(), Content: "  spaced out  \n"}}
		lines := strings.Split(formatItem(item), "\n")
		if lines[1] != "spaced out" {
			t.Errorf("content = %q, want %q", lines[1], "spaced out")
		}
	})

	t.Run("content truncation boundary", func(t *testing.T) {
		for _, n := range []int{199, 200, 201} {
			item := ScoredItem{Item: Item{AuthorName: "A", Extension: "social", Timestamp: fiveMinutesAgo(), Content: strings.Repeat("x", n)}}
			content := strings.Split(formatItem(item), "\n")[1]
			if n <= 200 {
				if content != strings.Repeat("x", n) {
					t.Errorf("len %d: content len = %d, want %d untruncated", n, len(content), n)
				}
				continue
			}
			if content != strings.Repeat("x", 200)+"..." {
				t.Errorf("len %d: content = %q, want 200 chars plus the ellipsis", n, content)
			}
		}
	})

	t.Run("meta line appended when present", func(t *testing.T) {
		item := ScoredItem{Item: Item{AuthorName: "Alice", Extension: "pm", Type: "issue", Timestamp: fiveMinutesAgo(), Content: "Body", State: "open"}}
		lines := strings.Split(formatItem(item), "\n")
		if len(lines) != 3 || lines[2] != "open" {
			t.Errorf("lines = %q, want a third meta line %q", lines, "open")
		}
	})

	t.Run("no meta line when there is nothing to show", func(t *testing.T) {
		item := ScoredItem{Item: Item{AuthorName: "Alice", Extension: "social", Timestamp: fiveMinutesAgo(), Content: "Body"}}
		if lines := strings.Split(formatItem(item), "\n"); len(lines) != 2 {
			t.Errorf("lines = %q, want exactly header and content", lines)
		}
	})
}

// TestFormatItemMeta checks which fields appear, in what order, and the rules
// that suppress a field.
func TestFormatItemMeta(t *testing.T) {
	tests := []struct {
		name string
		item Item
		want string
	}{
		{"nothing to show", Item{}, ""},
		{"state only", Item{State: "open"}, "open"},
		{"draft flag", Item{State: "open", Draft: true}, "open · draft"},
		{"branches need both ends", Item{Base: "main", Head: "feature"}, "main ← feature"},
		{"base without head is dropped", Item{Base: "main"}, ""},
		{"head without base is dropped", Item{Head: "feature"}, ""},
		{"assignees", Item{Assignees: "alice@test.com"}, "assigned:alice@test.com"},
		{"reviewers", Item{Reviewers: "bob@test.com"}, "reviewers:bob@test.com"},
		{"labels", Item{Labels: "bug,ui"}, "labels:bug,ui"},
		{"tag", Item{Tag: "v1.0.0"}, "v1.0.0"},
		{"version equal to tag is not repeated", Item{Tag: "v1.0.0", Version: "v1.0.0"}, "v1.0.0"},
		{"version differing from tag is shown", Item{Tag: "v1.0.0", Version: "1.0.0"}, "v1.0.0 · 1.0.0"},
		{"prerelease", Item{Tag: "v1.0.0-rc1", Prerelease: true}, "v1.0.0-rc1 · pre-release"},
		{"due date", Item{Due: "2026-01-31"}, "due:2026-01-31"},
		{"comment count", Item{Comments: 3}, "↩ 3"},
		{"zero comments hidden", Item{Comments: 0}, ""},
		{
			"full ordering",
			Item{
				State: "open", Draft: true, Base: "main", Head: "feature",
				Assignees: "alice", Reviewers: "bob", Labels: "bug",
				Tag: "v1", Version: "1", Prerelease: true, Due: "2026-01-31", Comments: 2,
			},
			"open · draft · main ← feature · assigned:alice · reviewers:bob · labels:bug · v1 · 1 · pre-release · due:2026-01-31 · ↩ 2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatItemMeta(ScoredItem{Item: tt.item}); got != tt.want {
				t.Errorf("formatItemMeta() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestFormatGroupedResultWithAuthors checks the author sub-grouping path, which
// runs whenever the grouped items still carry an author.
func TestFormatGroupedResultWithAuthors(t *testing.T) {
	result := Result{
		GroupBy: "state",
		Total:   3,
		Groups: []Group{{
			Key:   "open",
			Count: 3,
			Items: []GroupedItem{
				{Hash: "aaaaaaaaaaaa", Author: "Alice", Subject: "First"},
				{Hash: "bbbbbbbbbbbb", Author: "Bob", Subject: "Second"},
				{Hash: "cccccccccccc", Author: "Alice", Subject: "Third"},
			},
		}},
	}
	want := "## open (3)\n  Alice (2): First, Third\n  Bob (1): Second\n\nTotal: 3 (grouped by state)"
	if got := formatGroupedResult(result); got != want {
		t.Errorf("formatGroupedResult() =\n%q\nwant\n%q", got, want)
	}
}

// TestFormatGroupedResultAuthorOverflow checks the "+N more" suffix once an
// author has more than three subjects in a group.
func TestFormatGroupedResultAuthorOverflow(t *testing.T) {
	items := make([]GroupedItem, 0, 5)
	for i := 1; i <= 5; i++ {
		items = append(items, GroupedItem{Author: "Alice", Subject: fmt.Sprintf("S%d", i)})
	}
	got := formatGroupedResult(Result{GroupBy: "state", Total: 5, Groups: []Group{{Key: "open", Count: 5, Items: items}}})
	if !strings.Contains(got, "  Alice (5): S1, S2, S3, ... +2 more") {
		t.Errorf("missing author overflow line:\n%s", got)
	}
}

// TestFormatGroupedResultWithoutAuthors checks the flat subject list used when
// grouping by author, where the per-item author is deliberately omitted.
func TestFormatGroupedResultWithoutAuthors(t *testing.T) {
	items := make([]GroupedItem, 0, 7)
	for i := 1; i <= 7; i++ {
		items = append(items, GroupedItem{Subject: fmt.Sprintf("S%d", i)})
	}
	got := formatGroupedResult(Result{GroupBy: "author", Total: 7, Groups: []Group{{Key: "alice@test.com", Count: 7, Items: items}}})
	want := "## alice@test.com (7)\n  S1, S2, S3, S4, S5, ... +2 more\n\nTotal: 7 (grouped by author)"
	if got != want {
		t.Errorf("formatGroupedResult() =\n%q\nwant\n%q", got, want)
	}
}

// TestFormatGroupedResultCountOnly checks the --count-only shape: headers and
// the total, with no item lines.
func TestFormatGroupedResultCountOnly(t *testing.T) {
	result := Result{
		GroupBy: "state",
		Total:   5,
		Groups:  []Group{{Key: "open", Count: 3}, {Key: "closed", Count: 2}},
	}
	want := "## open (3)\n## closed (2)\n\nTotal: 5 (grouped by state)"
	if got := formatGroupedResult(result); got != want {
		t.Errorf("formatGroupedResult() =\n%q\nwant\n%q", got, want)
	}
}

// TestFormatGroupedResultMissingAuthor checks that an item without an author,
// in a group where others have one, is bucketed under "(unknown)".
func TestFormatGroupedResultMissingAuthor(t *testing.T) {
	result := Result{
		GroupBy: "state",
		Total:   2,
		Groups: []Group{{
			Key:   "open",
			Count: 2,
			Items: []GroupedItem{{Author: "Alice", Subject: "First"}, {Subject: "Second"}},
		}},
	}
	got := formatGroupedResult(result)
	if !strings.Contains(got, "  (unknown) (1): Second") {
		t.Errorf("missing (unknown) author bucket:\n%s", got)
	}
}

// TestTruncateStrings covers the count bound at n-1, n and n+1, and the
// per-string 50-character cap.
func TestTruncateStrings(t *testing.T) {
	ss := []string{"a", "b", "c", "d"}
	t.Run("n below length", func(t *testing.T) {
		if got := truncateStrings(ss, 3); len(got) != 3 || got[2] != "c" {
			t.Errorf("got %v, want the first 3", got)
		}
	})
	t.Run("n equal to length", func(t *testing.T) {
		if got := truncateStrings(ss, 4); len(got) != 4 || got[3] != "d" {
			t.Errorf("got %v, want all 4", got)
		}
	})
	t.Run("n above length", func(t *testing.T) {
		if got := truncateStrings(ss, 5); len(got) != 4 {
			t.Errorf("got %v, want all 4 with no padding", got)
		}
	})
	t.Run("n zero", func(t *testing.T) {
		if got := truncateStrings(ss, 0); len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})
	t.Run("empty input", func(t *testing.T) {
		if got := truncateStrings(nil, 3); len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})
	t.Run("per-string 50-char cap", func(t *testing.T) {
		for _, n := range []int{49, 50, 51} {
			got := truncateStrings([]string{strings.Repeat("x", n)}, 1)[0]
			if n <= 50 {
				if got != strings.Repeat("x", n) {
					t.Errorf("len %d: got %d chars, want untruncated", n, len(got))
				}
				continue
			}
			if got != strings.Repeat("x", 50)+"..." {
				t.Errorf("len %d: got %q, want 50 chars plus the ellipsis", n, got)
			}
		}
	})
}

// TestFormatDate covers every branch of the relative-time ladder and its
// boundaries.
func TestFormatDate(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{"under a minute", now.Add(-30 * time.Second), "just now"},
		{"exactly now", now, "just now"},
		{"future timestamp", now.Add(time.Hour), "just now"},
		{"one minute", now.Add(-time.Minute - time.Second), "1m ago"},
		{"minutes", now.Add(-45 * time.Minute), "45m ago"},
		{"last minute before an hour", now.Add(-59*time.Minute - 30*time.Second), "59m ago"},
		{"one hour", now.Add(-time.Hour - time.Second), "1h ago"},
		{"last hour before a day", now.Add(-23*time.Hour - 30*time.Minute), "23h ago"},
		{"one day", now.Add(-24*time.Hour - time.Second), "1d ago"},
		{"last day before a week", now.Add(-6*24*time.Hour - time.Hour), "6d ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatDate(tt.at); got != tt.want {
				t.Errorf("formatDate() = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("a week or older switches to an absolute date", func(t *testing.T) {
		at := time.Date(2020, 6, 5, 12, 0, 0, 0, time.UTC)
		if got := formatDate(at); got != "Jun 5, 2020" {
			t.Errorf("formatDate() = %q, want %q", got, "Jun 5, 2020")
		}
	})
	t.Run("exactly seven days is absolute", func(t *testing.T) {
		at := now.Add(-7 * 24 * time.Hour)
		if got := formatDate(at); got != at.Format("Jan 2, 2006") {
			t.Errorf("formatDate() = %q, want the absolute date %q", got, at.Format("Jan 2, 2006"))
		}
	})
}
