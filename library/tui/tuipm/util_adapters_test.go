// util_adapters_test.go - Tests for PM item type routing
package tuipm

import (
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

func TestIssueItem_ItemType(t *testing.T) {
	issue := pm.Issue{
		ID:      "issue123",
		Subject: "Test issue",
	}
	item := tuicore.NewItem(issue.ID, "pm", "issue", time.Now(), issue)
	itemType := item.ItemType()
	if itemType.Extension != "pm" {
		t.Errorf("Expected extension 'pm', got '%s'", itemType.Extension)
	}
	if itemType.Type != "issue" {
		t.Errorf("Expected type 'issue', got '%s'", itemType.Type)
	}
}

func TestMilestoneItem_ItemType(t *testing.T) {
	milestone := pm.Milestone{
		ID:    "milestone123",
		Title: "v1.0",
	}
	item := tuicore.NewItem(milestone.ID, "pm", "milestone", time.Now(), milestone)
	itemType := item.ItemType()
	if itemType.Extension != "pm" {
		t.Errorf("Expected extension 'pm', got '%s'", itemType.Extension)
	}
	if itemType.Type != "milestone" {
		t.Errorf("Expected type 'milestone', got '%s'", itemType.Type)
	}
}

func TestSprintItem_ItemType(t *testing.T) {
	sprint := pm.Sprint{
		ID:    "sprint123",
		Title: "Sprint 1",
	}
	item := tuicore.NewItem(sprint.ID, "pm", "sprint", time.Now(), sprint)
	itemType := item.ItemType()
	if itemType.Extension != "pm" {
		t.Errorf("Expected extension 'pm', got '%s'", itemType.Extension)
	}
	if itemType.Type != "sprint" {
		t.Errorf("Expected type 'sprint', got '%s'", itemType.Type)
	}
}

func TestPMNavTargetIntegration(t *testing.T) {
	// Test that PM nav targets are registered and work

	// Issue -> /pm/issue
	issue := pm.Issue{ID: "abc123"}
	issueItem := tuicore.NewItem(issue.ID, "pm", "issue", time.Now(), issue)
	loc := tuicore.GetNavTarget(issueItem)
	if loc.Path != "/pm/issue" {
		t.Errorf("Issue should navigate to /pm/issue, got %s", loc.Path)
	}
	if loc.Params["issueID"] != "abc123" {
		t.Errorf("Issue ID param should be 'abc123', got '%s'", loc.Params["issueID"])
	}

	// Milestone -> /pm/milestone
	milestone := pm.Milestone{ID: "def456"}
	milestoneItem := tuicore.NewItem(milestone.ID, "pm", "milestone", time.Now(), milestone)
	loc = tuicore.GetNavTarget(milestoneItem)
	if loc.Path != "/pm/milestone" {
		t.Errorf("Milestone should navigate to /pm/milestone, got %s", loc.Path)
	}
	if loc.Params["milestoneID"] != "def456" {
		t.Errorf("Milestone ID param should be 'def456', got '%s'", loc.Params["milestoneID"])
	}

	// Sprint -> /pm/sprint
	sprint := pm.Sprint{ID: "ghi789"}
	sprintItem := tuicore.NewItem(sprint.ID, "pm", "sprint", time.Now(), sprint)
	loc = tuicore.GetNavTarget(sprintItem)
	if loc.Path != "/pm/sprint" {
		t.Errorf("Sprint should navigate to /pm/sprint, got %s", loc.Path)
	}
	if loc.Params["sprintID"] != "ghi789" {
		t.Errorf("Sprint ID param should be 'ghi789', got '%s'", loc.Params["sprintID"])
	}
}
