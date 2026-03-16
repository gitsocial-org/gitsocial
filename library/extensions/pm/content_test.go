// content_test.go - Tests for pure content builder functions
package pm

import (
	"strings"
	"testing"
	"time"
)

func TestBuildIssueContent_subjectOnly(t *testing.T) {
	got := buildIssueContent("Fix login bug", "", CreateIssueOptions{})
	if !strings.Contains(got, "Fix login bug") {
		t.Errorf("should contain subject, got %q", got)
	}
	if !strings.Contains(got, `type="issue"`) {
		t.Error("should contain type=issue")
	}
	if !strings.Contains(got, `state="open"`) {
		t.Error("default state should be open")
	}
}

func TestBuildIssueContent_subjectAndBody(t *testing.T) {
	got := buildIssueContent("Fix login bug", "Detailed description", CreateIssueOptions{})
	if !strings.Contains(got, "Fix login bug") {
		t.Error("should contain subject")
	}
	if !strings.Contains(got, "Detailed description") {
		t.Error("should contain body")
	}
}

func TestBuildIssueContent_withAllFields(t *testing.T) {
	due := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	opts := CreateIssueOptions{
		State:     StateClosed,
		Assignees: []string{"alice@example.com", "bob@example.com"},
		Due:       &due,
		Milestone: "#commit:ms123@gitmsg/pm",
		Sprint:    "#commit:sp456@gitmsg/pm",
		Parent:    "#commit:parent789@gitmsg/pm",
		Root:      "#commit:root012@gitmsg/pm",
		Labels:    []Label{{Scope: "priority", Value: "high"}, {Value: "urgent"}},
	}
	got := buildIssueContent("Full issue", "Body text", opts)
	if !strings.Contains(got, `state="closed"`) {
		t.Error("should contain state=closed")
	}
	if !strings.Contains(got, `assignees="alice@example.com,bob@example.com"`) {
		t.Error("should contain assignees")
	}
	if !strings.Contains(got, `due="2025-12-31"`) {
		t.Error("should contain due date")
	}
	if !strings.Contains(got, `milestone="#commit:ms123@gitmsg/pm"`) {
		t.Error("should contain milestone ref")
	}
	if !strings.Contains(got, `sprint="#commit:sp456@gitmsg/pm"`) {
		t.Error("should contain sprint ref")
	}
	if !strings.Contains(got, `parent="#commit:parent789@gitmsg/pm"`) {
		t.Error("should contain parent ref")
	}
	if !strings.Contains(got, `root="#commit:root012@gitmsg/pm"`) {
		t.Error("should contain root ref")
	}
	if !strings.Contains(got, `labels="priority/high,urgent"`) {
		t.Error("should contain labels")
	}
}

func TestBuildIssueContentWithEdits(t *testing.T) {
	got := buildIssueContentWithEdits("Updated subject", "", CreateIssueOptions{}, "#commit:abc123@gitmsg/pm")
	if !strings.Contains(got, `edits="#commit:abc123@gitmsg/pm"`) {
		t.Errorf("should contain edits ref, got %q", got)
	}
}

func TestBuildIssueContentWithEdits_noEdits(t *testing.T) {
	got := buildIssueContentWithEdits("Subject", "", CreateIssueOptions{}, "")
	if strings.Contains(got, "edits=") {
		t.Error("should not contain edits field when empty")
	}
}

func TestBuildMilestoneContent_titleOnly(t *testing.T) {
	got := buildMilestoneContent("v1.0", "", StateOpen, nil, "", nil)
	if !strings.Contains(got, "v1.0") {
		t.Error("should contain title")
	}
	if !strings.Contains(got, `type="milestone"`) {
		t.Error("should contain type=milestone")
	}
	if !strings.Contains(got, `state="open"`) {
		t.Error("should contain state=open")
	}
	if strings.Contains(got, "due=") {
		t.Error("should not contain due when nil")
	}
}

func TestBuildMilestoneContent_withDueAndEdits(t *testing.T) {
	due := time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC)
	got := buildMilestoneContent("v2.0", "Major release", StateClosed, &due, "#commit:orig123@gitmsg/pm", nil)
	if !strings.Contains(got, "Major release") {
		t.Error("should contain body")
	}
	if !strings.Contains(got, `due="2025-06-30"`) {
		t.Error("should contain due date")
	}
	if !strings.Contains(got, `edits="#commit:orig123@gitmsg/pm"`) {
		t.Error("should contain edits ref")
	}
}

func TestBuildSprintContent_basic(t *testing.T) {
	start := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 10, 14, 0, 0, 0, 0, time.UTC)
	got := buildSprintContent("Sprint 1", "", SprintStatePlanned, start, end, "", nil)
	if !strings.Contains(got, "Sprint 1") {
		t.Error("should contain title")
	}
	if !strings.Contains(got, `type="sprint"`) {
		t.Error("should contain type=sprint")
	}
	if !strings.Contains(got, `state="planned"`) {
		t.Error("should contain state=planned")
	}
	if !strings.Contains(got, `start="2025-10-01"`) {
		t.Error("should contain start date")
	}
	if !strings.Contains(got, `end="2025-10-14"`) {
		t.Error("should contain end date")
	}
}

func TestBuildSprintContent_withBodyAndEdits(t *testing.T) {
	start := time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 11, 14, 0, 0, 0, 0, time.UTC)
	got := buildSprintContent("Sprint 2", "Sprint goals", SprintStateActive, start, end, "#commit:orig456@gitmsg/pm", nil)
	if !strings.Contains(got, "Sprint goals") {
		t.Error("should contain body")
	}
	if !strings.Contains(got, `state="active"`) {
		t.Error("should contain state=active")
	}
	if !strings.Contains(got, `edits="#commit:orig456@gitmsg/pm"`) {
		t.Error("should contain edits ref")
	}
}

func TestBuildRetractContent(t *testing.T) {
	got := buildRetractContent("#commit:abc123@gitmsg/pm")
	if !strings.Contains(got, `edits="#commit:abc123@gitmsg/pm"`) {
		t.Error("should contain edits ref")
	}
	if !strings.Contains(got, `retracted="true"`) {
		t.Error("should contain retracted=true")
	}
	if !strings.Contains(got, `ext="pm"`) {
		t.Error("should contain ext=pm")
	}
}
