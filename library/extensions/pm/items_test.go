// items_test.go - Tests for PM item conversion functions
package pm

import (
	"database/sql"
	"testing"
	"time"
)

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"single scoped", "priority/high", 1},
		{"multiple scoped", "priority/high,kind/bug", 2},
		{"unscoped", "urgent", 1},
		{"mixed", "priority/high,urgent,kind/bug", 3},
		{"whitespace", " priority/high , kind/bug ", 2},
		{"trailing comma", "priority/high,", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLabels(tt.input)
			if len(got) != tt.want {
				t.Errorf("parseLabels(%q) = %d labels, want %d", tt.input, len(got), tt.want)
			}
		})
	}
}

func TestParseLabels_scopeAndValue(t *testing.T) {
	got := parseLabels("priority/high")
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Scope != "priority" {
		t.Errorf("Scope = %q, want %q", got[0].Scope, "priority")
	}
	if got[0].Value != "high" {
		t.Errorf("Value = %q, want %q", got[0].Value, "high")
	}
}

func TestParseLabels_unscoped(t *testing.T) {
	got := parseLabels("urgent")
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Scope != "" {
		t.Errorf("Scope = %q, want empty", got[0].Scope)
	}
	if got[0].Value != "urgent" {
		t.Errorf("Value = %q, want %q", got[0].Value, "urgent")
	}
}

func TestParseAssignees(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"single", "alice@example.com", 1},
		{"multiple", "alice@example.com,bob@example.com", 2},
		{"whitespace", " alice@example.com , bob@example.com ", 2},
		{"trailing comma", "alice@example.com,", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAssignees(tt.input)
			if len(got) != tt.want {
				t.Errorf("parseAssignees(%q) = %d, want %d", tt.input, len(got), tt.want)
			}
		})
	}
}

func TestPMItemToIssue(t *testing.T) {
	item := PMItem{
		RepoURL:     "https://github.com/user/repo",
		Hash:        "abc123def456",
		Branch:      "gitmsg/pm",
		Type:        string(ItemTypeIssue),
		State:       "open",
		Assignees:   sql.NullString{String: "alice@example.com,bob@example.com", Valid: true},
		Due:         sql.NullString{String: "2025-12-31", Valid: true},
		Labels:      sql.NullString{String: "priority/high,kind/bug", Valid: true},
		Content:     "Fix login bug\n\nThe login page crashes",
		AuthorName:  "Alice",
		AuthorEmail: "alice@example.com",
		Timestamp:   time.Date(2025, 10, 15, 12, 0, 0, 0, time.UTC),
		IsEdited:    true,
		Comments:    3,
	}

	issue := PMItemToIssue(item)
	if issue.Subject != "Fix login bug" {
		t.Errorf("Subject = %q", issue.Subject)
	}
	if issue.Body != "The login page crashes" {
		t.Errorf("Body = %q", issue.Body)
	}
	if issue.State != StateOpen {
		t.Errorf("State = %q", issue.State)
	}
	if len(issue.Assignees) != 2 {
		t.Errorf("len(Assignees) = %d, want 2", len(issue.Assignees))
	}
	if issue.Due == nil {
		t.Fatal("Due should not be nil")
	}
	if issue.Due.Year() != 2025 || issue.Due.Month() != time.December || issue.Due.Day() != 31 {
		t.Errorf("Due = %v, want 2025-12-31", issue.Due)
	}
	if len(issue.Labels) != 2 {
		t.Errorf("len(Labels) = %d, want 2", len(issue.Labels))
	}
	if !issue.IsEdited {
		t.Error("IsEdited should be true")
	}
	if issue.Comments != 3 {
		t.Errorf("Comments = %d, want 3", issue.Comments)
	}
	if issue.Author.Name != "Alice" {
		t.Errorf("Author.Name = %q", issue.Author.Name)
	}
}

func TestPMItemToIssue_withRefs(t *testing.T) {
	item := PMItem{
		RepoURL:          "https://github.com/user/repo",
		Hash:             "abc123def456",
		Branch:           "gitmsg/pm",
		Type:             string(ItemTypeIssue),
		State:            "open",
		MilestoneRepoURL: sql.NullString{String: "https://github.com/user/repo", Valid: true},
		MilestoneHash:    sql.NullString{String: "ms123", Valid: true},
		MilestoneBranch:  sql.NullString{String: "gitmsg/pm", Valid: true},
		ParentRepoURL:    sql.NullString{String: "https://github.com/user/repo", Valid: true},
		ParentHash:       sql.NullString{String: "parent123", Valid: true},
		ParentBranch:     sql.NullString{String: "gitmsg/pm", Valid: true},
		Content:          "Sub-issue",
		AuthorName:       "Alice",
		AuthorEmail:      "alice@example.com",
	}

	issue := PMItemToIssue(item)
	if issue.Milestone == nil {
		t.Fatal("Milestone should not be nil")
	}
	if issue.Milestone.Hash != "ms123" {
		t.Errorf("Milestone.Hash = %q", issue.Milestone.Hash)
	}
	if issue.Parent == nil {
		t.Fatal("Parent should not be nil")
	}
	if issue.Parent.Hash != "parent123" {
		t.Errorf("Parent.Hash = %q", issue.Parent.Hash)
	}
}

func TestPMItemToIssue_nullRefs(t *testing.T) {
	item := PMItem{
		RepoURL:     "https://github.com/user/repo",
		Hash:        "abc123",
		Branch:      "gitmsg/pm",
		State:       "open",
		Content:     "Simple issue",
		AuthorName:  "Alice",
		AuthorEmail: "alice@example.com",
	}

	issue := PMItemToIssue(item)
	if issue.Milestone != nil {
		t.Error("Milestone should be nil")
	}
	if issue.Sprint != nil {
		t.Error("Sprint should be nil")
	}
	if issue.Parent != nil {
		t.Error("Parent should be nil")
	}
	if issue.Root != nil {
		t.Error("Root should be nil")
	}
}

func TestPMItemToMilestone(t *testing.T) {
	item := PMItem{
		RepoURL:     "https://github.com/user/repo",
		Hash:        "ms123def456",
		Branch:      "gitmsg/pm",
		Type:        string(ItemTypeMilestone),
		State:       "open",
		Due:         sql.NullString{String: "2025-12-31", Valid: true},
		Content:     "v1.0 Release\n\nFirst major release",
		AuthorName:  "Alice",
		AuthorEmail: "alice@example.com",
		Timestamp:   time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC),
	}

	ms := PMItemToMilestone(item)
	if ms.Title != "v1.0 Release" {
		t.Errorf("Title = %q", ms.Title)
	}
	if ms.Body != "First major release" {
		t.Errorf("Body = %q", ms.Body)
	}
	if ms.State != StateOpen {
		t.Errorf("State = %q", ms.State)
	}
	if ms.Due == nil {
		t.Fatal("Due should not be nil")
	}
}

func TestPMItemToSprint(t *testing.T) {
	item := PMItem{
		RepoURL:     "https://github.com/user/repo",
		Hash:        "sp123def456",
		Branch:      "gitmsg/pm",
		Type:        string(ItemTypeSprint),
		State:       string(SprintStateActive),
		StartDate:   sql.NullString{String: "2025-10-01", Valid: true},
		EndDate:     sql.NullString{String: "2025-10-14", Valid: true},
		Content:     "Sprint 1\n\nFirst sprint",
		AuthorName:  "Alice",
		AuthorEmail: "alice@example.com",
	}

	sprint := PMItemToSprint(item)
	if sprint.Title != "Sprint 1" {
		t.Errorf("Title = %q", sprint.Title)
	}
	if sprint.State != SprintStateActive {
		t.Errorf("State = %q", sprint.State)
	}
	if sprint.Start.IsZero() {
		t.Error("Start should not be zero")
	}
	if sprint.End.IsZero() {
		t.Error("End should not be zero")
	}
	if sprint.Start.Day() != 1 || sprint.End.Day() != 14 {
		t.Errorf("Start=%v, End=%v", sprint.Start, sprint.End)
	}
}

func TestPMItemToIssue_withSprintAndRootRefs(t *testing.T) {
	item := PMItem{
		RepoURL:       "https://github.com/user/repo",
		Hash:          "abc123def456",
		Branch:        "gitmsg/pm",
		Type:          string(ItemTypeIssue),
		State:         "open",
		SprintRepoURL: sql.NullString{String: "https://github.com/user/repo", Valid: true},
		SprintHash:    sql.NullString{String: "sp123", Valid: true},
		SprintBranch:  sql.NullString{String: "gitmsg/pm", Valid: true},
		RootRepoURL:   sql.NullString{String: "https://github.com/user/repo", Valid: true},
		RootHash:      sql.NullString{String: "root456", Valid: true},
		RootBranch:    sql.NullString{String: "gitmsg/pm", Valid: true},
		Content:       "Issue with sprint and root",
		AuthorName:    "Alice",
		AuthorEmail:   "alice@example.com",
	}
	issue := PMItemToIssue(item)
	if issue.Sprint == nil {
		t.Fatal("Sprint should not be nil")
	}
	if issue.Sprint.Hash != "sp123" {
		t.Errorf("Sprint.Hash = %q, want sp123", issue.Sprint.Hash)
	}
	if issue.Root == nil {
		t.Fatal("Root should not be nil")
	}
	if issue.Root.Hash != "root456" {
		t.Errorf("Root.Hash = %q, want root456", issue.Root.Hash)
	}
}

func TestBuildOrderClause_items_updated(t *testing.T) {
	got := buildOrderClause("updated", "asc")
	if got != "v.timestamp ASC" {
		t.Errorf("buildOrderClause(updated, asc) = %q, want %q", got, "v.timestamp ASC")
	}
}

func TestBuildOrderClause(t *testing.T) {
	tests := []struct {
		name      string
		sortField string
		sortOrder string
		contains  string
	}{
		{"default", "", "", "v.timestamp DESC"},
		{"created asc", "created", "asc", "v.timestamp ASC"},
		{"due desc", "due", "desc", "v.due DESC"},
		{"start asc", "start", "asc", "v.start_date ASC"},
		{"priority", "priority", "asc", "CASE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildOrderClause(tt.sortField, tt.sortOrder)
			if !contains(got, tt.contains) {
				t.Errorf("buildOrderClause(%q, %q) = %q, should contain %q", tt.sortField, tt.sortOrder, got, tt.contains)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || indexOfStr(s, substr) >= 0)
}

func indexOfStr(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
