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

func TestGetWIPOverride(t *testing.T) {
	p := UserPrefs{}
	if p.GetWIPOverride("Backlog") != nil {
		t.Error("empty prefs should return nil WIP override")
	}

	p.SetWIPOverride("Backlog", 5)
	got := p.GetWIPOverride("Backlog")
	if got == nil || *got != 5 {
		t.Errorf("GetWIPOverride(Backlog) = %v, want 5", got)
	}

	if p.GetWIPOverride("Done") != nil {
		t.Error("unset column should return nil")
	}
}

func TestSetWIPOverride(t *testing.T) {
	p := UserPrefs{}
	p.SetWIPOverride("In Progress", 3)
	p.SetWIPOverride("Review", 2)

	if got := p.GetWIPOverride("In Progress"); got == nil || *got != 3 {
		t.Errorf("In Progress WIP = %v, want 3", got)
	}
	if got := p.GetWIPOverride("Review"); got == nil || *got != 2 {
		t.Errorf("Review WIP = %v, want 2", got)
	}

	// Overwrite
	p.SetWIPOverride("In Progress", 10)
	if got := p.GetWIPOverride("In Progress"); got == nil || *got != 10 {
		t.Errorf("In Progress WIP after overwrite = %v, want 10", got)
	}
}

func TestClearWIPOverride(t *testing.T) {
	p := UserPrefs{}
	p.SetWIPOverride("Backlog", 5)
	p.ClearWIPOverride("Backlog")
	if p.GetWIPOverride("Backlog") != nil {
		t.Error("cleared WIP override should return nil")
	}
}

func TestClearWIPOverride_nilMap(t *testing.T) {
	p := UserPrefs{}
	p.ClearWIPOverride("Backlog") // should not panic
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

func TestCycleSwimlaneField_clearsCollapsed(t *testing.T) {
	p := UserPrefs{
		SwimlaneField:      "priority",
		CollapsedSwimlanes: []string{"high", "low"},
	}
	p.CycleSwimlaneField()
	if len(p.CollapsedSwimlanes) != 0 {
		t.Error("CycleSwimlaneField should clear collapsed swimlanes")
	}
}

func TestIsSwimlaneCollapsed(t *testing.T) {
	p := UserPrefs{CollapsedSwimlanes: []string{"high", "low"}}
	if !p.IsSwimlaneCollapsed("high") {
		t.Error("high should be collapsed")
	}
	if p.IsSwimlaneCollapsed("medium") {
		t.Error("medium should not be collapsed")
	}
}

func TestToggleSwimlaneCollapsed(t *testing.T) {
	p := UserPrefs{}

	p.ToggleSwimlaneCollapsed("high")
	if !p.IsSwimlaneCollapsed("high") {
		t.Error("high should be collapsed after toggle")
	}

	p.ToggleSwimlaneCollapsed("high")
	if p.IsSwimlaneCollapsed("high") {
		t.Error("high should not be collapsed after second toggle")
	}
}
