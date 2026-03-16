// prefs.go - User preferences for PM boards
package pm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// UserPrefs stores per-repo board preferences.
type UserPrefs struct {
	CollapsedColumns   []string       `json:"collapsed_columns,omitempty"`
	WIPOverrides       map[string]int `json:"wip_overrides,omitempty"`
	SwimlaneField      string         `json:"swimlane_field,omitempty"`
	CollapsedSwimlanes []string       `json:"collapsed_swimlanes,omitempty"`
}

// SwimlaneFields defines available swimlane grouping options.
var SwimlaneFields = []string{"", "priority", "kind", "assignees", "author"}

// GetUserPrefs loads user preferences for a repository.
func GetUserPrefs(repoURL string) UserPrefs {
	path := userPrefsPath(repoURL)
	data, err := os.ReadFile(path)
	if err != nil {
		return UserPrefs{}
	}
	var prefs UserPrefs
	if err := json.Unmarshal(data, &prefs); err != nil {
		return UserPrefs{}
	}
	return prefs
}

// SaveUserPrefs saves user preferences for a repository.
func SaveUserPrefs(repoURL string, prefs UserPrefs) error {
	path := userPrefsPath(repoURL)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// userPrefsPath returns the path to user preferences for a repository.
func userPrefsPath(repoURL string) string {
	hash := sha256.Sum256([]byte(repoURL))
	hashStr := hex.EncodeToString(hash[:8])
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = os.Getenv("HOME")
	}
	return filepath.Join(configDir, "gitmsg", "boards", hashStr+".json")
}

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

// GetWIPOverride returns the WIP override for a column, or nil if none.
func (p *UserPrefs) GetWIPOverride(name string) *int {
	if p.WIPOverrides == nil {
		return nil
	}
	if wip, ok := p.WIPOverrides[name]; ok {
		return &wip
	}
	return nil
}

// SetWIPOverride sets a WIP override for a column.
func (p *UserPrefs) SetWIPOverride(name string, wip int) {
	if p.WIPOverrides == nil {
		p.WIPOverrides = make(map[string]int)
	}
	p.WIPOverrides[name] = wip
}

// ClearWIPOverride removes a WIP override for a column.
func (p *UserPrefs) ClearWIPOverride(name string) {
	if p.WIPOverrides != nil {
		delete(p.WIPOverrides, name)
	}
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
	// Clear collapsed swimlanes when changing field
	p.CollapsedSwimlanes = nil
}

// IsSwimlaneCollapsed checks if a swimlane is collapsed.
func (p *UserPrefs) IsSwimlaneCollapsed(name string) bool {
	for _, s := range p.CollapsedSwimlanes {
		if s == name {
			return true
		}
	}
	return false
}

// ToggleSwimlaneCollapsed toggles the collapsed state of a swimlane.
func (p *UserPrefs) ToggleSwimlaneCollapsed(name string) {
	for i, s := range p.CollapsedSwimlanes {
		if s == name {
			p.CollapsedSwimlanes = append(p.CollapsedSwimlanes[:i], p.CollapsedSwimlanes[i+1:]...)
			return
		}
	}
	p.CollapsedSwimlanes = append(p.CollapsedSwimlanes, name)
}
