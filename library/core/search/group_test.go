// group_test.go - Tests for search result grouping and its DB-backed enrichment
package search

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

// pmSchemaForSearchTest mirrors library/extensions/pm/schema.go. The search
// package sits under core and cannot import an extension, so the DDL is copied.
const pmSchemaForSearchTest = `
CREATE TABLE IF NOT EXISTS pm_items (
    repo_url TEXT NOT NULL, hash TEXT NOT NULL, branch TEXT NOT NULL,
    type TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'open',
    assignees TEXT, due TEXT, start_date TEXT, end_date TEXT,
    milestone_repo_url TEXT, milestone_hash TEXT, milestone_branch TEXT,
    sprint_repo_url TEXT, sprint_hash TEXT, sprint_branch TEXT,
    parent_repo_url TEXT, parent_hash TEXT, parent_branch TEXT,
    root_repo_url TEXT, root_hash TEXT, root_branch TEXT, labels TEXT,
    PRIMARY KEY (repo_url, hash, branch)
);
`

// reviewSchemaForSearchTest mirrors library/extensions/review/schema.go. Note
// there is deliberately no `labels` column: PR labels live on
// core_commits.labels, which is what review_items_resolved reads.
const reviewSchemaForSearchTest = `
CREATE TABLE IF NOT EXISTS review_items (
    repo_url TEXT NOT NULL, hash TEXT NOT NULL, branch TEXT NOT NULL,
    type TEXT NOT NULL, state TEXT, draft INTEGER DEFAULT 0,
    base TEXT, base_tip TEXT, head TEXT, head_tip TEXT,
    closes TEXT, reviewers TEXT,
    pull_request_repo_url TEXT, pull_request_hash TEXT, pull_request_branch TEXT,
    commit_ref TEXT, file TEXT,
    old_line INTEGER, new_line INTEGER, old_line_end INTEGER, new_line_end INTEGER,
    review_state TEXT, suggestion INTEGER DEFAULT 0,
    PRIMARY KEY (repo_url, hash, branch)
);
`

// openCacheWithExtensions opens a temp cache carrying the pm and review tables
// the enrichment queries read.
func openCacheWithExtensions(t *testing.T) {
	t.Helper()
	cache.RegisterSchema("pm", pmSchemaForSearchTest)
	cache.RegisterSchema("review", reviewSchemaForSearchTest)
	testutil.OpenTempCache(t, "")
}

// testRepoURL is the workspace URL every seeded row in this file belongs to.
const testRepoURL = "https://github.com/u/r"

// seedCommit inserts one core_commits row and optionally sets its labels.
func seedCommit(t *testing.T, hash, message, labels string) {
	t.Helper()
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: testRepoURL, Branch: "main",
		AuthorName: "Test User", AuthorEmail: "test@test.com",
		Message: message, Timestamp: time.Now(),
	}}); err != nil {
		t.Fatalf("InsertCommits: %v", err)
	}
	if labels == "" {
		return
	}
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE core_commits SET labels = ? WHERE repo_url = ? AND hash = ?`, labels, testRepoURL, hash)
		return err
	}); err != nil {
		t.Fatalf("set labels: %v", err)
	}
}

// execTestSQL runs one statement against the open test cache.
func execTestSQL(t *testing.T, query string, args ...interface{}) {
	t.Helper()
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(query, args...)
		return err
	}); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// scored builds a ScoredItem with the composite key set, for grouping tests.
func scored(hash string) ScoredItem {
	return ScoredItem{Item: Item{RepoURL: testRepoURL, Hash: hash, Branch: "main"}}
}

// TestIsValidGroupBy pins the accepted --group-by fields to the set CLI.md
// documents, in both directions.
func TestIsValidGroupBy(t *testing.T) {
	documented := []string{"state", "author", "type", "extension", "repo", "label", "assignee", "reviewer", "milestone", "base"}
	for _, field := range documented {
		if !IsValidGroupBy(field) {
			t.Errorf("IsValidGroupBy(%q) = false, CLI.md documents it as valid", field)
		}
	}
	if len(validGroupByFields) != len(documented) {
		t.Errorf("validGroupByFields has %d entries, CLI.md documents %d", len(validGroupByFields), len(documented))
	}
	for _, field := range []string{"", "State", "STATE", "sprint", "tag", "draft", "labels", "assignees"} {
		if IsValidGroupBy(field) {
			t.Errorf("IsValidGroupBy(%q) = true, want false", field)
		}
	}
}

// TestNeedsEnrichment checks that exactly the fields not carried on the search
// row itself are the ones marked as needing a database round trip.
func TestNeedsEnrichment(t *testing.T) {
	tests := map[string]bool{
		"state": true, "label": true, "assignee": true,
		"reviewer": true, "base": true, "milestone": true,
		"author": false, "type": false, "extension": false, "repo": false,
		"": false, "nonsense": false,
	}
	for field, want := range tests {
		if got := needsEnrichment(field); got != want {
			t.Errorf("needsEnrichment(%q) = %v, want %v", field, got, want)
		}
	}
}

// TestSplitCSVOrNone covers empty, absent and whitespace-only CSV fields, which
// all have to collapse to the single "(none)" bucket.
func TestSplitCSVOrNone(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", []string{"(none)"}},
		{"single", "bug", []string{"bug"}},
		{"two", "bug,ui", []string{"bug", "ui"}},
		{"padded", " bug , ui ", []string{"bug", "ui"}},
		{"empty middle element", "bug,,ui", []string{"bug", "ui"}},
		{"trailing comma", "bug,", []string{"bug"}},
		{"only separators", ",,,", []string{"(none)"}},
		{"only whitespace", "   ", []string{"(none)"}},
		{"whitespace between separators", " , ", []string{"(none)"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := splitCSVOrNone(tt.input); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitCSVOrNone(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestExtractGroupKeys checks the key each field reads, the "(none)" fallback
// when the item does not carry that field, and multi-key CSV fields.
func TestExtractGroupKeys(t *testing.T) {
	full := ScoredItem{Item: Item{
		RepoURL:        "https://github.com/u/r",
		AuthorName:     "Alice Smith",
		AuthorEmail:    "alice@test.com",
		Type:           "issue",
		Extension:      "pm",
		State:          "open",
		groupState:     "closed",
		groupLabels:    "bug, ui",
		groupAssignees: "alice@test.com,bob@test.com",
		groupReviewers: "carol@test.com",
		groupBase:      "main",
		groupMilestone: "v1.0",
	}}
	tests := []struct {
		field string
		want  []string
	}{
		{"state", []string{"open"}},
		{"author", []string{"alice@test.com"}},
		{"type", []string{"issue"}},
		{"extension", []string{"pm"}},
		{"repo", []string{"https://github.com/u/r"}},
		{"label", []string{"bug", "ui"}},
		{"assignee", []string{"alice@test.com", "bob@test.com"}},
		{"reviewer", []string{"carol@test.com"}},
		{"milestone", []string{"v1.0"}},
		{"base", []string{"main"}},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if got := extractGroupKeys(full, tt.field); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractGroupKeys(%q) = %v, want %v", tt.field, got, tt.want)
			}
		})
	}

	t.Run("state falls back to enriched state", func(t *testing.T) {
		item := ScoredItem{Item: Item{groupState: "merged"}}
		if got := extractGroupKeys(item, "state"); !reflect.DeepEqual(got, []string{"merged"}) {
			t.Errorf("got %v, want [merged]", got)
		}
	})

	t.Run("field the item does not carry", func(t *testing.T) {
		empty := ScoredItem{}
		for _, field := range []string{"state", "author", "type", "extension", "repo", "label", "assignee", "reviewer", "milestone", "base"} {
			if got := extractGroupKeys(empty, field); !reflect.DeepEqual(got, []string{"(none)"}) {
				t.Errorf("extractGroupKeys(empty, %q) = %v, want [(none)]", field, got)
			}
		}
	})

	t.Run("unknown field buckets everything under (none)", func(t *testing.T) {
		if got := extractGroupKeys(full, "sprint"); !reflect.DeepEqual(got, []string{"(none)"}) {
			t.Errorf("got %v, want [(none)]", got)
		}
	})

	t.Run("author groups by email not display name", func(t *testing.T) {
		item := ScoredItem{Item: Item{AuthorName: "Alice Smith"}}
		if got := extractGroupKeys(item, "author"); !reflect.DeepEqual(got, []string{"(none)"}) {
			t.Errorf("got %v, want [(none)]: grouping keys on AuthorEmail", got)
		}
	})
}

// TestGroupByCountsAndOrder checks that groups carry the full member count and
// come back ordered by count, descending.
func TestGroupByCountsAndOrder(t *testing.T) {
	items := []ScoredItem{
		{Item: Item{Type: "issue", Hash: "aaaaaaaaaaaaaaaa"}},
		{Item: Item{Type: "pr", Hash: "bbbbbbbbbbbbbbbb"}},
		{Item: Item{Type: "issue", Hash: "cccccccccccccccc"}},
		{Item: Item{Type: "issue", Hash: "dddddddddddddddd"}},
		{Item: Item{Type: "pr", Hash: "eeeeeeeeeeeeeeee"}},
		{Item: Item{Hash: "ffffffffffffffff"}},
	}
	groups := groupBy(items, "type", 0, false)
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(groups))
	}
	wantKeys := []string{"issue", "pr", "(none)"}
	wantCounts := []int{3, 2, 1}
	for i := range groups {
		if groups[i].Key != wantKeys[i] || groups[i].Count != wantCounts[i] {
			t.Errorf("group %d = %q/%d, want %q/%d", i, groups[i].Key, groups[i].Count, wantKeys[i], wantCounts[i])
		}
		if len(groups[i].Items) != groups[i].Count {
			t.Errorf("group %q: %d items for count %d", groups[i].Key, len(groups[i].Items), groups[i].Count)
		}
	}
}

// TestGroupByEmptyInput checks that no items produce no groups.
func TestGroupByEmptyInput(t *testing.T) {
	if groups := groupBy(nil, "state", 0, false); len(groups) != 0 {
		t.Errorf("got %d groups for no items, want 0", len(groups))
	}
}

// TestGroupByMultiValued checks that a multi-label item is counted in every
// group it belongs to, so group counts can exceed the result total.
func TestGroupByMultiValued(t *testing.T) {
	items := []ScoredItem{
		{Item: Item{Hash: "aaaaaaaaaaaaaaaa", groupLabels: "bug,ui"}},
		{Item: Item{Hash: "bbbbbbbbbbbbbbbb", groupLabels: "bug"}},
		{Item: Item{Hash: "cccccccccccccccc"}},
	}
	groups := groupBy(items, "label", 0, false)
	counts := map[string]int{}
	total := 0
	for _, g := range groups {
		counts[g.Key] = g.Count
		total += g.Count
	}
	want := map[string]int{"bug": 2, "ui": 1, "(none)": 1}
	if !reflect.DeepEqual(counts, want) {
		t.Errorf("counts = %v, want %v", counts, want)
	}
	if total != 4 {
		t.Errorf("sum of group counts = %d, want 4 for 3 items with one in two groups", total)
	}
}

// TestGroupByTop checks that --top caps the items rendered per group while the
// group count still reports every member.
func TestGroupByTop(t *testing.T) {
	items := []ScoredItem{
		{Item: Item{Type: "issue", Hash: "aaaaaaaaaaaaaaaa", Content: "first"}},
		{Item: Item{Type: "issue", Hash: "bbbbbbbbbbbbbbbb", Content: "second"}},
		{Item: Item{Type: "issue", Hash: "cccccccccccccccc", Content: "third"}},
	}
	groups := groupBy(items, "type", 2, false)
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	if groups[0].Count != 3 {
		t.Errorf("Count = %d, want 3 (the full membership, not the capped list)", groups[0].Count)
	}
	if len(groups[0].Items) != 2 {
		t.Fatalf("got %d items, want 2", len(groups[0].Items))
	}
	if groups[0].Items[0].Subject != "first" || groups[0].Items[1].Subject != "second" {
		t.Errorf("kept %q,%q; want the first two in input order", groups[0].Items[0].Subject, groups[0].Items[1].Subject)
	}

	t.Run("top above membership keeps everything", func(t *testing.T) {
		g := groupBy(items, "type", 10, false)
		if len(g[0].Items) != 3 {
			t.Errorf("got %d items, want 3", len(g[0].Items))
		}
	})
	t.Run("top zero means unlimited", func(t *testing.T) {
		g := groupBy(items, "type", 0, false)
		if len(g[0].Items) != 3 {
			t.Errorf("got %d items, want 3", len(g[0].Items))
		}
	})
}

// TestGroupByCountOnly checks that --count-only reports counts with no items.
func TestGroupByCountOnly(t *testing.T) {
	items := []ScoredItem{
		{Item: Item{Type: "issue", Hash: "aaaaaaaaaaaaaaaa"}},
		{Item: Item{Type: "issue", Hash: "bbbbbbbbbbbbbbbb"}},
	}
	groups := groupBy(items, "type", 0, true)
	if len(groups) != 1 || groups[0].Count != 2 {
		t.Fatalf("groups = %+v, want one group of 2", groups)
	}
	if groups[0].Items != nil {
		t.Errorf("Items = %+v, want nil under --count-only", groups[0].Items)
	}
}

// TestToGroupedItem checks the compact per-item projection: short hash, subject
// line, date, and the context fields dropped when they are the group key.
func TestToGroupedItem(t *testing.T) {
	ts := time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)
	item := ScoredItem{Item: Item{
		RepoURL:     "https://github.com/u/r",
		Hash:        "0123456789abcdef0123456789abcdef01234567",
		AuthorName:  "Alice Smith",
		Content:     "  Fix the parser\n\nBody line that must not appear.  ",
		Timestamp:   ts,
		State:       "open",
		groupLabels: "bug,ui",
	}}

	gi := toGroupedItem(item, "type")
	if gi.Hash != "0123456789ab" {
		t.Errorf("Hash = %q, want the 12-char prefix", gi.Hash)
	}
	if gi.Subject != "Fix the parser" {
		t.Errorf("Subject = %q, want the trimmed first line only", gi.Subject)
	}
	if gi.Timestamp != "2026-03-14" {
		t.Errorf("Timestamp = %q, want 2026-03-14", gi.Timestamp)
	}
	if gi.Author != "Alice Smith" || gi.State != "open" || gi.Labels != "bug,ui" || gi.RepoURL != "https://github.com/u/r" {
		t.Errorf("context fields = %+v, want all four populated when grouping by type", gi)
	}

	t.Run("group field omitted from context", func(t *testing.T) {
		if got := toGroupedItem(item, "author"); got.Author != "" {
			t.Errorf("Author = %q, want empty when grouping by author", got.Author)
		}
		if got := toGroupedItem(item, "state"); got.State != "" {
			t.Errorf("State = %q, want empty when grouping by state", got.State)
		}
		if got := toGroupedItem(item, "label"); got.Labels != "" {
			t.Errorf("Labels = %q, want empty when grouping by label", got.Labels)
		}
		if got := toGroupedItem(item, "repo"); got.RepoURL != "" {
			t.Errorf("RepoURL = %q, want empty when grouping by repo", got.RepoURL)
		}
	})

	t.Run("state falls back to enriched state", func(t *testing.T) {
		enriched := ScoredItem{Item: Item{
			Hash: "0123456789abcdef", Timestamp: ts, groupState: "merged",
		}}
		if got := toGroupedItem(enriched, "type"); got.State != "merged" {
			t.Errorf("State = %q, want merged", got.State)
		}
	})

	t.Run("long subject truncated at 100", func(t *testing.T) {
		long := ScoredItem{Item: Item{
			Hash: "0123456789abcdef", Timestamp: ts,
			Content: strings100Plus(),
		}}
		got := toGroupedItem(long, "type").Subject
		if len(got) != 103 {
			t.Errorf("len(Subject) = %d, want 103 (100 chars plus the ellipsis)", len(got))
		}
		if got[100:] != "..." {
			t.Errorf("Subject tail = %q, want ...", got[100:])
		}
	})
}

// strings100Plus returns a 120-character single-line subject.
func strings100Plus() string {
	s := ""
	for i := 0; i < 120; i++ {
		s += "x"
	}
	return s
}

// TestBuildHashFilter checks the IN clause is scoped to the distinct hashes of
// the result set, deduplicated across branches.
func TestBuildHashFilter(t *testing.T) {
	keyIndex := map[itemKey][]int{
		{"https://a", "hash1", "main"}:    {0},
		{"https://a", "hash1", "feature"}: {1},
		{"https://b", "hash2", "main"}:    {2},
	}
	clause, args := buildHashFilter(keyIndex)
	if clause != "hash IN (?,?)" {
		t.Errorf("clause = %q, want hash IN (?,?) for 2 distinct hashes", clause)
	}
	if len(args) != 2 {
		t.Fatalf("args = %v, want 2", args)
	}
	seen := map[interface{}]bool{args[0]: true, args[1]: true}
	if !seen["hash1"] || !seen["hash2"] {
		t.Errorf("args = %v, want hash1 and hash2", args)
	}

	t.Run("single hash", func(t *testing.T) {
		clause, args := buildHashFilter(map[itemKey][]int{{"https://a", "h", "main"}: {0}})
		if clause != "hash IN (?)" || len(args) != 1 {
			t.Errorf("clause = %q args = %v, want hash IN (?) with 1 arg", clause, args)
		}
	})
}

// TestEnrichForGroupingPM checks that issue state, labels and assignees are read
// back onto the result set from pm_items.
func TestEnrichForGroupingPM(t *testing.T) {
	openCacheWithExtensions(t)
	seedCommit(t, "issuehash0001", "Broken parser", "")
	execTestSQL(t, `INSERT INTO pm_items (repo_url, hash, branch, type, state, assignees, labels)
		VALUES (?, ?, 'main', 'issue', 'closed', 'alice@test.com,bob@test.com', 'bug,ui')`, testRepoURL, "issuehash0001")

	for _, field := range []string{"state", "label", "assignee"} {
		items := []ScoredItem{scored("issuehash0001")}
		enrichForGrouping(items, field)
		if items[0].groupState != "closed" {
			t.Errorf("%s: groupState = %q, want closed", field, items[0].groupState)
		}
		if items[0].groupLabels != "bug,ui" {
			t.Errorf("%s: groupLabels = %q, want bug,ui", field, items[0].groupLabels)
		}
		if items[0].groupAssignees != "alice@test.com,bob@test.com" {
			t.Errorf("%s: groupAssignees = %q", field, items[0].groupAssignees)
		}
	}

	t.Run("grouping end to end", func(t *testing.T) {
		items := []ScoredItem{scored("issuehash0001")}
		enrichForGrouping(items, "label")
		groups := groupBy(items, "label", 0, true)
		keys := make([]string, 0, len(groups))
		for _, g := range groups {
			keys = append(keys, g.Key)
		}
		if !reflect.DeepEqual(keys, []string{"bug", "ui"}) && !reflect.DeepEqual(keys, []string{"ui", "bug"}) {
			t.Errorf("group keys = %v, want bug and ui", keys)
		}
	})
}

// TestEnrichForGroupingSkipsUnneededFields checks that fields carried on the
// search row itself never touch the database.
func TestEnrichForGroupingSkipsUnneededFields(t *testing.T) {
	openCacheWithExtensions(t)
	seedCommit(t, "issuehash0002", "Broken parser", "")
	execTestSQL(t, `INSERT INTO pm_items (repo_url, hash, branch, type, state, labels)
		VALUES (?, ?, 'main', 'issue', 'closed', 'bug')`, testRepoURL, "issuehash0002")

	for _, field := range []string{"author", "type", "extension", "repo"} {
		items := []ScoredItem{scored("issuehash0002")}
		enrichForGrouping(items, field)
		if items[0].groupState != "" || items[0].groupLabels != "" {
			t.Errorf("%s: enrichment ran, groupState = %q groupLabels = %q", field, items[0].groupState, items[0].groupLabels)
		}
	}
}

// TestEnrichForGroupingReview checks that PR state, labels, reviewers and base
// branch are read back onto the result set. Labels for a PR live on
// core_commits.labels, the same source review_items_resolved reads.
func TestEnrichForGroupingReview(t *testing.T) {
	openCacheWithExtensions(t)
	seedCommit(t, "prhash00000001", "Add the parser", "bug,ui")
	execTestSQL(t, `INSERT INTO review_items (repo_url, hash, branch, type, state, reviewers, base)
		VALUES (?, ?, 'main', 'pull-request', 'open', 'carol@test.com', 'main')`, testRepoURL, "prhash00000001")

	for _, field := range []string{"state", "label", "reviewer", "base"} {
		items := []ScoredItem{scored("prhash00000001")}
		enrichForGrouping(items, field)
		switch field {
		case "state":
			if items[0].groupState != "open" {
				t.Errorf("state: groupState = %q, want open", items[0].groupState)
			}
		case "label":
			if items[0].groupLabels != "bug,ui" {
				t.Errorf("label: groupLabels = %q, want bug,ui", items[0].groupLabels)
			}
		case "reviewer":
			if items[0].groupReviewers != "carol@test.com" {
				t.Errorf("reviewer: groupReviewers = %q, want carol@test.com", items[0].groupReviewers)
			}
		case "base":
			if items[0].groupBase != "main" {
				t.Errorf("base: groupBase = %q, want main", items[0].groupBase)
			}
		}
	}

	t.Run("feedback rows are not pull requests", func(t *testing.T) {
		seedCommit(t, "fbhash00000001", "Looks good", "")
		execTestSQL(t, `INSERT INTO review_items (repo_url, hash, branch, type, state, base)
			VALUES (?, ?, 'main', 'feedback', 'approved', 'main')`, testRepoURL, "fbhash00000001")
		items := []ScoredItem{scored("fbhash00000001")}
		enrichForGrouping(items, "base")
		if items[0].groupBase != "" {
			t.Errorf("groupBase = %q, want empty: only pull-request rows are enriched", items[0].groupBase)
		}
	})
}

// TestEnrichMilestoneNames checks that an issue's milestone composite ref is
// resolved to the milestone's subject line for display as a group key.
func TestEnrichMilestoneNames(t *testing.T) {
	openCacheWithExtensions(t)
	seedCommit(t, "mshash00000001", "Release 1.0\n\nThe first stable cut.", "")
	seedCommit(t, "issuehash0003", "Broken parser", "")
	execTestSQL(t, `INSERT INTO pm_items (repo_url, hash, branch, type, state, milestone_repo_url, milestone_hash, milestone_branch)
		VALUES (?, ?, 'main', 'issue', 'open', ?, ?, 'main')`, testRepoURL, "issuehash0003", testRepoURL, "mshash00000001")

	items := []ScoredItem{scored("issuehash0003")}
	enrichForGrouping(items, "milestone")
	if items[0].groupMilestone != "Release 1.0" {
		t.Errorf("groupMilestone = %q, want the milestone subject %q", items[0].groupMilestone, "Release 1.0")
	}
	if got := extractGroupKeys(items[0], "milestone"); !reflect.DeepEqual(got, []string{"Release 1.0"}) {
		t.Errorf("group key = %v, want [Release 1.0]", got)
	}

	t.Run("issue with no milestone", func(t *testing.T) {
		seedCommit(t, "issuehash0004", "No milestone here", "")
		execTestSQL(t, `INSERT INTO pm_items (repo_url, hash, branch, type, state)
			VALUES (?, ?, 'main', 'issue', 'open')`, testRepoURL, "issuehash0004")
		items := []ScoredItem{scored("issuehash0004")}
		enrichForGrouping(items, "milestone")
		if got := extractGroupKeys(items[0], "milestone"); !reflect.DeepEqual(got, []string{"(none)"}) {
			t.Errorf("group key = %v, want [(none)]", got)
		}
	})
}

// TestEnrichForGroupingNoItems checks the no-op guard on an empty result set.
func TestEnrichForGroupingNoItems(t *testing.T) {
	openCacheWithExtensions(t)
	enrichForGrouping(nil, "state")
	enrichForGrouping([]ScoredItem{}, "label")
}
