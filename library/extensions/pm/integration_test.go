// integration_test.go - Git workspace integration tests for PM extension
package pm

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"

	_ "github.com/gitsocial-org/gitsocial/extensions/social"
)

var baseRepoDir string
var testCacheDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pm-test-base-*")
	if err != nil {
		panic(err)
	}
	git.Init(dir, "main")
	git.ExecGit(dir, []string{"config", "user.email", "test@test.com"})
	git.ExecGit(dir, []string{"config", "user.name", "Test User"})
	git.CreateCommit(dir, git.CommitOptions{Message: "Initial commit", AllowEmpty: true})
	baseRepoDir = dir

	cacheDir, err := os.MkdirTemp("", "pm-test-cache-*")
	if err != nil {
		panic(err)
	}
	cache.Open(cacheDir)
	testCacheDir = cacheDir

	code := m.Run()
	cache.Reset()
	os.RemoveAll(cacheDir)
	os.RemoveAll(dir)
	os.Exit(code)
}

func cloneFixture(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	cmd := exec.Command("cp", "-a", baseRepoDir+"/.", dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cloneFixture: %v: %s", err, out)
	}
	return dst
}

func initWorkspace(t *testing.T) string {
	t.Helper()
	workdir := cloneFixture(t)
	setupTestDB(t)
	return workdir
}

// --- Issue CRUD ---

func TestIssueCRUD(t *testing.T) {
	t.Parallel()

	t.Run("CreateIssue", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := CreateIssue(workdir, "Test issue", "Description here", CreateIssueOptions{})
		if !result.Success {
			t.Fatalf("CreateIssue() failed: %s", result.Error.Message)
		}
		issue := result.Data
		if issue.Subject != "Test issue" {
			t.Errorf("Subject = %q, want %q", issue.Subject, "Test issue")
		}
		if issue.Body != "Description here" {
			t.Errorf("Body = %q, want %q", issue.Body, "Description here")
		}
		if issue.State != StateOpen {
			t.Errorf("State = %q, want open", issue.State)
		}
	})

	t.Run("CreateIssue_withOptions", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		due := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
		result := CreateIssue(workdir, "Full issue", "", CreateIssueOptions{
			Assignees: []string{"alice@test.com"},
			Due:       &due,
			Labels:    []Label{{Scope: "priority", Value: "high"}},
		})
		if !result.Success {
			t.Fatalf("CreateIssue() failed: %s", result.Error.Message)
		}
		issue := result.Data
		if len(issue.Assignees) != 1 || issue.Assignees[0] != "alice@test.com" {
			t.Errorf("Assignees = %v", issue.Assignees)
		}
		if issue.Due == nil || issue.Due.Year() != 2025 {
			t.Errorf("Due = %v", issue.Due)
		}
		if len(issue.Labels) != 1 || issue.Labels[0].Value != "high" {
			t.Errorf("Labels = %v", issue.Labels)
		}
	})

	t.Run("GetIssue", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateIssue(workdir, "Find me", "", CreateIssueOptions{})
		if !created.Success {
			t.Fatalf("CreateIssue() failed: %s", created.Error.Message)
		}
		found := GetIssue(created.Data.ID)
		if !found.Success {
			t.Fatalf("GetIssue() failed: %s", found.Error.Message)
		}
		if found.Data.Subject != "Find me" {
			t.Errorf("Subject = %q, want %q", found.Data.Subject, "Find me")
		}
	})

	t.Run("GetIssue_notFound", func(t *testing.T) {
		t.Parallel()
		_ = cloneFixture(t)
		result := GetIssue("nonexistent123456")
		if result.Success {
			t.Error("GetIssue() should fail for non-existent issue")
		}
	})

	t.Run("UpdateIssue", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateIssue(workdir, "Original subject", "Original body", CreateIssueOptions{})
		if !created.Success {
			t.Fatalf("CreateIssue() failed: %s", created.Error.Message)
		}
		newSubject := "Updated subject"
		newBody := "Updated body"
		closed := StateClosed
		updated := UpdateIssue(workdir, created.Data.ID, UpdateIssueOptions{
			Subject: &newSubject,
			Body:    &newBody,
			State:   &closed,
		})
		if !updated.Success {
			t.Fatalf("UpdateIssue() failed: %s", updated.Error.Message)
		}
		if updated.Data.Subject != "Updated subject" {
			t.Errorf("Subject = %q, want %q", updated.Data.Subject, "Updated subject")
		}
		if updated.Data.State != StateClosed {
			t.Errorf("State = %q, want closed", updated.Data.State)
		}
		if !updated.Data.IsEdited {
			t.Error("IsEdited should be true after update")
		}
	})

	t.Run("UpdateIssue_notFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := UpdateIssue(workdir, "nonexistent123456", UpdateIssueOptions{})
		if result.Success {
			t.Error("UpdateIssue() should fail for non-existent issue")
		}
	})

	t.Run("UpdateIssue_withLabelsAndAssignees", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateIssue(workdir, "Label test", "", CreateIssueOptions{})
		if !created.Success {
			t.Fatalf("CreateIssue() failed: %s", created.Error.Message)
		}
		labels := []Label{{Scope: "priority", Value: "critical"}}
		assignees := []string{"bob@test.com"}
		updated := UpdateIssue(workdir, created.Data.ID, UpdateIssueOptions{
			Labels:    &labels,
			Assignees: &assignees,
		})
		if !updated.Success {
			t.Fatalf("UpdateIssue() failed: %s", updated.Error.Message)
		}
		if len(updated.Data.Labels) != 1 || updated.Data.Labels[0].Value != "critical" {
			t.Errorf("Labels = %v", updated.Data.Labels)
		}
		if len(updated.Data.Assignees) != 1 || updated.Data.Assignees[0] != "bob@test.com" {
			t.Errorf("Assignees = %v", updated.Data.Assignees)
		}
	})

	t.Run("CloseIssue", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateIssue(workdir, "Close me", "", CreateIssueOptions{})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		result := CloseIssue(workdir, created.Data.ID)
		if !result.Success {
			t.Fatalf("CloseIssue() failed: %s", result.Error.Message)
		}
		if result.Data.State != StateClosed {
			t.Errorf("State = %q, want closed", result.Data.State)
		}
	})

	t.Run("ReopenIssue", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateIssue(workdir, "Reopen me", "", CreateIssueOptions{State: StateClosed})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		result := ReopenIssue(workdir, created.Data.ID)
		if !result.Success {
			t.Fatalf("ReopenIssue() failed: %s", result.Error.Message)
		}
		if result.Data.State != StateOpen {
			t.Errorf("State = %q, want open", result.Data.State)
		}
	})

	t.Run("RetractIssue", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateIssue(workdir, "Retract me", "", CreateIssueOptions{})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		result := RetractIssue(workdir, created.Data.ID)
		if !result.Success {
			t.Fatalf("RetractIssue() failed: %s", result.Error.Message)
		}
		if !result.Data {
			t.Error("RetractIssue should return true")
		}
	})

	t.Run("RetractIssue_notFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := RetractIssue(workdir, "nonexistent123456")
		if result.Success {
			t.Error("RetractIssue() should fail for non-existent issue")
		}
	})
}

// --- Issue with refs ---

func TestIssueRefs(t *testing.T) {
	t.Parallel()

	t.Run("CreateIssue_withMilestoneAndSprint", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		ms := CreateMilestone(workdir, "v1.0", "", CreateMilestoneOptions{})
		if !ms.Success {
			t.Fatal(ms.Error.Message)
		}
		sp := CreateSprint(workdir, "Sprint 1", "", CreateSprintOptions{
			Start: time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 10, 14, 0, 0, 0, 0, time.UTC),
		})
		if !sp.Success {
			t.Fatal(sp.Error.Message)
		}
		result := CreateIssue(workdir, "Linked issue", "", CreateIssueOptions{
			Milestone: ms.Data.ID,
			Sprint:    sp.Data.ID,
		})
		if !result.Success {
			t.Fatalf("CreateIssue() failed: %s", result.Error.Message)
		}
		if result.Data.Milestone == nil {
			t.Error("Milestone should not be nil")
		}
		if result.Data.Sprint == nil {
			t.Error("Sprint should not be nil")
		}
	})

	t.Run("UpdateIssue_withMilestoneAndSprint", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateIssue(workdir, "Plain issue", "", CreateIssueOptions{})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		ms := CreateMilestone(workdir, "v2.0", "", CreateMilestoneOptions{})
		if !ms.Success {
			t.Fatal(ms.Error.Message)
		}
		msRef := ms.Data.ID
		updated := UpdateIssue(workdir, created.Data.ID, UpdateIssueOptions{Milestone: &msRef})
		if !updated.Success {
			t.Fatalf("UpdateIssue() failed: %s", updated.Error.Message)
		}
		if updated.Data.Milestone == nil {
			t.Error("Milestone should be set after update")
		}
	})

	t.Run("UpdateIssue_withParentAndRoot", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		parent := CreateIssue(workdir, "Parent issue", "", CreateIssueOptions{})
		if !parent.Success {
			t.Fatal(parent.Error.Message)
		}
		child := CreateIssue(workdir, "Child issue", "", CreateIssueOptions{})
		if !child.Success {
			t.Fatal(child.Error.Message)
		}
		parentRef := parent.Data.ID
		rootRef := parent.Data.ID
		updated := UpdateIssue(workdir, child.Data.ID, UpdateIssueOptions{
			Parent: &parentRef,
			Root:   &rootRef,
		})
		if !updated.Success {
			t.Fatalf("UpdateIssue() failed: %s", updated.Error.Message)
		}
		if updated.Data.Parent == nil {
			t.Error("Parent should be set after update")
		}
		if updated.Data.Root == nil {
			t.Error("Root should be set after update")
		}
	})

	t.Run("UpdateIssue_withDue", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateIssue(workdir, "Due issue", "", CreateIssueOptions{})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		due := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
		updated := UpdateIssue(workdir, created.Data.ID, UpdateIssueOptions{Due: &due})
		if !updated.Success {
			t.Fatalf("UpdateIssue() failed: %s", updated.Error.Message)
		}
		if updated.Data.Due == nil {
			t.Error("Due should be set after update")
		}
	})

	t.Run("UpdateIssue_preserveExistingRefs", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		ms := CreateMilestone(workdir, "Preserve MS", "", CreateMilestoneOptions{})
		if !ms.Success {
			t.Fatal(ms.Error.Message)
		}
		sp := CreateSprint(workdir, "Preserve Sprint", "", CreateSprintOptions{
			Start: time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 10, 14, 0, 0, 0, 0, time.UTC),
		})
		if !sp.Success {
			t.Fatal(sp.Error.Message)
		}
		parent := CreateIssue(workdir, "Parent for preserve", "", CreateIssueOptions{})
		if !parent.Success {
			t.Fatal(parent.Error.Message)
		}
		created := CreateIssue(workdir, "Issue with all refs", "", CreateIssueOptions{
			Milestone: ms.Data.ID,
			Sprint:    sp.Data.ID,
			Parent:    parent.Data.ID,
			Root:      parent.Data.ID,
		})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		newSubject := "Updated subject preserving refs"
		result := UpdateIssue(workdir, created.Data.ID, UpdateIssueOptions{Subject: &newSubject})
		if !result.Success {
			t.Fatalf("UpdateIssue() failed: %s", result.Error.Message)
		}
		if result.Data.Milestone == nil {
			t.Error("Milestone should be preserved")
		}
		if result.Data.Sprint == nil {
			t.Error("Sprint should be preserved")
		}
		if result.Data.Parent == nil {
			t.Error("Parent should be preserved")
		}
		if result.Data.Root == nil {
			t.Error("Root should be preserved")
		}
	})

	t.Run("UpdateIssue_updateSprintRef", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateIssue(workdir, "Sprint update test", "", CreateIssueOptions{})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		sp := CreateSprint(workdir, "New Sprint", "", CreateSprintOptions{
			Start: time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 11, 14, 0, 0, 0, 0, time.UTC),
		})
		if !sp.Success {
			t.Fatal(sp.Error.Message)
		}
		spRef := sp.Data.ID
		result := UpdateIssue(workdir, created.Data.ID, UpdateIssueOptions{Sprint: &spRef})
		if !result.Success {
			t.Fatalf("UpdateIssue() failed: %s", result.Error.Message)
		}
	})
}

// --- Milestone CRUD ---

func TestMilestoneCRUD(t *testing.T) {
	t.Parallel()

	t.Run("CreateMilestone", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := CreateMilestone(workdir, "v1.0 Release", "First major release", CreateMilestoneOptions{})
		if !result.Success {
			t.Fatalf("CreateMilestone() failed: %s", result.Error.Message)
		}
		ms := result.Data
		if ms.Title != "v1.0 Release" {
			t.Errorf("Title = %q", ms.Title)
		}
		if ms.Body != "First major release" {
			t.Errorf("Body = %q", ms.Body)
		}
		if ms.State != StateOpen {
			t.Errorf("State = %q, want open", ms.State)
		}
	})

	t.Run("CreateMilestone_withDue", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		due := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
		result := CreateMilestone(workdir, "Q4 Milestone", "", CreateMilestoneOptions{Due: &due})
		if !result.Success {
			t.Fatalf("CreateMilestone() failed: %s", result.Error.Message)
		}
		if result.Data.Due == nil {
			t.Fatal("Due should not be nil")
		}
		if result.Data.Due.Year() != 2025 || result.Data.Due.Month() != time.December {
			t.Errorf("Due = %v", result.Data.Due)
		}
	})

	t.Run("GetMilestone", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateMilestone(workdir, "Find this MS", "", CreateMilestoneOptions{})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		found := GetMilestone(created.Data.ID)
		if !found.Success {
			t.Fatalf("GetMilestone() failed: %s", found.Error.Message)
		}
		if found.Data.Title != "Find this MS" {
			t.Errorf("Title = %q", found.Data.Title)
		}
	})

	t.Run("GetMilestone_notFound", func(t *testing.T) {
		t.Parallel()
		_ = cloneFixture(t)
		result := GetMilestone("nonexistent123456")
		if result.Success {
			t.Error("GetMilestone() should fail for non-existent milestone")
		}
	})

	t.Run("UpdateMilestone", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateMilestone(workdir, "Original MS", "", CreateMilestoneOptions{})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		newTitle := "Updated MS"
		newBody := "New description"
		updated := UpdateMilestone(workdir, created.Data.ID, UpdateMilestoneOptions{
			Title: &newTitle,
			Body:  &newBody,
		})
		if !updated.Success {
			t.Fatalf("UpdateMilestone() failed: %s", updated.Error.Message)
		}
		if updated.Data.Title != "Updated MS" {
			t.Errorf("Title = %q", updated.Data.Title)
		}
	})

	t.Run("UpdateMilestone_notFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := UpdateMilestone(workdir, "nonexistent123456", UpdateMilestoneOptions{})
		if result.Success {
			t.Error("UpdateMilestone() should fail for non-existent milestone")
		}
	})

	t.Run("UpdateMilestone_withDue", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateMilestone(workdir, "MS with due", "", CreateMilestoneOptions{})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		due := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
		updated := UpdateMilestone(workdir, created.Data.ID, UpdateMilestoneOptions{Due: &due})
		if !updated.Success {
			t.Fatalf("UpdateMilestone() failed: %s", updated.Error.Message)
		}
		if updated.Data.Due == nil {
			t.Error("Due should be set after update")
		}
	})

	t.Run("CloseMilestone", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateMilestone(workdir, "Close MS", "", CreateMilestoneOptions{})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		result := CloseMilestone(workdir, created.Data.ID)
		if !result.Success {
			t.Fatalf("CloseMilestone() failed: %s", result.Error.Message)
		}
		if result.Data.State != StateClosed {
			t.Errorf("State = %q, want closed", result.Data.State)
		}
	})

	t.Run("ReopenMilestone", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateMilestone(workdir, "Reopen MS", "", CreateMilestoneOptions{State: StateClosed})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		result := ReopenMilestone(workdir, created.Data.ID)
		if !result.Success {
			t.Fatalf("ReopenMilestone() failed: %s", result.Error.Message)
		}
		if result.Data.State != StateOpen {
			t.Errorf("State = %q, want open", result.Data.State)
		}
	})

	t.Run("CancelMilestone", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateMilestone(workdir, "Cancel MS", "", CreateMilestoneOptions{})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		result := CancelMilestone(workdir, created.Data.ID)
		if !result.Success {
			t.Fatalf("CancelMilestone() failed: %s", result.Error.Message)
		}
		if result.Data.State != StateCancelled {
			t.Errorf("State = %q, want canceled", result.Data.State)
		}
	})

	t.Run("RetractMilestone", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateMilestone(workdir, "Retract MS", "", CreateMilestoneOptions{})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		result := RetractMilestone(workdir, created.Data.ID)
		if !result.Success {
			t.Fatalf("RetractMilestone() failed: %s", result.Error.Message)
		}
	})

	t.Run("RetractMilestone_notFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := RetractMilestone(workdir, "nonexistent123456")
		if result.Success {
			t.Error("RetractMilestone() should fail for non-existent milestone")
		}
	})
}

// --- Milestone issues ---

func TestMilestoneIssues(t *testing.T) {
	t.Parallel()

	t.Run("GetMilestoneIssues", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		ms := CreateMilestone(workdir, "MS with issues", "", CreateMilestoneOptions{})
		if !ms.Success {
			t.Fatal(ms.Error.Message)
		}
		CreateIssue(workdir, "Issue 1", "", CreateIssueOptions{Milestone: ms.Data.ID})
		CreateIssue(workdir, "Issue 2", "", CreateIssueOptions{Milestone: ms.Data.ID})
		CreateIssue(workdir, "Unlinked", "", CreateIssueOptions{})
		result := GetMilestoneIssues(ms.Data.ID, []string{"open"})
		if !result.Success {
			t.Fatalf("GetMilestoneIssues() failed: %s", result.Error.Message)
		}
		if len(result.Data) != 2 {
			t.Errorf("expected 2 milestone issues, got %d", len(result.Data))
		}
	})

	t.Run("GetMilestoneIssues_invalidRef", func(t *testing.T) {
		t.Parallel()
		_ = cloneFixture(t)
		result := GetMilestoneIssues("", []string{"open"})
		if result.Success {
			t.Error("should fail with empty ref")
		}
		if result.Error.Code != "INVALID_REF" {
			t.Errorf("error code = %q, want INVALID_REF", result.Error.Code)
		}
	})
}

// --- Sprint CRUD ---

func TestSprintCRUD(t *testing.T) {
	t.Parallel()

	t.Run("CreateSprint", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		start := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2025, 10, 14, 0, 0, 0, 0, time.UTC)
		result := CreateSprint(workdir, "Sprint 1", "First sprint", CreateSprintOptions{Start: start, End: end})
		if !result.Success {
			t.Fatalf("CreateSprint() failed: %s", result.Error.Message)
		}
		sprint := result.Data
		if sprint.Title != "Sprint 1" {
			t.Errorf("Title = %q", sprint.Title)
		}
		if sprint.Body != "First sprint" {
			t.Errorf("Body = %q", sprint.Body)
		}
		if sprint.State != SprintStatePlanned {
			t.Errorf("State = %q, want planned", sprint.State)
		}
		if sprint.Start.Day() != 1 {
			t.Errorf("Start = %v", sprint.Start)
		}
		if sprint.End.Day() != 14 {
			t.Errorf("End = %v", sprint.End)
		}
	})

	t.Run("GetSprint", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateSprint(workdir, "Find Sprint", "", CreateSprintOptions{
			Start: time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 11, 14, 0, 0, 0, 0, time.UTC),
		})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		found := GetSprint(created.Data.ID)
		if !found.Success {
			t.Fatalf("GetSprint() failed: %s", found.Error.Message)
		}
		if found.Data.Title != "Find Sprint" {
			t.Errorf("Title = %q", found.Data.Title)
		}
	})

	t.Run("GetSprint_notFound", func(t *testing.T) {
		t.Parallel()
		_ = cloneFixture(t)
		result := GetSprint("nonexistent123456")
		if result.Success {
			t.Error("GetSprint() should fail for non-existent sprint")
		}
	})

	t.Run("UpdateSprint", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateSprint(workdir, "Original Sprint", "", CreateSprintOptions{
			Start: time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 10, 14, 0, 0, 0, 0, time.UTC),
		})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		newTitle := "Updated Sprint"
		newBody := "Updated description"
		newStart := time.Date(2025, 10, 7, 0, 0, 0, 0, time.UTC)
		newEnd := time.Date(2025, 10, 21, 0, 0, 0, 0, time.UTC)
		updated := UpdateSprint(workdir, created.Data.ID, UpdateSprintOptions{
			Title: &newTitle,
			Body:  &newBody,
			Start: &newStart,
			End:   &newEnd,
		})
		if !updated.Success {
			t.Fatalf("UpdateSprint() failed: %s", updated.Error.Message)
		}
		if updated.Data.Title != "Updated Sprint" {
			t.Errorf("Title = %q", updated.Data.Title)
		}
	})

	t.Run("UpdateSprint_notFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := UpdateSprint(workdir, "nonexistent123456", UpdateSprintOptions{})
		if result.Success {
			t.Error("UpdateSprint() should fail for non-existent sprint")
		}
	})

	t.Run("ActivateSprint", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateSprint(workdir, "Activate Sprint", "", CreateSprintOptions{
			Start: time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 10, 14, 0, 0, 0, 0, time.UTC),
		})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		result := ActivateSprint(workdir, created.Data.ID)
		if !result.Success {
			t.Fatalf("ActivateSprint() failed: %s", result.Error.Message)
		}
		if result.Data.State != SprintStateActive {
			t.Errorf("State = %q, want active", result.Data.State)
		}
	})

	t.Run("CompleteSprint", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateSprint(workdir, "Complete Sprint", "", CreateSprintOptions{
			Start: time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 10, 14, 0, 0, 0, 0, time.UTC),
		})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		result := CompleteSprint(workdir, created.Data.ID)
		if !result.Success {
			t.Fatalf("CompleteSprint() failed: %s", result.Error.Message)
		}
		if result.Data.State != SprintStateCompleted {
			t.Errorf("State = %q, want completed", result.Data.State)
		}
	})

	t.Run("CancelSprint", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateSprint(workdir, "Cancel Sprint", "", CreateSprintOptions{
			Start: time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 10, 14, 0, 0, 0, 0, time.UTC),
		})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		result := CancelSprint(workdir, created.Data.ID)
		if !result.Success {
			t.Fatalf("CancelSprint() failed: %s", result.Error.Message)
		}
		if result.Data.State != SprintStateCancelled {
			t.Errorf("State = %q, want canceled", result.Data.State)
		}
	})

	t.Run("RetractSprint", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreateSprint(workdir, "Retract Sprint", "", CreateSprintOptions{
			Start: time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 10, 14, 0, 0, 0, 0, time.UTC),
		})
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		result := RetractSprint(workdir, created.Data.ID)
		if !result.Success {
			t.Fatalf("RetractSprint() failed: %s", result.Error.Message)
		}
	})

	t.Run("RetractSprint_notFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := RetractSprint(workdir, "nonexistent123456")
		if result.Success {
			t.Error("RetractSprint() should fail for non-existent sprint")
		}
	})

	t.Run("GetSprintIssues", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		sp := CreateSprint(workdir, "Sprint with issues", "", CreateSprintOptions{
			Start: time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 10, 14, 0, 0, 0, 0, time.UTC),
		})
		if !sp.Success {
			t.Fatal(sp.Error.Message)
		}
		CreateIssue(workdir, "Sprint Issue 1", "", CreateIssueOptions{Sprint: sp.Data.ID})
		CreateIssue(workdir, "Sprint Issue 2", "", CreateIssueOptions{Sprint: sp.Data.ID})
		CreateIssue(workdir, "No sprint", "", CreateIssueOptions{})
		result := GetSprintIssues(sp.Data.ID, []string{"open"})
		if !result.Success {
			t.Fatalf("GetSprintIssues() failed: %s", result.Error.Message)
		}
		if len(result.Data) != 2 {
			t.Errorf("expected 2 sprint issues, got %d", len(result.Data))
		}
	})

	t.Run("GetSprintIssues_invalidRef", func(t *testing.T) {
		t.Parallel()
		_ = cloneFixture(t)
		result := GetSprintIssues("", []string{"open"})
		if result.Success {
			t.Error("should fail with empty ref")
		}
		if result.Error.Code != "INVALID_REF" {
			t.Errorf("error code = %q, want INVALID_REF", result.Error.Code)
		}
	})
}

// --- Board and config ---

func TestBoardAndConfig(t *testing.T) {
	t.Parallel()

	t.Run("GetPMConfig_default", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		config := GetPMConfig(workdir)
		if config.Version != "0.1.0" {
			t.Errorf("Version = %q", config.Version)
		}
		if config.Branch != "gitmsg/pm" {
			t.Errorf("Branch = %q", config.Branch)
		}
		if config.Framework != "kanban" {
			t.Errorf("Framework = %q", config.Framework)
		}
	})

	t.Run("SavePMConfig_andReload", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		config := PMConfig{
			Version:   "0.2.0",
			Branch:    "gitmsg/pm",
			Framework: "scrum",
		}
		if err := SavePMConfig(workdir, config); err != nil {
			t.Fatalf("SavePMConfig() error = %v", err)
		}
		loaded := GetPMConfig(workdir)
		if loaded.Framework != "scrum" {
			t.Errorf("Framework = %q, want scrum", loaded.Framework)
		}
	})

	t.Run("SavePMConfig_emptyVersion", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		config := PMConfig{Framework: "minimal"}
		if err := SavePMConfig(workdir, config); err != nil {
			t.Fatalf("SavePMConfig() error = %v", err)
		}
		loaded := GetPMConfig(workdir)
		if loaded.Version != "0.1.0" {
			t.Errorf("Version = %q, want 0.1.0 (default)", loaded.Version)
		}
	})

	t.Run("GetPMConfig_emptyVersionAndBranch", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		hash, err := git.CreateCommitTree(workdir, `{"framework":"scrum"}`, "")
		if err != nil {
			t.Fatal(err)
		}
		git.WriteRef(workdir, "refs/gitmsg/pm/config", hash)
		config := GetPMConfig(workdir)
		if config.Version != "0.1.0" {
			t.Errorf("empty version should default to 0.1.0, got %q", config.Version)
		}
		if config.Branch != "gitmsg/pm" {
			t.Errorf("empty branch should default to gitmsg/pm, got %q", config.Branch)
		}
		if config.Framework != "scrum" {
			t.Errorf("Framework = %q, want scrum", config.Framework)
		}
	})

	t.Run("GetPMConfig_invalidJSON", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		hash, err := git.CreateCommitTree(workdir, "not valid json{", "")
		if err != nil {
			t.Fatal(err)
		}
		git.WriteRef(workdir, "refs/gitmsg/pm/config", hash)
		config := GetPMConfig(workdir)
		if config.Version != "0.1.0" {
			t.Errorf("invalid JSON should return default config, got %+v", config)
		}
	})

	t.Run("GetBoardConfig_default", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		board := GetBoardConfig(workdir, "")
		if board.Name == "" {
			t.Error("board name should not be empty")
		}
		if len(board.Columns) == 0 {
			t.Error("board should have columns")
		}
	})

	t.Run("SetBoardConfig_newBoard", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		board := BoardConfig{
			ID:   "custom",
			Name: "Custom Board",
			Columns: []ColumnConfig{
				{Name: "Todo", Filter: "state:open"},
				{Name: "Done", Filter: "state:closed"},
			},
		}
		if err := SetBoardConfig(workdir, board); err != nil {
			t.Fatalf("SetBoardConfig() error = %v", err)
		}
		loaded := GetBoardConfig(workdir, "custom")
		if loaded.ID != "custom" {
			t.Errorf("ID = %q, want custom", loaded.ID)
		}
		if len(loaded.Columns) != 2 {
			t.Errorf("len(Columns) = %d, want 2", len(loaded.Columns))
		}
	})

	t.Run("SetBoardConfig_updateExisting", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		board := BoardConfig{
			ID:      "update-me",
			Name:    "Original",
			Columns: []ColumnConfig{{Name: "Col1", Filter: "state:open"}},
		}
		SetBoardConfig(workdir, board)
		board.Name = "Updated"
		board.Columns = append(board.Columns, ColumnConfig{Name: "Col2", Filter: "state:closed"})
		if err := SetBoardConfig(workdir, board); err != nil {
			t.Fatalf("SetBoardConfig() update error = %v", err)
		}
		loaded := GetBoardConfig(workdir, "update-me")
		if loaded.Name != "Updated" {
			t.Errorf("Name = %q, want Updated", loaded.Name)
		}
		if len(loaded.Columns) != 2 {
			t.Errorf("len(Columns) = %d, want 2", len(loaded.Columns))
		}
	})

	t.Run("GetBoardView", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateIssue(workdir, "Open issue", "", CreateIssueOptions{})
		CreateIssue(workdir, "In progress issue", "", CreateIssueOptions{
			Labels: []Label{{Scope: "status", Value: "in-progress"}},
		})
		CreateIssue(workdir, "Closed issue", "", CreateIssueOptions{State: StateClosed})
		result := GetBoardView(workdir)
		if !result.Success {
			t.Fatalf("GetBoardView() failed: %s", result.Error.Message)
		}
		if len(result.Data.Columns) == 0 {
			t.Error("board view should have columns")
		}
		totalIssues := 0
		for _, col := range result.Data.Columns {
			totalIssues += len(col.Issues)
		}
		if totalIssues != 3 {
			t.Errorf("expected 3 total issues across columns, got %d", totalIssues)
		}
	})

	t.Run("GetBoardViewByID", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		board := BoardConfig{
			ID:   "test-board",
			Name: "Test Board",
			Columns: []ColumnConfig{
				{Name: "Open", Filter: "state:open"},
				{Name: "Closed", Filter: "state:closed"},
			},
		}
		SetBoardConfig(workdir, board)
		CreateIssue(workdir, "An issue", "", CreateIssueOptions{})
		result := GetBoardViewByID(workdir, "test-board")
		if !result.Success {
			t.Fatalf("GetBoardViewByID() failed: %s", result.Error.Message)
		}
		if result.Data.ID != "test-board" {
			t.Errorf("ID = %q, want test-board", result.Data.ID)
		}
	})

	t.Run("GetBoardView_unmatchedIssueFallsToFirstColumn", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		board := BoardConfig{
			ID:   "narrow",
			Name: "Narrow Board",
			Columns: []ColumnConfig{
				{Name: "Todo", Filter: "status:todo"},
				{Name: "Done", Filter: "state:closed"},
			},
		}
		SetBoardConfig(workdir, board)
		CreateIssue(workdir, "Unmatched issue", "", CreateIssueOptions{})
		result := GetBoardViewByID(workdir, "narrow")
		if !result.Success {
			t.Fatalf("GetBoardViewByID() failed: %s", result.Error.Message)
		}
		if len(result.Data.Columns[0].Issues) != 1 {
			t.Errorf("first column should have 1 unmatched issue, got %d", len(result.Data.Columns[0].Issues))
		}
	})
}

// --- Comment integration ---

func TestCommentIntegration(t *testing.T) {
	t.Parallel()

	t.Run("CommentOnItem", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		issue := CreateIssue(workdir, "Comment target", "", CreateIssueOptions{})
		if !issue.Success {
			t.Fatal(issue.Error.Message)
		}
		result := CommentOnItem(workdir, issue.Data.ID, "This is a comment")
		if !result.Success {
			t.Fatalf("CommentOnItem() failed: %s", result.Error.Message)
		}
	})

	t.Run("CommentOnItem_notFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := CommentOnItem(workdir, "nonexistent123456", "comment")
		if result.Success {
			t.Error("CommentOnItem() should fail for non-existent item")
		}
	})

	t.Run("GetItemComments", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		issue := CreateIssue(workdir, "Comments thread", "", CreateIssueOptions{})
		if !issue.Success {
			t.Fatal(issue.Error.Message)
		}
		CommentOnItem(workdir, issue.Data.ID, "First comment")
		CommentOnItem(workdir, issue.Data.ID, "Second comment")
		result := GetItemComments(issue.Data.ID, "local:"+workdir)
		if !result.Success {
			t.Fatalf("GetItemComments() failed: %s", result.Error.Message)
		}
		if len(result.Data) != 2 {
			t.Errorf("expected 2 comments, got %d", len(result.Data))
		}
	})

	t.Run("GetItemComments_notFound", func(t *testing.T) {
		t.Parallel()
		_ = cloneFixture(t)
		result := GetItemComments("nonexistent123456", "local:/tmp")
		if result.Success {
			t.Error("GetItemComments() should fail for non-existent item")
		}
	})
}

// --- Sync (sequential — resets shared DB) ---

func TestSyncWorkspaceToCache(t *testing.T) {
	workdir := initWorkspace(t)
	CreateIssue(workdir, "Sync issue 1", "", CreateIssueOptions{})
	CreateIssue(workdir, "Sync issue 2", "", CreateIssueOptions{})

	cache.Reset()
	dir := t.TempDir()
	if err := cache.Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})

	if err := SyncWorkspaceToCache(workdir); err != nil {
		t.Fatalf("SyncWorkspaceToCache() error = %v", err)
	}

	items, err := GetPMItems(PMQuery{Types: []string{string(ItemTypeIssue)}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 2 {
		t.Errorf("expected at least 2 synced issues, got %d", len(items))
	}
}

// --- Framework ---

func TestFrameworkIntegration(t *testing.T) {
	t.Parallel()

	t.Run("FrameworkFeatures_noConfig", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		milestones, sprints := FrameworkFeatures(workdir)
		if !milestones {
			t.Error("kanban should have milestones")
		}
		if sprints {
			t.Error("kanban should not have sprints")
		}
	})

	t.Run("FrameworkFeatures_withKanban", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		SavePMConfig(workdir, PMConfig{Framework: "kanban"})
		milestones, sprints := FrameworkFeatures(workdir)
		if !milestones {
			t.Error("kanban should have milestones")
		}
		if sprints {
			t.Error("kanban should not have sprints")
		}
	})

	t.Run("FrameworkFeatures_withScrum", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		SavePMConfig(workdir, PMConfig{Framework: "scrum"})
		milestones, sprints := FrameworkFeatures(workdir)
		if !milestones {
			t.Error("scrum should have milestones")
		}
		if !sprints {
			t.Error("scrum should have sprints")
		}
	})

	t.Run("FrameworkFeatures_minimalFramework", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		SavePMConfig(workdir, PMConfig{Framework: "minimal"})
		milestones, sprints := FrameworkFeatures(workdir)
		if milestones {
			t.Error("minimal should not have milestones")
		}
		if sprints {
			t.Error("minimal should not have sprints")
		}
	})

	t.Run("FrameworkFeatures_unknownFramework", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		SavePMConfig(workdir, PMConfig{Framework: "unknown"})
		milestones, sprints := FrameworkFeatures(workdir)
		if !milestones {
			t.Error("unknown framework should default to kanban milestones")
		}
		if sprints {
			t.Error("unknown framework should default to kanban sprints")
		}
	})
}
