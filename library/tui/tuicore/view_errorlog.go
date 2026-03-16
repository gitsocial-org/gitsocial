// view_errorlog.go - Error log view showing session warnings and errors
package tuicore

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ErrorLogView displays accumulated session warnings and errors.
type ErrorLogView struct {
	scroll int
}

// NewErrorLogView creates a new error log view.
func NewErrorLogView() *ErrorLogView {
	return &ErrorLogView{}
}

// Bindings returns keybindings for the error log view.
func (v *ErrorLogView) Bindings() []Binding {
	noop := func(ctx *HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []Binding{
		{Key: "j", Label: "scroll down", Contexts: []Context{ErrorLog}, Handler: noop},
		{Key: "k", Label: "scroll up", Contexts: []Context{ErrorLog}, Handler: noop},
		{Key: "x", Label: "clear all", Contexts: []Context{ErrorLog}, Handler: noop},
	}
}

// Activate resets scroll when the view becomes active.
func (v *ErrorLogView) Activate(state *State) tea.Cmd {
	v.scroll = 0
	return nil
}

// Update handles messages.
func (v *ErrorLogView) Update(msg tea.Msg, state *State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		switch msg.(type) {
		case tea.MouseWheelMsg:
			m := msg.Mouse()
			if m.Button == tea.MouseWheelUp {
				v.scroll -= 3
				if v.scroll < 0 {
					v.scroll = 0
				}
			} else {
				v.scroll += 3
			}
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			v.scroll++
		case "k", "up":
			if v.scroll > 0 {
				v.scroll--
			}
		case "x":
			state.ClearErrorLog()
			v.scroll = 0
			return func() tea.Msg {
				return LogErrorMsg{} // trigger nav badge update with zero count
			}
		}
	}
	return nil
}

// Render renders the error log view.
func (v *ErrorLogView) Render(state *State) string {
	wrapper := NewViewWrapper(state)
	width := wrapper.ContentWidth()

	if len(state.ErrorLog) == 0 {
		content := Dim.Render("No errors or warnings in this session")
		footer := RenderFooter(state.Registry, ErrorLog, width, nil)
		return wrapper.Render(content, footer)
	}

	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(StatusError))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(StatusWarning))

	var lines []string
	// Show entries newest-first
	for i := len(state.ErrorLog) - 1; i >= 0; i-- {
		entry := state.ErrorLog[i]
		timestamp := Dim.Render(entry.Time.Format("15:04:05"))
		var severity string
		if entry.Severity == LogSeverityError {
			severity = errorStyle.Render("ERROR")
		} else {
			severity = warnStyle.Render("WARN ")
		}
		context := ""
		if entry.Context != "" {
			context = Dim.Render(fmt.Sprintf("[%s]", entry.Context)) + " "
		}
		line := fmt.Sprintf("%s %s %s%s", timestamp, severity, context, entry.Message)
		if width > 0 {
			line = lipgloss.NewStyle().Width(width).Render(line)
		}
		lines = append(lines, strings.Split(line, "\n")...)
	}

	height := wrapper.ContentHeight()
	if v.scroll >= len(lines) {
		v.scroll = len(lines) - 1
	}
	if v.scroll < 0 {
		v.scroll = 0
	}
	end := v.scroll + height
	if end > len(lines) {
		end = len(lines)
	}
	visible := strings.Join(lines[v.scroll:end], "\n")

	footer := RenderFooter(state.Registry, ErrorLog, width, nil)
	return wrapper.Render(visible, footer)
}
