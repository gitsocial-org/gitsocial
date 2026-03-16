// component_confirm.go - Reusable confirm dialog component
package tuicore

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ConfirmDialog is a reusable y/n confirmation prompt.
type ConfirmDialog struct {
	active      bool
	prompt      string
	destructive bool
	onConfirm   func() tea.Cmd
}

// Show activates the confirm dialog with a prompt and action callback.
func (d *ConfirmDialog) Show(prompt string, destructive bool, onConfirm func() tea.Cmd) {
	d.active = true
	d.prompt = prompt
	d.destructive = destructive
	d.onConfirm = onConfirm
}

// IsActive returns whether the dialog is showing.
func (d *ConfirmDialog) IsActive() bool {
	return d.active
}

// Reset dismisses the dialog.
func (d *ConfirmDialog) Reset() {
	d.active = false
	d.onConfirm = nil
}

// HandleKey processes y/n/esc input. Returns (handled, cmd).
func (d *ConfirmDialog) HandleKey(key string) (bool, tea.Cmd) {
	if !d.active {
		return false, nil
	}
	switch key {
	case "y", "Y":
		d.active = false
		onConfirm := d.onConfirm
		d.onConfirm = nil
		if onConfirm != nil {
			return true, onConfirm()
		}
		return true, nil
	case "n", "N", "esc":
		d.active = false
		d.onConfirm = nil
		return true, nil
	}
	return true, nil
}

// Render returns the styled confirmation prompt for the footer.
func (d *ConfirmDialog) Render() string {
	if !d.active {
		return ""
	}
	color := ConfirmAction
	if d.destructive {
		color = ConfirmDestructive
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true)
	return style.Render(d.prompt + " [y/n]")
}
