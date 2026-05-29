// prefs.go - User preferences for PM boards
package pm

// UserPrefs holds in-session board view preferences (collapse, swimlane); not persisted.
type UserPrefs struct {
	CollapsedColumns []string
	SwimlaneField    string
}

// SwimlaneFields defines available swimlane grouping options.
var SwimlaneFields = []string{"", "priority", "kind", "assignees", "author"}

// IsColumnCollapsed checks if a column is collapsed in user preferences.
func (p *UserPrefs) IsColumnCollapsed(name string) bool {
	for _, c := range p.CollapsedColumns {
		if c == name {
			return true
		}
	}
	return false
}

// ToggleColumnCollapsed toggles the collapsed state of a column.
func (p *UserPrefs) ToggleColumnCollapsed(name string) {
	for i, c := range p.CollapsedColumns {
		if c == name {
			p.CollapsedColumns = append(p.CollapsedColumns[:i], p.CollapsedColumns[i+1:]...)
			return
		}
	}
	p.CollapsedColumns = append(p.CollapsedColumns, name)
}

// CycleSwimlaneField cycles to the next swimlane grouping field.
func (p *UserPrefs) CycleSwimlaneField() {
	current := 0
	for i, f := range SwimlaneFields {
		if f == p.SwimlaneField {
			current = i
			break
		}
	}
	next := (current + 1) % len(SwimlaneFields)
	p.SwimlaneField = SwimlaneFields[next]
}
