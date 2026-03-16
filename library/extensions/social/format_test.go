// format_test.go - Tests for text formatting functions
package social

import (
	"strings"
	"testing"
	"time"
)

func TestFormatDate_justNow(t *testing.T) {
	got := formatDate(time.Now())
	if got != "just now" {
		t.Errorf("formatDate(now) = %q, want %q", got, "just now")
	}
}

func TestFormatDate_minutes(t *testing.T) {
	got := formatDate(time.Now().Add(-5 * time.Minute))
	if got != "5m ago" {
		t.Errorf("formatDate(-5m) = %q, want %q", got, "5m ago")
	}
}

func TestFormatDate_hours(t *testing.T) {
	got := formatDate(time.Now().Add(-3 * time.Hour))
	if got != "3h ago" {
		t.Errorf("formatDate(-3h) = %q, want %q", got, "3h ago")
	}
}

func TestFormatDate_days(t *testing.T) {
	got := formatDate(time.Now().Add(-3 * 24 * time.Hour))
	if got != "3d ago" {
		t.Errorf("formatDate(-3d) = %q, want %q", got, "3d ago")
	}
}

func TestFormatDate_oldDate(t *testing.T) {
	ts := time.Date(2020, 1, 15, 0, 0, 0, 0, time.UTC)
	got := formatDate(ts)
	if got != "Jan 15, 2020" {
		t.Errorf("formatDate(old) = %q, want %q", got, "Jan 15, 2020")
	}
}

func TestFormatPost_quotes(t *testing.T) {
	post := Post{
		Author:    Author{Name: "Alice"},
		Timestamp: time.Now(),
		Content:   "Quoted post",
		Interactions: Interactions{
			Quotes: 3,
		},
	}
	got := FormatPost(post)
	if !strings.Contains(got, "3 quotes") {
		t.Error("FormatPost should contain quote count")
	}
}

func TestFormatPost(t *testing.T) {
	post := Post{
		ID:         "abc123",
		Repository: "https://github.com/user/repo",
		Author:     Author{Name: "Alice"},
		Timestamp:  time.Now().Add(-30 * time.Minute),
		Content:    "Hello world",
		Interactions: Interactions{
			Comments: 5,
			Reposts:  2,
		},
		Display: Display{
			RepositoryName: "user/repo",
			CommitHash:     "abc123",
		},
	}

	got := FormatPost(post)
	if !strings.Contains(got, "Alice") {
		t.Error("FormatPost should contain author name")
	}
	if !strings.Contains(got, "Hello world") {
		t.Error("FormatPost should contain content")
	}
	if !strings.Contains(got, "5 comments") {
		t.Error("FormatPost should contain comment count")
	}
	if !strings.Contains(got, "2 reposts") {
		t.Error("FormatPost should contain repost count")
	}
	if !strings.Contains(got, "user/repo") {
		t.Error("FormatPost should contain repo name")
	}
}

func TestFormatPost_edited(t *testing.T) {
	post := Post{
		Author:    Author{Name: "Alice"},
		Timestamp: time.Now(),
		Content:   "Edited post",
		IsEdited:  true,
	}

	got := FormatPost(post)
	if !strings.Contains(got, "(edited)") {
		t.Error("FormatPost should show (edited) marker")
	}
}

func TestFormatPost_retracted(t *testing.T) {
	post := Post{
		Author:      Author{Name: "Alice"},
		Timestamp:   time.Now(),
		Content:     "Should not appear",
		IsRetracted: true,
	}

	got := FormatPost(post)
	if !strings.Contains(got, "[retracted]") {
		t.Error("FormatPost should show [retracted]")
	}
	if strings.Contains(got, "Should not appear") {
		t.Error("FormatPost should not show content for retracted posts")
	}
}

func TestFormatPost_longContent(t *testing.T) {
	long := strings.Repeat("a", 300)
	post := Post{
		Author:    Author{Name: "Alice"},
		Timestamp: time.Now(),
		Content:   long,
	}

	got := FormatPost(post)
	if !strings.Contains(got, "...") {
		t.Error("FormatPost should truncate long content with ellipsis")
	}
}

func TestFormatPost_fallbackDisplay(t *testing.T) {
	post := Post{
		ID:         "abc123",
		Repository: "https://github.com/user/repo",
		Author:     Author{Name: "Alice"},
		Timestamp:  time.Now(),
		Content:    "Test",
	}

	got := FormatPost(post)
	if !strings.Contains(got, "https://github.com/user/repo") {
		t.Error("FormatPost should fall back to Repository when Display.RepositoryName is empty")
	}
	if !strings.Contains(got, "abc123") {
		t.Error("FormatPost should fall back to ID when Display.CommitHash is empty")
	}
}

func TestFormatTimeline(t *testing.T) {
	posts := []Post{
		{Author: Author{Name: "Alice"}, Timestamp: time.Now(), Content: "First"},
		{Author: Author{Name: "Bob"}, Timestamp: time.Now(), Content: "Second"},
	}

	got := FormatTimeline(posts)
	if !strings.Contains(got, "Alice") {
		t.Error("FormatTimeline should contain first author")
	}
	if !strings.Contains(got, "Bob") {
		t.Error("FormatTimeline should contain second author")
	}
	if !strings.Contains(got, "---") {
		t.Error("FormatTimeline should contain separator")
	}
}

func TestFormatTimeline_empty(t *testing.T) {
	got := FormatTimeline(nil)
	if got != "No posts found." {
		t.Errorf("FormatTimeline(nil) = %q, want %q", got, "No posts found.")
	}
}

func TestFormatList(t *testing.T) {
	list := List{
		ID:           "my-list",
		Name:         "My List",
		Repositories: []string{"repo1", "repo2"},
	}

	got := FormatList(list)
	if !strings.Contains(got, "my-list (My List)") {
		t.Error("FormatList should contain ID and name")
	}
	if !strings.Contains(got, "2 repositories") {
		t.Error("FormatList should contain repo count")
	}
}

func TestFormatList_followed(t *testing.T) {
	list := List{
		ID:                "my-list",
		Name:              "Followed",
		IsFollowedLocally: true,
	}

	got := FormatList(list)
	if !strings.Contains(got, "[followed]") {
		t.Error("FormatList should show [followed] for locally followed lists")
	}
}

func TestFormatList_unpushed(t *testing.T) {
	list := List{
		ID:         "my-list",
		Name:       "Unpushed",
		IsUnpushed: true,
	}

	got := FormatList(list)
	if !strings.Contains(got, "[⇡]") {
		t.Error("FormatList should show [⇡] for unpushed lists")
	}
}

func TestFormatLists_empty(t *testing.T) {
	got := FormatLists(nil)
	if got != "No lists found." {
		t.Errorf("FormatLists(nil) = %q", got)
	}
}

func TestFormatLists_nonEmpty(t *testing.T) {
	lists := []List{
		{ID: "list-a", Name: "Alpha", Repositories: []string{"repo1"}},
		{ID: "list-b", Name: "Beta", Repositories: []string{"repo2", "repo3"}},
	}
	got := FormatLists(lists)
	if !strings.Contains(got, "list-a (Alpha)") {
		t.Error("FormatLists should contain first list")
	}
	if !strings.Contains(got, "list-b (Beta)") {
		t.Error("FormatLists should contain second list")
	}
	if !strings.Contains(got, "1 repositories") {
		t.Error("FormatLists should contain first list repo count")
	}
	if !strings.Contains(got, "2 repositories") {
		t.Error("FormatLists should contain second list repo count")
	}
}

func TestFormatRepository(t *testing.T) {
	repo := Repository{
		Name:   "user/repo",
		URL:    "https://github.com/user/repo",
		Branch: "main",
		Lists:  []string{"list1", "list2"},
	}

	got := FormatRepository(repo)
	if !strings.Contains(got, "user/repo") {
		t.Error("FormatRepository should contain name")
	}
	if !strings.Contains(got, "https://github.com/user/repo") {
		t.Error("FormatRepository should contain URL")
	}
	if !strings.Contains(got, "branch: main") {
		t.Error("FormatRepository should contain branch")
	}
	if !strings.Contains(got, "lists: list1, list2") {
		t.Error("FormatRepository should contain lists")
	}
}

func TestFormatRepositories_empty(t *testing.T) {
	got := FormatRepositories(nil)
	if got != "No repositories found." {
		t.Errorf("FormatRepositories(nil) = %q", got)
	}
}

func TestFormatRepositories_nonEmpty(t *testing.T) {
	repos := []Repository{
		{Name: "user/repo1", URL: "https://github.com/user/repo1", Branch: "main", Lists: []string{"list1"}},
		{Name: "user/repo2", URL: "https://github.com/user/repo2"},
	}
	got := FormatRepositories(repos)
	if !strings.Contains(got, "user/repo1") {
		t.Error("FormatRepositories should contain first repo")
	}
	if !strings.Contains(got, "user/repo2") {
		t.Error("FormatRepositories should contain second repo")
	}
	if !strings.Contains(got, "branch: main") {
		t.Error("FormatRepositories should contain branch for first repo")
	}
	if !strings.Contains(got, "lists: list1") {
		t.Error("FormatRepositories should contain lists for first repo")
	}
}

func TestFormatRelatedRepository(t *testing.T) {
	repo := RelatedRepository{
		Repository: Repository{
			Name: "related/repo",
			URL:  "https://github.com/related/repo",
		},
		Relationships: RelationshipInfo{
			SharedLists:   []string{"list1"},
			SharedAuthors: []string{"alice@example.com"},
		},
	}

	got := FormatRelatedRepository(repo)
	if !strings.Contains(got, "shared lists: list1") {
		t.Error("FormatRelatedRepository should contain shared lists")
	}
	if !strings.Contains(got, "shared authors: alice@example.com") {
		t.Error("FormatRelatedRepository should contain shared authors")
	}
}

func TestFormatRelatedRepositories_empty(t *testing.T) {
	got := FormatRelatedRepositories(nil)
	if got != "No related repositories found." {
		t.Errorf("FormatRelatedRepositories(nil) = %q", got)
	}
}

func TestFormatRelatedRepositories_nonEmpty(t *testing.T) {
	repos := []RelatedRepository{
		{
			Repository:    Repository{Name: "related/one", URL: "https://github.com/related/one"},
			Relationships: RelationshipInfo{SharedLists: []string{"list1"}, SharedAuthors: []string{"alice@test.com"}},
		},
		{
			Repository:    Repository{Name: "related/two", URL: "https://github.com/related/two"},
			Relationships: RelationshipInfo{SharedLists: []string{"list2"}},
		},
	}
	got := FormatRelatedRepositories(repos)
	if !strings.Contains(got, "related/one") {
		t.Error("FormatRelatedRepositories should contain first repo")
	}
	if !strings.Contains(got, "related/two") {
		t.Error("FormatRelatedRepositories should contain second repo")
	}
	if !strings.Contains(got, "shared lists: list1") {
		t.Error("FormatRelatedRepositories should contain shared lists for first repo")
	}
	if !strings.Contains(got, "shared authors: alice@test.com") {
		t.Error("FormatRelatedRepositories should contain shared authors for first repo")
	}
}

func TestFormatLogEntry(t *testing.T) {
	entry := LogEntry{
		Hash:      "abc123def456",
		Timestamp: time.Now().Add(-5 * time.Minute),
		Author:    Author{Name: "Alice"},
		Type:      LogTypePost,
		Details:   "Created a post",
	}

	got := FormatLogEntry(entry)
	if !strings.Contains(got, "abc123d") {
		t.Error("FormatLogEntry should truncate hash to 7 chars")
	}
	if !strings.Contains(got, "post") {
		t.Error("FormatLogEntry should contain entry type")
	}
	if !strings.Contains(got, "Alice") {
		t.Error("FormatLogEntry should contain author")
	}
}

func TestFormatLogs_empty(t *testing.T) {
	got := FormatLogs(nil)
	if got != "No activity found." {
		t.Errorf("FormatLogs(nil) = %q", got)
	}
}

func TestFormatLogs(t *testing.T) {
	entries := []LogEntry{
		{Hash: "abc123def456", Timestamp: time.Now(), Author: Author{Name: "Alice"}, Type: LogTypePost, Details: "Post 1"},
		{Hash: "def456abc123", Timestamp: time.Now(), Author: Author{Name: "Bob"}, Type: LogTypeComment, Details: "Comment 1"},
	}

	got := FormatLogs(entries)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Errorf("FormatLogs should produce 2 lines, got %d", len(lines))
	}
}
