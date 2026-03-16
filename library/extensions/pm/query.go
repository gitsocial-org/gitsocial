// query.go - Query parsing for PM item filtering
package pm

import (
	"regexp"
	"strings"
	"time"
)

// Filter represents a single filter condition.
type Filter struct {
	Field    string // state, assignees, due, milestone, or label scope
	Value    string // filter value
	Negate   bool   // true if prefixed with -
	IsLabel  bool   // true if this is a label filter (scope:value)
	Operator string // for date comparisons: eq, lt, gt, lte, gte
}

// Query represents a parsed filter query.
type Query struct {
	Filters    []Filter
	TextSearch string // free text search
	SortField  string // priority, created, updated, due
	SortOrder  string // asc, desc
}

// knownFields are fields that map directly to database columns.
var knownFields = map[string]bool{
	"state":     true,
	"assignees": true,
	"due":       true,
	"milestone": true,
	"parent":    true,
	"root":      true,
}

// ParseQuery parses a filter string into a Query struct.
func ParseQuery(input string) Query {
	q := Query{
		SortField: "created",
		SortOrder: "desc",
	}
	if strings.TrimSpace(input) == "" {
		return q
	}

	// Extract quoted text search first
	textSearchRe := regexp.MustCompile(`"([^"]*)"`)
	if matches := textSearchRe.FindStringSubmatch(input); len(matches) > 1 {
		q.TextSearch = matches[1]
		input = textSearchRe.ReplaceAllString(input, "")
	}

	// Split remaining input by whitespace
	tokens := strings.Fields(input)
	for _, token := range tokens {
		// Skip OR for now (treat as implicit AND)
		if strings.ToUpper(token) == "OR" {
			continue
		}

		filter := parseFilterToken(token)
		if filter != nil {
			q.Filters = append(q.Filters, *filter)
		}
	}

	return q
}

// parseFilterToken parses a single filter token like "state:open" or "-kind:chore".
func parseFilterToken(token string) *Filter {
	negate := false
	if strings.HasPrefix(token, "-") {
		negate = true
		token = token[1:]
	}

	idx := strings.Index(token, ":")
	if idx < 0 {
		return nil // Not a filter token
	}

	field := token[:idx]
	value := token[idx+1:]
	if field == "" || value == "" {
		return nil
	}

	filter := &Filter{
		Field:  field,
		Value:  value,
		Negate: negate,
	}

	// Check if it's a known field or a label
	if !knownFields[field] {
		filter.IsLabel = true
	}

	// Parse date operators for due field
	if field == "due" {
		filter.Operator, filter.Value = parseDateValue(value)
	}

	return filter
}

// parseDateValue handles special date values like "today", "overdue", "week", "7d".
func parseDateValue(value string) (operator, resolved string) {
	today := time.Now().Format("2006-01-02")

	switch value {
	case "today":
		return "eq", today
	case "overdue":
		return "lt", today
	case "week":
		// Due within this week (next 7 days)
		weekEnd := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
		return "lte", weekEnd
	default:
		// Check for relative format like "7d"
		if strings.HasSuffix(value, "d") {
			daysStr := strings.TrimSuffix(value, "d")
			if days := parseInt(daysStr); days > 0 {
				target := time.Now().AddDate(0, 0, days).Format("2006-01-02")
				return "lte", target
			}
		}
		// Exact date
		return "eq", value
	}
}

// parseInt parses a string to int, returns 0 on error.
func parseInt(s string) int {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// escapeLike escapes SQL LIKE wildcards in user input.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}

// BuildWhereClause converts filters to SQL WHERE conditions.
func (q *Query) BuildWhereClause() (where string, args []interface{}) {
	var conditions []string

	for _, f := range q.Filters {
		cond, condArgs := f.toSQLCondition()
		if cond != "" {
			conditions = append(conditions, cond)
			args = append(args, condArgs...)
		}
	}

	// Text search on message content
	if q.TextSearch != "" {
		conditions = append(conditions, `v.resolved_message LIKE ? ESCAPE '\'`)
		args = append(args, "%"+escapeLike(q.TextSearch)+"%")
	}

	if len(conditions) == 0 {
		return "", nil
	}

	return strings.Join(conditions, " AND "), args
}

// toSQLCondition converts a single filter to SQL.
func (f *Filter) toSQLCondition() (string, []interface{}) {
	if f.IsLabel {
		// Label filter: check if labels field contains scope/value
		label := escapeLike(f.Field + "/" + f.Value)
		if f.Negate {
			return `(v.labels IS NULL OR v.labels NOT LIKE ? ESCAPE '\')`, []interface{}{"%" + label + "%"}
		}
		return `v.labels LIKE ? ESCAPE '\'`, []interface{}{"%" + label + "%"}
	}

	// Field filter
	col := fieldToColumn(f.Field)
	if col == "" {
		return "", nil
	}

	if f.Field == "due" {
		return f.dueSQLCondition(col)
	}

	if f.Negate {
		return col + " != ?", []interface{}{f.Value}
	}
	return col + " = ?", []interface{}{f.Value}
}

// dueSQLCondition handles date comparison operators.
func (f *Filter) dueSQLCondition(col string) (string, []interface{}) {
	op := "="
	switch f.Operator {
	case "lt":
		op = "<"
	case "gt":
		op = ">"
	case "lte":
		op = "<="
	case "gte":
		op = ">="
	}

	if f.Negate {
		// Flip the operator for negation
		switch op {
		case "=":
			op = "!="
		case "<":
			op = ">="
		case ">":
			op = "<="
		case "<=":
			op = ">"
		case ">=":
			op = "<"
		}
	}

	return col + " " + op + " ?", []interface{}{f.Value}
}

// fieldToColumn maps filter field names to SQL column names.
func fieldToColumn(field string) string {
	switch field {
	case "state":
		return "v.state"
	case "assignees":
		return "v.assignees"
	case "due":
		return "v.due"
	case "milestone":
		return "v.milestone_hash"
	case "parent":
		return "v.parent_hash"
	case "root":
		return "v.root_hash"
	default:
		return ""
	}
}

// BuildOrderClause returns SQL ORDER BY clause.
func (q *Query) BuildOrderClause() string {
	col := "v.timestamp"
	switch q.SortField {
	case "created":
		col = "v.timestamp"
	case "updated":
		col = "v.timestamp" // TODO: track updated separately
	case "due":
		col = "v.due"
	case "priority":
		// Sort by priority label value
		col = "CASE " +
			"WHEN v.labels LIKE '%priority/critical%' THEN 1 " +
			"WHEN v.labels LIKE '%priority/high%' THEN 2 " +
			"WHEN v.labels LIKE '%priority/medium%' THEN 3 " +
			"WHEN v.labels LIKE '%priority/low%' THEN 4 " +
			"ELSE 5 END"
	}

	order := "DESC"
	if q.SortOrder == "asc" {
		order = "ASC"
	}

	// For due date, put NULLs last
	if q.SortField == "due" {
		return "CASE WHEN " + col + " IS NULL THEN 1 ELSE 0 END, " + col + " " + order
	}

	return col + " " + order
}
