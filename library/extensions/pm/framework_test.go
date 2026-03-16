// framework_test.go - Tests for PM framework definitions
package pm

import (
	"testing"
)

func TestGetFramework(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"minimal", true},
		{"kanban", true},
		{"scrum", true},
		{"nonexistent", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetFramework(tt.name)
			if (got != nil) != tt.want {
				t.Errorf("GetFramework(%q) != nil = %v, want %v", tt.name, got != nil, tt.want)
			}
		})
	}
}

func TestGetFramework_properties(t *testing.T) {
	kanban := GetFramework("kanban")
	if kanban == nil {
		t.Fatal("GetFramework(kanban) returned nil")
	}
	if kanban.Name != "kanban" {
		t.Errorf("Name = %q, want %q", kanban.Name, "kanban")
	}
	if !kanban.HasMilestones {
		t.Error("kanban should have milestones")
	}
	if kanban.HasSprints {
		t.Error("kanban should not have sprints")
	}

	scrum := GetFramework("scrum")
	if scrum == nil {
		t.Fatal("GetFramework(scrum) returned nil")
	}
	if !scrum.HasSprints {
		t.Error("scrum should have sprints")
	}
	if !scrum.HasMilestones {
		t.Error("scrum should have milestones")
	}

	minimal := GetFramework("minimal")
	if minimal == nil {
		t.Fatal("GetFramework(minimal) returned nil")
	}
	if minimal.HasMilestones {
		t.Error("minimal should not have milestones")
	}
}

func TestListFrameworks(t *testing.T) {
	frameworks := ListFrameworks()
	if len(frameworks) != 3 {
		t.Fatalf("len(ListFrameworks()) = %d, want 3", len(frameworks))
	}
	expected := map[string]bool{"minimal": true, "kanban": true, "scrum": true}
	for _, name := range frameworks {
		if !expected[name] {
			t.Errorf("unexpected framework: %q", name)
		}
	}
}

func TestValidateLabel(t *testing.T) {
	kanban := GetFramework("kanban")
	if kanban == nil {
		t.Fatal("kanban framework not found")
	}

	tests := []struct {
		name  string
		label Label
		want  bool
	}{
		{"valid status", Label{Scope: "status", Value: "in-progress"}, true},
		{"valid priority", Label{Scope: "priority", Value: "high"}, true},
		{"invalid status value", Label{Scope: "status", Value: "unknown"}, false},
		{"unknown scope (custom)", Label{Scope: "custom", Value: "anything"}, true},
		{"unscoped label", Label{Value: "urgent"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kanban.ValidateLabel(tt.label)
			if got != tt.want {
				t.Errorf("ValidateLabel(%+v) = %v, want %v", tt.label, got, tt.want)
			}
		})
	}
}

func TestGetDefaultLabel(t *testing.T) {
	kanban := GetFramework("kanban")
	if kanban == nil {
		t.Fatal("kanban framework not found")
	}

	// kanban has no default labels
	got := kanban.GetDefaultLabel("status")
	if got != nil {
		t.Errorf("kanban GetDefaultLabel(status) = %+v, want nil", got)
	}

	// unknown scope
	got = kanban.GetDefaultLabel("nonexistent")
	if got != nil {
		t.Errorf("GetDefaultLabel(nonexistent) = %+v, want nil", got)
	}
}

func TestGetDefaultLabel_withDefault(t *testing.T) {
	fw := Framework{
		Name: "custom",
		Labels: map[string]LabelConfig{
			"priority": {Values: []string{"low", "medium", "high"}, Default: "medium"},
		},
	}
	got := fw.GetDefaultLabel("priority")
	if got == nil {
		t.Fatal("GetDefaultLabel should return a label")
	}
	if got.Scope != "priority" || got.Value != "medium" {
		t.Errorf("GetDefaultLabel = %+v, want priority/medium", got)
	}
}

func TestFrameworkBoard(t *testing.T) {
	kanban := GetFramework("kanban")
	if kanban == nil {
		t.Fatal("kanban not found")
	}
	if kanban.Board.Scope != "status" {
		t.Errorf("Board.Scope = %q, want %q", kanban.Board.Scope, "status")
	}
	if len(kanban.Board.Columns) != 4 {
		t.Errorf("len(Board.Columns) = %d, want 4", len(kanban.Board.Columns))
	}

	scrum := GetFramework("scrum")
	if scrum == nil {
		t.Fatal("scrum not found")
	}
	if len(scrum.Board.Columns) != 5 {
		t.Errorf("scrum len(Board.Columns) = %d, want 5", len(scrum.Board.Columns))
	}

	// Check WIP limits on kanban
	for _, col := range kanban.Board.Columns {
		if col.Name == "In Progress" || col.Name == "Review" {
			if col.WIP == nil {
				t.Errorf("column %q should have WIP limit", col.Name)
			} else if *col.WIP != 3 {
				t.Errorf("column %q WIP = %d, want 3", col.Name, *col.WIP)
			}
		}
	}
}
