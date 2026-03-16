// framework.go - Framework definitions for PM workflows
package pm

// Framework defines a PM workflow with labels and board configuration.
type Framework struct {
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	Labels        map[string]LabelConfig `json:"labels"`
	Board         FrameworkBoard         `json:"board"`
	HasMilestones bool                   `json:"hasMilestones"`
	HasSprints    bool                   `json:"hasSprints"`
}

// LabelConfig defines valid values for a label scope.
type LabelConfig struct {
	Values  []string `json:"values"`
	Default string   `json:"default,omitempty"`
	Single  bool     `json:"single,omitempty"`
}

// FrameworkBoard defines board columns for a framework.
type FrameworkBoard struct {
	Scope   string            `json:"scope"`
	Columns []FrameworkColumn `json:"columns"`
}

// FrameworkColumn defines a single board column.
type FrameworkColumn struct {
	Name   string `json:"name"`
	Filter string `json:"filter"`
	WIP    *int   `json:"wip,omitempty"`
}

var (
	// FrameworkMinimal is for individuals and quick capture.
	FrameworkMinimal = Framework{
		Name:        "minimal",
		Description: "Quick capture, no process overhead",
		Labels:      map[string]LabelConfig{},
		Board: FrameworkBoard{
			Scope: "status",
			Columns: []FrameworkColumn{
				{Name: "Open", Filter: "state:open"},
				{Name: "Closed", Filter: "state:closed"},
			},
		},
	}

	// FrameworkKanban is flow-based work management.
	FrameworkKanban = Framework{
		Name:          "kanban",
		Description:   "Flow-based with status columns",
		HasMilestones: true,
		Labels: map[string]LabelConfig{
			"status": {
				Values: []string{"in-progress", "review", "done"},
				Single: true,
			},
			"priority": {
				Values: []string{"critical", "high", "medium", "low"},
				Single: true,
			},
			"kind": {
				Values: []string{"bug", "feature", "task", "chore"},
				Single: true,
			},
		},
		Board: FrameworkBoard{
			Scope: "status",
			Columns: []FrameworkColumn{
				{Name: "Backlog", Filter: "state:open"},
				{Name: "In Progress", Filter: "status:in-progress", WIP: intPtr(3)},
				{Name: "Review", Filter: "status:review", WIP: intPtr(3)},
				{Name: "Done", Filter: "state:closed"},
			},
		},
	}

	// FrameworkScrum is sprint-based with story points.
	FrameworkScrum = Framework{
		Name:          "scrum",
		Description:   "Sprint-based with story points",
		HasMilestones: true,
		HasSprints:    true,
		Labels: map[string]LabelConfig{
			"status": {
				Values: []string{"sprint-backlog", "in-progress", "review"},
				Single: true,
			},
			"priority": {
				Values: []string{"critical", "high", "medium", "low"},
				Single: true,
			},
			"kind": {
				Values: []string{"story", "bug", "task", "spike"},
				Single: true,
			},
			"points": {
				Values: []string{"1", "2", "3", "5", "8", "13"},
				Single: true,
			},
		},
		Board: FrameworkBoard{
			Scope: "status",
			Columns: []FrameworkColumn{
				{Name: "Backlog", Filter: "state:open"},
				{Name: "Sprint", Filter: "status:sprint-backlog"},
				{Name: "In Progress", Filter: "status:in-progress"},
				{Name: "Review", Filter: "status:review"},
				{Name: "Done", Filter: "state:closed"},
			},
		},
	}

	// builtinFrameworks maps framework names to their definitions.
	builtinFrameworks = map[string]Framework{
		"minimal": FrameworkMinimal,
		"kanban":  FrameworkKanban,
		"scrum":   FrameworkScrum,
	}
)

// GetFramework returns a framework by name, or nil if not found.
func GetFramework(name string) *Framework {
	if f, ok := builtinFrameworks[name]; ok {
		return &f
	}
	return nil
}

// ListFrameworks returns all available framework names.
func ListFrameworks() []string {
	return []string{"minimal", "kanban", "scrum"}
}

// ValidateLabel checks if a label is valid for the given framework.
func (f *Framework) ValidateLabel(label Label) bool {
	if label.Scope == "" {
		return true // Unscoped labels are always valid
	}
	config, ok := f.Labels[label.Scope]
	if !ok {
		return true // Unknown scopes are allowed (custom labels)
	}
	for _, v := range config.Values {
		if v == label.Value {
			return true
		}
	}
	return false
}

// GetDefaultLabel returns the default label for a scope, if defined.
func (f *Framework) GetDefaultLabel(scope string) *Label {
	config, ok := f.Labels[scope]
	if !ok || config.Default == "" {
		return nil
	}
	return &Label{Scope: scope, Value: config.Default}
}

// FrameworkFeatures returns which features are enabled for the current framework.
func FrameworkFeatures(workdir string) (milestones bool, sprints bool) {
	config := GetPMConfig(workdir)
	fw := GetFramework(config.Framework)
	if fw == nil {
		return FrameworkKanban.HasMilestones, FrameworkKanban.HasSprints
	}
	return fw.HasMilestones, fw.HasSprints
}

// intPtr returns a pointer to an int value.
func intPtr(i int) *int {
	return &i
}
