// prefs_test.go - Tests for user preferences
package pm

import (
	"testing"
)

func TestIsColumnCollapsed(t *testing.T) {
	p := UserPrefs{CollapsedColumns: []string{"Backlog", "Done"}}
	if !p.IsColumnCollapsed("Backlog") {
		t.Error("Backlog should be collapsed")
	}
	if !p.IsColumnCollapsed("Done") {
		t.Error("Done should be collapsed")
	}
	if p.IsColumnCollapsed("In Progress") {
		t.Error("In Progress should not be collapsed")
	}
}

func TestIsColumnCollapsed_empty(t *testing.T) {
	p := UserPrefs{}
	if p.IsColumnCollapsed("Backlog") {
		t.Error("empty prefs should not have collapsed columns")
	}
}

func TestToggleColumnCollapsed(t *testing.T) {
	p := UserPrefs{}

	// Collapse
	p.ToggleColumnCollapsed("Backlog")
	if !p.IsColumnCollapsed("Backlog") {
		t.Error("Backlog should be collapsed after toggle")
	}

	// Uncollapse
	p.ToggleColumnCollapsed("Backlog")
	if p.IsColumnCollapsed("Backlog") {
		t.Error("Backlog should not be collapsed after second toggle")
	}
}

func TestCycleSwimlaneField(t *testing.T) {
	p := UserPrefs{}
	if p.SwimlaneField != "" {
		t.Errorf("initial SwimlaneField = %q, want empty", p.SwimlaneField)
	}

	// Cycle through all fields
	expected := SwimlaneFields[1:]  // skip first empty
	expected = append(expected, "") // wraps back to empty
	for _, want := range expected {
		p.CycleSwimlaneField()
		if p.SwimlaneField != want {
			t.Errorf("after cycle: SwimlaneField = %q, want %q", p.SwimlaneField, want)
		}
	}
}
