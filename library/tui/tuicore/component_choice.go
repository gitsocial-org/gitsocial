// component_choice.go - Reusable multi-choice dialog component
package tuicore

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Choice represents a single option in a ChoiceDialog.
type Choice struct {
	Key   string
	Label string
}

// ChoiceDialog is a reusable multi-choice prompt with labeled keys.
type ChoiceDialog struct {
	active   bool
	prompt   string
	choices  []Choice
	onChoice func(key string) tea.Cmd
}

// Show activates the choice dialog with a prompt, choices, and callback.
func (d *ChoiceDialog) Show(prompt string, choices []Choice, onChoice func(key string) tea.Cmd) {
	d.active = true
	d.prompt = prompt
	d.choices = choices
	d.onChoice = onChoice
}

// IsActive returns whether the dialog is showing.
func (d *ChoiceDialog) IsActive() bool {
	return d.active
}

// Reset dismisses the dialog.
func (d *ChoiceDialog) Reset() {
	d.active = false
	d.choices = nil
	d.onChoice = nil
}

// HandleKey processes choice key input. Returns (handled, cmd).
func (d *ChoiceDialog) HandleKey(key string) (bool, tea.Cmd) {
	if !d.active {
		return false, nil
	}
	if key == "esc" {
		d.active = false
		d.choices = nil
		d.onChoice = nil
		return true, nil
	}
	for _, c := range d.choices {
		if key == c.Key {
			d.active = false
			onChoice := d.onChoice
			choiceKey := c.Key
			d.choices = nil
			d.onChoice = nil
			if onChoice != nil {
				return true, onChoice(choiceKey)
			}
			return true, nil
		}
	}
	return true, nil
}

// Render returns the styled choice prompt for the footer.
func (d *ChoiceDialog) Render() string {
	if !d.active {
		return ""
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(ConfirmAction)).Bold(true)
	var parts []string
	parts = append(parts, d.prompt)
	for _, c := range d.choices {
		parts = append(parts, "["+c.Key+"]"+c.Label)
	}
	parts = append(parts, "esc")
	return style.Render(strings.Join(parts, "  "))
}
