// view_help.go - Help view showing concepts and keybinding reference from embedded docs
package tuicore

import (
	_ "embed"
	"strings"

	tea "charm.land/bubbletea/v2"
)

//go:generate cp ../../../../documentation/TUI-HELP.md help.md
//go:generate cp ../../../../documentation/TUI-KEYS.md help_keys.md

//go:embed help.md
var helpContent string

//go:embed help_keys.md
var helpKeysContent string

// HelpView displays the embedded help and keybinding documentation.
type HelpView struct {
	scroll int
	lines  []string
}

// NewHelpView creates a new help view.
func NewHelpView() *HelpView {
	return &HelpView{}
}

// Bindings returns keybindings for the help view.
func (v *HelpView) Bindings() []Binding {
	noop := func(ctx *HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []Binding{
		{Key: "j", Label: "scroll down", Contexts: []Context{Help}, Handler: noop},
		{Key: "k", Label: "scroll up", Contexts: []Context{Help}, Handler: noop},
		{Key: "ctrl+d", Label: "half-page down", Contexts: []Context{Help}, Handler: noop},
		{Key: "ctrl+u", Label: "half-page up", Contexts: []Context{Help}, Handler: noop},
		{Key: "home", Label: "top", Contexts: []Context{Help}, Handler: noop},
		{Key: "end", Label: "bottom", Contexts: []Context{Help}, Handler: noop},
	}
}

// Activate resets scroll and renders content when the view becomes active.
func (v *HelpView) Activate(state *State) tea.Cmd {
	v.scroll = 0
	width := state.InnerWidth()
	combined := helpContent + "\n" + helpKeysContent
	rendered := RenderMarkdown(combined, width)
	rendered = strings.TrimRight(rendered, "\n")
	v.lines = strings.Split(rendered, "\n")
	return nil
}

// Update handles messages.
func (v *HelpView) Update(msg tea.Msg, state *State) tea.Cmd {
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
		case "ctrl+d", "pgdown":
			v.scroll += state.InnerHeight() / 2
		case "ctrl+u", "pgup":
			v.scroll -= state.InnerHeight() / 2
			if v.scroll < 0 {
				v.scroll = 0
			}
		case "home", "g":
			v.scroll = 0
		case "end", "G":
			v.scroll = 99999
		}
	}
	return nil
}

// Render renders the help view.
func (v *HelpView) Render(state *State) string {
	wrapper := NewViewWrapper(state)
	if len(v.lines) == 0 {
		footer := RenderFooter(state.Registry, Help, wrapper.ContentWidth(), nil)
		return wrapper.Render(Dim.Render("Loading..."), footer)
	}
	height := wrapper.ContentHeight()
	if v.scroll >= len(v.lines) {
		v.scroll = len(v.lines) - 1
	}
	if v.scroll < 0 {
		v.scroll = 0
	}
	end := v.scroll + height
	if end > len(v.lines) {
		end = len(v.lines)
	}
	visible := strings.Join(v.lines[v.scroll:end], "\n")
	footer := RenderFooter(state.Registry, Help, wrapper.ContentWidth(), nil)
	return wrapper.Render(visible, footer)
}
