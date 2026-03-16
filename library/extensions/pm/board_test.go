// board_test.go - Tests for board configuration and matching logic
package pm

import (
	"testing"
)

func TestDefaultPMConfig(t *testing.T) {
	config := DefaultPMConfig()
	if config.Version != "0.1.0" {
		t.Errorf("Version = %q, want %q", config.Version, "0.1.0")
	}
	if config.Branch != "gitmsg/pm" {
		t.Errorf("Branch = %q, want %q", config.Branch, "gitmsg/pm")
	}
	if config.Framework != "kanban" {
		t.Errorf("Framework = %q, want %q", config.Framework, "kanban")
	}
}

func TestDefaultBoardConfig(t *testing.T) {
	board := DefaultBoardConfig()
	if board.ID != "default" {
		t.Errorf("ID = %q, want %q", board.ID, "default")
	}
	if len(board.Columns) != 4 {
		t.Errorf("len(Columns) = %d, want 4", len(board.Columns))
	}
	if board.Columns[0].Name != "Backlog" {
		t.Errorf("first column = %q, want %q", board.Columns[0].Name, "Backlog")
	}
}

func TestResolveBoardConfig_customBoard(t *testing.T) {
	config := PMConfig{
		Boards: []BoardConfig{
			{ID: "custom", Name: "My Board", Columns: []ColumnConfig{{Name: "Todo", Filter: "state:open"}}},
		},
	}
	board := ResolveBoardConfig(config, "custom")
	if board.ID != "custom" {
		t.Errorf("ID = %q, want %q", board.ID, "custom")
	}
	if board.Name != "My Board" {
		t.Errorf("Name = %q, want %q", board.Name, "My Board")
	}
}

func TestResolveBoardConfig_firstBoard(t *testing.T) {
	config := PMConfig{
		Boards: []BoardConfig{
			{ID: "first", Name: "First"},
			{ID: "second", Name: "Second"},
		},
	}
	board := ResolveBoardConfig(config, "")
	if board.ID != "first" {
		t.Errorf("ID = %q, want %q (first board)", board.ID, "first")
	}
}

func TestResolveBoardConfig_framework(t *testing.T) {
	config := PMConfig{Framework: "kanban"}
	board := ResolveBoardConfig(config, "")
	if board.ID != "kanban" {
		t.Errorf("ID = %q, want %q", board.ID, "kanban")
	}
	if len(board.Columns) != 4 {
		t.Errorf("len(Columns) = %d, want 4", len(board.Columns))
	}
}

func TestResolveBoardConfig_default(t *testing.T) {
	config := PMConfig{}
	board := ResolveBoardConfig(config, "")
	if board.ID != "default" {
		t.Errorf("ID = %q, want %q (default fallback)", board.ID, "default")
	}
}

func TestResolveBoardConfig_unknownBoardID(t *testing.T) {
	config := PMConfig{
		Framework: "scrum",
		Boards:    []BoardConfig{{ID: "first", Name: "First"}},
	}
	// Unknown ID falls through to first board
	board := ResolveBoardConfig(config, "nonexistent")
	if board.ID != "first" {
		t.Errorf("ID = %q, want %q (first board fallback)", board.ID, "first")
	}
}

func TestMatchFilter_stateOpen(t *testing.T) {
	issue := Issue{State: StateOpen}
	if !matchFilter(issue, "state:open") {
		t.Error("matchFilter should match state:open")
	}
	if matchFilter(issue, "state:closed") {
		t.Error("matchFilter should not match state:closed")
	}
}

func TestMatchFilter_label(t *testing.T) {
	issue := Issue{
		State:  StateOpen,
		Labels: []Label{{Scope: "status", Value: "in-progress"}},
	}
	if !matchFilter(issue, "status:in-progress") {
		t.Error("matchFilter should match label status:in-progress")
	}
	if matchFilter(issue, "status:review") {
		t.Error("matchFilter should not match status:review")
	}
}

func TestMatchFilter_orFilters(t *testing.T) {
	issue := Issue{
		State:  StateOpen,
		Labels: []Label{{Scope: "priority", Value: "high"}},
	}
	if !matchFilter(issue, "priority:high,priority:critical") {
		t.Error("matchFilter should match OR filter")
	}
}

func TestMatchFilter_noColon(t *testing.T) {
	issue := Issue{State: StateOpen}
	if matchFilter(issue, "invalid") {
		t.Error("matchFilter should not match filter without colon")
	}
}

func TestMatchIssueToColumn(t *testing.T) {
	issue := Issue{
		State:  StateOpen,
		Labels: []Label{{Scope: "status", Value: "in-progress"}},
	}
	filters := []string{"state:open", "status:in-progress", "status:review", "state:closed"}

	got := matchIssueToColumn(issue, filters)
	// Should prefer label match (status:in-progress at index 1) over state match (state:open at index 0)
	if got != 1 {
		t.Errorf("matchIssueToColumn() = %d, want 1", got)
	}
}

func TestMatchIssueToColumn_stateOnly(t *testing.T) {
	issue := Issue{State: StateOpen}
	filters := []string{"state:open", "status:in-progress", "state:closed"}

	got := matchIssueToColumn(issue, filters)
	if got != 0 {
		t.Errorf("matchIssueToColumn() = %d, want 0 (state:open)", got)
	}
}

func TestMatchIssueToColumn_noMatch(t *testing.T) {
	issue := Issue{
		State:  StateCancelled,
		Labels: []Label{{Scope: "status", Value: "unknown"}},
	}
	filters := []string{"state:open", "state:closed"}

	got := matchIssueToColumn(issue, filters)
	if got != -1 {
		t.Errorf("matchIssueToColumn() = %d, want -1 (no match)", got)
	}
}

func TestResolveBoardConfig_frameworkUnknown(t *testing.T) {
	config := PMConfig{Framework: "nonexistent"}
	board := ResolveBoardConfig(config, "")
	if board.ID != "default" {
		t.Errorf("unknown framework should return default board, got ID = %q", board.ID)
	}
}
