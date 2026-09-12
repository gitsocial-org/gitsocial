// group_test.go - Tests for search result grouping over a seeded cache
package search

import (
	"database/sql"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

// The search package sits under core and cannot import an extension, so the
// tables it LEFT JOINs are declared here from each extension's schema.go.
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

const releaseSchemaForSearchTest = `
CREATE TABLE IF NOT EXISTS release_items (
    repo_url TEXT NOT NULL, hash TEXT NOT NULL, branch TEXT NOT NULL,
    tag TEXT, version TEXT, prerelease INTEGER DEFAULT 0,
    artifacts TEXT, artifact_url TEXT, checksums TEXT, signed_by TEXT, sbom TEXT,
    PRIMARY KEY (repo_url, hash, branch)
);
`

const socialSchemaForSearchTest = `
CREATE TABLE IF NOT EXISTS social_items (
    repo_url TEXT NOT NULL, hash TEXT NOT NULL, branch TEXT NOT NULL,
    type TEXT NOT NULL,
    original_repo_url TEXT, original_hash TEXT, original_branch TEXT,
    reply_to_repo_url TEXT, reply_to_hash TEXT, reply_to_branch TEXT,
    PRIMARY KEY (repo_url, hash, branch)
);
`

// testRepoURL is the repository every seeded row belongs to.
const testRepoURL = "https://github.com/u/r"

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

// seedCommit writes one commit through cache.InsertCommits, the path that fills
// core_commits.labels and the core_labels linking table from the GitMsg header.
func seedCommit(t *testing.T, hash, authorName, authorEmail, subject, ext string, fields map[string]string, minutesAgo int) {
	t.Helper()
	message := protocol.FormatMessage(subject, protocol.Header{Ext: ext, V: "1", Fields: fields}, nil)
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: testRepoURL, Branch: "main",
		AuthorName: authorName, AuthorEmail: authorEmail,
		Message:   message,
		Timestamp: time.Date(2026, 3, 14, 12, 0, 0, 0, time.UTC).Add(-time.Duration(minutesAgo) * time.Minute),
	}}); err != nil {
		t.Fatalf("InsertCommits %s: %v", hash, err)
	}
}

// Corpus hashes, one per seeded item. Ordered newest first by timestamp.
const (
	hashIssueOpen   = "aaaaaaaaaaaa1111"
	hashIssueClosed = "bbbbbbbbbbbb2222"
	hashPullRequest = "cccccccccccc3333"
	hashRelease     = "dddddddddddd4444"
	hashPost        = "eeeeeeeeeeee5555"
	hashMilestone   = "ffffffffffff6666"
)

// seedCorpus fills a temp cache with one item per extension: two issues, a pull
// request, a release, a post and a milestone. Labels are authored on the commit
// header, so every item type carries them the way the fetch path writes them.
func seedCorpus(t *testing.T) {
	t.Helper()
	cache.RegisterSchema("pm", pmSchemaForSearchTest)
	cache.RegisterSchema("review", reviewSchemaForSearchTest)
	cache.RegisterSchema("release", releaseSchemaForSearchTest)
	cache.RegisterSchema("social", socialSchemaForSearchTest)
	testutil.OpenTempCache(t, "")

	seedCommit(t, hashMilestone, "Alice", "alice@test.com", "Release 1.0\n\nThe first stable cut.", "pm",
		map[string]string{"type": "milestone", "state": "open"}, 0)
	execTestSQL(t, `INSERT INTO pm_items (repo_url, hash, branch, type, state) VALUES (?, ?, 'main', 'milestone', 'open')`,
		testRepoURL, hashMilestone)

	seedCommit(t, hashIssueOpen, "Alice", "alice@test.com", "Broken parser", "pm",
		map[string]string{"type": "issue", "state": "open", "labels": "bug,ui"}, 1)
	execTestSQL(t, `INSERT INTO pm_items (repo_url, hash, branch, type, state, assignees, labels,
		milestone_repo_url, milestone_hash, milestone_branch)
		VALUES (?, ?, 'main', 'issue', 'open', 'alice@test.com,bob@test.com', 'bug,ui', ?, ?, 'main')`,
		testRepoURL, hashIssueOpen, testRepoURL, hashMilestone)

	seedCommit(t, hashIssueClosed, "Bob", "bob@test.com", "Slow startup", "pm",
		map[string]string{"type": "issue", "state": "closed", "labels": "bug"}, 2)
	execTestSQL(t, `INSERT INTO pm_items (repo_url, hash, branch, type, state, assignees, labels)
		VALUES (?, ?, 'main', 'issue', 'closed', 'bob@test.com', 'bug')`, testRepoURL, hashIssueClosed)

	seedCommit(t, hashPullRequest, "Alice", "alice@test.com", "Add the parser", "review",
		map[string]string{"type": "pull-request", "state": "open", "base": "main", "labels": "ui"}, 3)
	execTestSQL(t, `INSERT INTO review_items (repo_url, hash, branch, type, state, reviewers, base, head)
		VALUES (?, ?, 'main', 'pull-request', 'open', 'carol@test.com', 'main', 'feature')`, testRepoURL, hashPullRequest)

	seedCommit(t, hashRelease, "Bob", "bob@test.com", "Cut v1.0.0", "release",
		map[string]string{"tag": "v1.0.0", "labels": "ui"}, 4)
	execTestSQL(t, `INSERT INTO release_items (repo_url, hash, branch, tag, version)
		VALUES (?, ?, 'main', 'v1.0.0', '1.0.0')`, testRepoURL, hashRelease)

	seedCommit(t, hashPost, "Carol", "carol@test.com", "Hello world", "social",
		map[string]string{"type": "post", "labels": "bug"}, 5)
	execTestSQL(t, `INSERT INTO social_items (repo_url, hash, branch, type) VALUES (?, ?, 'main', 'post')`,
		testRepoURL, hashPost)
}

// corpusGroups runs the seeded corpus through the search query and groups it.
func corpusGroups(t *testing.T, field string) []Group {
	t.Helper()
	items, err := queryItems(searchQuery{RepoURL: testRepoURL})
	if err != nil {
		t.Fatalf("queryItems: %v", err)
	}
	if len(items) != 6 {
		t.Fatalf("queryItems returned %d rows, want the 6 seeded items", len(items))
	}
	scored := make([]ScoredItem, len(items))
	for i := range items {
		scored[i] = ScoredItem{Item: items[i], Score: 1}
	}
	return groupBy(scored, field, 0, false)
}

// keysAndCounts flattens groups to "key=count" pairs in the order returned.
func keysAndCounts(groups []Group) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.Key+"="+strconv.Itoa(g.Count))
	}
	return out
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

// TestSplitCSVOrNone covers empty, absent and whitespace-only CSV fields, which
// all collapse to the single "(none)" bucket.
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

// TestExtractGroupKeys checks that every field reads the column the search query
// already carries on the row, with "(none)" when the row does not carry it.
func TestExtractGroupKeys(t *testing.T) {
	full := ScoredItem{Item: Item{
		RepoURL:     testRepoURL,
		AuthorName:  "Alice Smith",
		AuthorEmail: "alice@test.com",
		Type:        "issue",
		Extension:   "pm",
		State:       "open",
		Labels:      "bug, ui",
		Assignees:   "alice@test.com,bob@test.com",
		Reviewers:   "carol@test.com",
		Base:        "main",
	}}
	tests := []struct {
		field string
		want  []string
	}{
		{"state", []string{"open"}},
		{"author", []string{"alice@test.com"}},
		{"type", []string{"issue"}},
		{"extension", []string{"pm"}},
		{"repo", []string{testRepoURL}},
		{"label", []string{"bug", "ui"}},
		{"assignee", []string{"alice@test.com", "bob@test.com"}},
		{"reviewer", []string{"carol@test.com"}},
		{"base", []string{"main"}},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if got := extractGroupKeys(full, tt.field); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractGroupKeys(%q) = %v, want %v", tt.field, got, tt.want)
			}
		})
	}

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

// TestGroupByEveryField walks every --group-by mode over one seeded corpus and
// pins the keys, the counts and the order the CLI prints them in.
func TestGroupByEveryField(t *testing.T) {
	seedCorpus(t)
	tests := []struct {
		field string
		want  []string
	}{
		{"label", []string{"bug=3", "ui=3", "(none)=1"}},
		{"state", []string{"open=3", "(none)=2", "closed=1"}},
		{"author", []string{"alice@test.com=3", "bob@test.com=2", "carol@test.com=1"}},
		{"type", []string{"issue=2", "milestone=1", "post=1", "pull-request=1", "v1.0.0=1"}},
		{"extension", []string{"pm=3", "release=1", "review=1", "social=1"}},
		{"repo", []string{testRepoURL + "=6"}},
		{"assignee", []string{"(none)=4", "bob@test.com=2", "alice@test.com=1"}},
		{"reviewer", []string{"(none)=5", "carol@test.com=1"}},
		{"base", []string{"(none)=5", "main=1"}},
		{"milestone", []string{"(none)=5", "Release 1.0=1"}},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if got := keysAndCounts(corpusGroups(t, tt.field)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("group-by %s = %v, want %v", tt.field, got, tt.want)
			}
		})
	}
}

// TestGroupByLabelReachesEveryItemType checks that a release and a post carrying
// labels group under those labels instead of falling into "(none)".
func TestGroupByLabelReachesEveryItemType(t *testing.T) {
	seedCorpus(t)
	groups := corpusGroups(t, "label")
	members := make(map[string][]string)
	for _, g := range groups {
		for _, item := range g.Items {
			members[g.Key] = append(members[g.Key], item.Hash)
		}
	}
	if !containsHash(members["ui"], hashRelease) {
		t.Errorf("release is not in the ui group: %v", members["ui"])
	}
	if !containsHash(members["bug"], hashPost) {
		t.Errorf("post is not in the bug group: %v", members["bug"])
	}
	if containsHash(members["(none)"], hashRelease) || containsHash(members["(none)"], hashPost) {
		t.Errorf("a labelled item fell into (none): %v", members["(none)"])
	}
}

// containsHash reports whether the short hashes hold the given full hash.
func containsHash(short []string, full string) bool {
	for _, s := range short {
		if s == full[:12] {
			return true
		}
	}
	return false
}

// TestGroupByTieBreakIsStable checks that groups of equal count come back in
// name order, and in the same order on a second run.
func TestGroupByTieBreakIsStable(t *testing.T) {
	seedCorpus(t)
	first := keysAndCounts(corpusGroups(t, "label"))
	second := keysAndCounts(corpusGroups(t, "label"))
	if !reflect.DeepEqual(first, second) {
		t.Errorf("two runs disagree: %v then %v", first, second)
	}
	if !reflect.DeepEqual(first, []string{"bug=3", "ui=3", "(none)=1"}) {
		t.Errorf("label groups = %v, want bug and ui tied in name order", first)
	}

	t.Run("ties among many groups", func(t *testing.T) {
		items := []ScoredItem{
			{Item: Item{Hash: "aaaaaaaaaaaaaaaa", Type: "zulu"}},
			{Item: Item{Hash: "bbbbbbbbbbbbbbbb", Type: "alpha"}},
			{Item: Item{Hash: "cccccccccccccccc", Type: "mike"}},
			{Item: Item{Hash: "dddddddddddddddd", Type: "alpha"}},
			{Item: Item{Hash: "eeeeeeeeeeeeeeee", Type: "bravo"}},
		}
		want := []string{"alpha=2", "bravo=1", "mike=1", "zulu=1"}
		for run := 0; run < 3; run++ {
			if got := keysAndCounts(groupBy(items, "type", 0, true)); !reflect.DeepEqual(got, want) {
				t.Fatalf("run %d = %v, want %v", run, got, want)
			}
		}
	})

	t.Run("(none) sorts by name like any other key", func(t *testing.T) {
		items := []ScoredItem{
			{Item: Item{Hash: "aaaaaaaaaaaaaaaa", Type: "zulu"}},
			{Item: Item{Hash: "bbbbbbbbbbbbbbbb"}},
		}
		want := []string{"(none)=1", "zulu=1"}
		if got := keysAndCounts(groupBy(items, "type", 0, true)); !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
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
		{Item: Item{Hash: "aaaaaaaaaaaaaaaa", Labels: "bug,ui"}},
		{Item: Item{Hash: "bbbbbbbbbbbbbbbb", Labels: "bug"}},
		{Item: Item{Hash: "cccccccccccccccc"}},
	}
	got := keysAndCounts(groupBy(items, "label", 0, false))
	want := []string{"bug=2", "(none)=1", "ui=1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
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
		t.Errorf("Count = %d, want 3, the full membership", groups[0].Count)
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
		RepoURL:    testRepoURL,
		Hash:       "0123456789abcdef0123456789abcdef01234567",
		AuthorName: "Alice Smith",
		Content:    "  Fix the parser\n\nBody line that must not appear.  ",
		Timestamp:  ts,
		State:      "open",
		Labels:     "bug,ui",
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
	if gi.Author != "Alice Smith" || gi.State != "open" || gi.Labels != "bug,ui" || gi.RepoURL != testRepoURL {
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

	t.Run("long subject truncated at 100", func(t *testing.T) {
		long := ScoredItem{Item: Item{
			Hash: "0123456789abcdef", Timestamp: ts,
			Content: strings.Repeat("x", 120),
		}}
		got := toGroupedItem(long, "type").Subject
		if len(got) != 103 {
			t.Errorf("len(Subject) = %d, want 103, 100 chars plus the ellipsis", len(got))
		}
		if got[100:] != "..." {
			t.Errorf("Subject tail = %q, want ...", got[100:])
		}
	})
}
