// query_test.go - Tests for query parsing and SQL generation
package pm

import (
	"strings"
	"testing"
)

func TestParseQuery_empty(t *testing.T) {
	q := ParseQuery("")
	if len(q.Filters) != 0 {
		t.Errorf("ParseQuery('') filters = %d, want 0", len(q.Filters))
	}
	if q.SortField != "created" {
		t.Errorf("default SortField = %q, want %q", q.SortField, "created")
	}
	if q.SortOrder != "desc" {
		t.Errorf("default SortOrder = %q, want %q", q.SortOrder, "desc")
	}
}

func TestParseQuery_singleFilter(t *testing.T) {
	q := ParseQuery("state:open")
	if len(q.Filters) != 1 {
		t.Fatalf("len(filters) = %d, want 1", len(q.Filters))
	}
	if q.Filters[0].Field != "state" {
		t.Errorf("Field = %q, want %q", q.Filters[0].Field, "state")
	}
	if q.Filters[0].Value != "open" {
		t.Errorf("Value = %q, want %q", q.Filters[0].Value, "open")
	}
	if q.Filters[0].IsLabel {
		t.Error("state should not be a label filter")
	}
}

func TestParseQuery_multipleFilters(t *testing.T) {
	q := ParseQuery("state:open priority:high")
	if len(q.Filters) != 2 {
		t.Fatalf("len(filters) = %d, want 2", len(q.Filters))
	}
	if q.Filters[1].Field != "priority" {
		t.Errorf("second filter field = %q", q.Filters[1].Field)
	}
	if !q.Filters[1].IsLabel {
		t.Error("priority should be a label filter")
	}
}

func TestParseQuery_negatedFilter(t *testing.T) {
	q := ParseQuery("-kind:chore")
	if len(q.Filters) != 1 {
		t.Fatalf("len(filters) = %d, want 1", len(q.Filters))
	}
	if !q.Filters[0].Negate {
		t.Error("filter should be negated")
	}
	if q.Filters[0].Field != "kind" {
		t.Errorf("Field = %q, want %q", q.Filters[0].Field, "kind")
	}
}

func TestParseQuery_textSearch(t *testing.T) {
	q := ParseQuery(`state:open "fix login"`)
	if q.TextSearch != "fix login" {
		t.Errorf("TextSearch = %q, want %q", q.TextSearch, "fix login")
	}
	if len(q.Filters) != 1 {
		t.Errorf("len(filters) = %d, want 1", len(q.Filters))
	}
}

func TestParseQuery_dueToday(t *testing.T) {
	q := ParseQuery("due:today")
	if len(q.Filters) != 1 {
		t.Fatalf("len(filters) = %d, want 1", len(q.Filters))
	}
	if q.Filters[0].Operator != "eq" {
		t.Errorf("Operator = %q, want %q", q.Filters[0].Operator, "eq")
	}
	// Value should be today's date in YYYY-MM-DD format
	if len(q.Filters[0].Value) != 10 {
		t.Errorf("Value = %q, want YYYY-MM-DD format", q.Filters[0].Value)
	}
}

func TestParseQuery_dueOverdue(t *testing.T) {
	q := ParseQuery("due:overdue")
	if len(q.Filters) != 1 {
		t.Fatalf("len(filters) = %d, want 1", len(q.Filters))
	}
	if q.Filters[0].Operator != "lt" {
		t.Errorf("Operator = %q, want %q", q.Filters[0].Operator, "lt")
	}
}

func TestParseQuery_dueWeek(t *testing.T) {
	q := ParseQuery("due:week")
	if len(q.Filters) != 1 {
		t.Fatalf("len(filters) = %d, want 1", len(q.Filters))
	}
	if q.Filters[0].Operator != "lte" {
		t.Errorf("Operator = %q, want %q", q.Filters[0].Operator, "lte")
	}
}

func TestParseQuery_dueRelativeDays(t *testing.T) {
	q := ParseQuery("due:7d")
	if len(q.Filters) != 1 {
		t.Fatalf("len(filters) = %d, want 1", len(q.Filters))
	}
	if q.Filters[0].Operator != "lte" {
		t.Errorf("Operator = %q, want %q", q.Filters[0].Operator, "lte")
	}
}

func TestParseQuery_dueExactDate(t *testing.T) {
	q := ParseQuery("due:2025-12-31")
	if len(q.Filters) != 1 {
		t.Fatalf("len(filters) = %d, want 1", len(q.Filters))
	}
	if q.Filters[0].Operator != "eq" {
		t.Errorf("Operator = %q, want %q", q.Filters[0].Operator, "eq")
	}
	if q.Filters[0].Value != "2025-12-31" {
		t.Errorf("Value = %q, want %q", q.Filters[0].Value, "2025-12-31")
	}
}

func TestParseQuery_orIgnored(t *testing.T) {
	q := ParseQuery("state:open OR state:closed")
	if len(q.Filters) != 2 {
		t.Errorf("len(filters) = %d, want 2 (OR token ignored)", len(q.Filters))
	}
}

func TestParseQuery_noColonNotFilter(t *testing.T) {
	q := ParseQuery("freetext")
	if len(q.Filters) != 0 {
		t.Errorf("len(filters) = %d, want 0 (no colon = not a filter)", len(q.Filters))
	}
}

func TestBuildWhereClause_empty(t *testing.T) {
	q := ParseQuery("")
	where, args := q.BuildWhereClause()
	if where != "" {
		t.Errorf("where = %q, want empty", where)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want empty", args)
	}
}

func TestBuildWhereClause_stateFilter(t *testing.T) {
	q := ParseQuery("state:open")
	where, args := q.BuildWhereClause()
	if !strings.Contains(where, "v.state = ?") {
		t.Errorf("where = %q, want state condition", where)
	}
	if len(args) != 1 || args[0] != "open" {
		t.Errorf("args = %v, want [open]", args)
	}
}

func TestBuildWhereClause_negatedState(t *testing.T) {
	q := ParseQuery("-state:closed")
	where, args := q.BuildWhereClause()
	if !strings.Contains(where, "v.state != ?") {
		t.Errorf("where = %q, want negated state condition", where)
	}
	if len(args) != 1 || args[0] != "closed" {
		t.Errorf("args = %v, want [closed]", args)
	}
}

func TestBuildWhereClause_labelFilter(t *testing.T) {
	q := ParseQuery("priority:high")
	where, args := q.BuildWhereClause()
	if !strings.Contains(where, "v.labels LIKE ?") {
		t.Errorf("where = %q, want labels LIKE", where)
	}
	if len(args) != 1 || args[0] != "%priority/high%" {
		t.Errorf("args = %v, want [%%priority/high%%]", args)
	}
}

func TestBuildWhereClause_negatedLabel(t *testing.T) {
	q := ParseQuery("-kind:chore")
	where, _ := q.BuildWhereClause()
	if !strings.Contains(where, "NOT LIKE") {
		t.Errorf("where = %q, want NOT LIKE for negated label", where)
	}
}

func TestBuildWhereClause_textSearch(t *testing.T) {
	q := ParseQuery(`"login bug"`)
	where, args := q.BuildWhereClause()
	if !strings.Contains(where, "v.resolved_message LIKE ?") {
		t.Errorf("where = %q, want message LIKE", where)
	}
	if len(args) != 1 || args[0] != "%login bug%" {
		t.Errorf("args = %v, want [%%login bug%%]", args)
	}
}

func TestBuildWhereClause_dueDate(t *testing.T) {
	q := ParseQuery("due:overdue")
	where, args := q.BuildWhereClause()
	if !strings.Contains(where, "v.due < ?") {
		t.Errorf("where = %q, want due < ?", where)
	}
	if len(args) != 1 {
		t.Errorf("args len = %d, want 1", len(args))
	}
}

func TestBuildOrderClause_default(t *testing.T) {
	q := ParseQuery("")
	got := q.BuildOrderClause()
	if got != "v.timestamp DESC" {
		t.Errorf("BuildOrderClause() = %q, want %q", got, "v.timestamp DESC")
	}
}

func TestBuildOrderClause_due(t *testing.T) {
	q := Query{SortField: "due", SortOrder: "asc"}
	got := q.BuildOrderClause()
	if !strings.Contains(got, "v.due") {
		t.Errorf("BuildOrderClause(due) = %q, should contain v.due", got)
	}
	if !strings.Contains(got, "IS NULL THEN 1") {
		t.Error("BuildOrderClause(due) should put NULLs last")
	}
}

func TestBuildOrderClause_priority(t *testing.T) {
	q := Query{SortField: "priority", SortOrder: "asc"}
	got := q.BuildOrderClause()
	if !strings.Contains(got, "CASE") {
		t.Errorf("BuildOrderClause(priority) = %q, should use CASE", got)
	}
	if !strings.Contains(got, "critical") {
		t.Error("BuildOrderClause(priority) should reference critical")
	}
}

func TestFieldToColumn(t *testing.T) {
	tests := []struct {
		field string
		want  string
	}{
		{"state", "v.state"},
		{"assignees", "v.assignees"},
		{"due", "v.due"},
		{"milestone", "v.milestone_hash"},
		{"parent", "v.parent_hash"},
		{"root", "v.root_hash"},
		{"unknown", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got := fieldToColumn(tt.field)
			if got != tt.want {
				t.Errorf("fieldToColumn(%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

func TestDueSQLCondition_operators(t *testing.T) {
	tests := []struct {
		name     string
		operator string
		negate   bool
		wantOp   string
	}{
		{"gt", "gt", false, ">"},
		{"gte", "gte", false, ">="},
		{"lt", "lt", false, "<"},
		{"lte", "lte", false, "<="},
		{"eq", "eq", false, "="},
		{"negate eq", "eq", true, "!="},
		{"negate lt", "lt", true, ">="},
		{"negate gt", "gt", true, "<="},
		{"negate lte", "lte", true, ">"},
		{"negate gte", "gte", true, "<"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Filter{Field: "due", Value: "2025-01-01", Operator: tt.operator, Negate: tt.negate}
			cond, args := f.dueSQLCondition("v.due")
			if !strings.Contains(cond, tt.wantOp) {
				t.Errorf("dueSQLCondition() = %q, want operator %q", cond, tt.wantOp)
			}
			if len(args) != 1 || args[0] != "2025-01-01" {
				t.Errorf("args = %v, want [2025-01-01]", args)
			}
		})
	}
}

func TestBuildOrderClause_updated(t *testing.T) {
	q := Query{SortField: "updated", SortOrder: "desc"}
	got := q.BuildOrderClause()
	if got != "v.timestamp DESC" {
		t.Errorf("BuildOrderClause(updated) = %q, want %q", got, "v.timestamp DESC")
	}
}

func TestParseFilterToken_emptyFieldOrValue(t *testing.T) {
	if parseFilterToken(":value") != nil {
		t.Error("empty field should return nil")
	}
	if parseFilterToken("field:") != nil {
		t.Error("empty value should return nil")
	}
	if parseFilterToken("nocolon") != nil {
		t.Error("no colon should return nil")
	}
}

func TestToSQLCondition_unknownField(t *testing.T) {
	f := &Filter{Field: "nonexistent", Value: "val", IsLabel: false}
	cond, args := f.toSQLCondition()
	if cond != "" {
		t.Errorf("unknown field should return empty condition, got %q", cond)
	}
	if len(args) != 0 {
		t.Errorf("unknown field should return no args, got %v", args)
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"7", 7},
		{"14", 14},
		{"0", 0},
		{"abc", 0},
		{"12x", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseInt(tt.input)
			if got != tt.want {
				t.Errorf("parseInt(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
