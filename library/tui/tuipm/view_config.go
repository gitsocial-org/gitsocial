// config.go - PM configuration view with framework picker
package tuipm

import (
	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// ConfigView displays PM configuration with a framework picker.
type ConfigView struct {
	workdir      string
	framework    string
	cursor       int
	lastClickIdx int
	options      []frameworkOption
	err          string
	zonePrefix   string
}

type frameworkOption struct {
	name        string
	description string
}

var frameworkOptions = []frameworkOption{
	{"minimal", "Quick capture, no process overhead"},
	{"kanban", "Flow-based with status columns"},
	{"scrum", "Sprint-based with story points"},
}

// NewConfigView creates a new PM config view.
func NewConfigView(workdir string) *ConfigView {
	return &ConfigView{
		workdir:      workdir,
		options:      frameworkOptions,
		lastClickIdx: -1,
		zonePrefix:   zone.NewPrefix(),
	}
}

// Bindings returns keybindings for the config view.
func (v *ConfigView) Bindings() []tuicore.Binding {
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.PMConfig}, Handler: push},
	}
}

// SetSize sets the view dimensions.
func (v *ConfigView) SetSize(width, height int) {}

// Activate loads the config when the view becomes active.
func (v *ConfigView) Activate(state *tuicore.State) tea.Cmd {
	return v.loadConfig()
}

// loadConfig loads PM configuration.
func (v *ConfigView) loadConfig() tea.Cmd {
	return func() tea.Msg {
		config := pm.GetPMConfig(v.workdir)
		return pmConfigLoadedMsg{framework: config.Framework}
	}
}

type pmConfigLoadedMsg struct {
	framework string
}

// Update handles messages and returns commands.
func (v *ConfigView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		return v.handleMouse(msg)
	case tea.KeyPressMsg:
		return v.handleKey(msg)
	case pmConfigLoadedMsg:
		v.framework = msg.framework
		if v.framework == "" {
			v.framework = "kanban"
		}
		v.cursor = v.frameworkIndex(v.framework)
	case PMConfigSavedMsg:
		if msg.Err != "" {
			v.err = msg.Err
		} else {
			v.framework = msg.Framework
			v.err = ""
		}
	}
	return nil
}

// frameworkIndex returns the index of a framework in options.
func (v *ConfigView) frameworkIndex(name string) int {
	for i, opt := range v.options {
		if opt.name == name {
			return i
		}
	}
	return 0
}

// handleMouse processes mouse input.
func (v *ConfigView) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg:
		idx := tuicore.ZoneClicked(msg, len(v.options), v.zonePrefix)
		if idx >= 0 {
			if idx == v.lastClickIdx && idx == v.cursor {
				v.lastClickIdx = -1
				return v.selectFramework()
			}
			v.cursor = idx
			v.lastClickIdx = idx
		}
	case tea.MouseWheelMsg:
		m := msg.Mouse()
		if m.Button == tea.MouseWheelUp {
			if v.cursor > 0 {
				v.cursor--
			}
		} else {
			if v.cursor < len(v.options)-1 {
				v.cursor++
			}
		}
	}
	return nil
}

// handleKey processes keyboard input.
func (v *ConfigView) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		if v.cursor < len(v.options)-1 {
			v.cursor++
		}
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "enter":
		return v.selectFramework()
	}
	return nil
}

// selectFramework saves the selected framework.
func (v *ConfigView) selectFramework() tea.Cmd {
	selected := v.options[v.cursor].name
	if selected == v.framework {
		return nil // No change
	}
	return func() tea.Msg {
		config := pm.GetPMConfig(v.workdir)
		config.Framework = selected
		if err := pm.SavePMConfig(v.workdir, config); err != nil {
			return PMConfigSavedMsg{Err: err.Error()}
		}
		return PMConfigSavedMsg{Framework: selected}
	}
}

// PMConfigSavedMsg is sent when PM config is saved (exported for message bus).
type PMConfigSavedMsg struct {
	Framework string
	Err       string
}

// IsInputActive returns false as this view doesn't have text input.
func (v *ConfigView) IsInputActive() bool {
	return false
}

// Render renders the config view to a string.
func (v *ConfigView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	rs := tuicore.DefaultRowStyles()
	var content string
	content += tuicore.RenderHeader(rs, "Framework") + "\n"
	for i, opt := range v.options {
		selected := v.cursor == i
		current := opt.name == v.framework
		suffix := ""
		if current {
			suffix = " (current)"
		}
		row := tuicore.RenderRow(rs, opt.name, opt.description+suffix, "", selected)
		content += tuicore.MarkZone(tuicore.ZoneID(v.zonePrefix, i), row) + "\n"
	}
	if v.err != "" {
		content += "\n" + tuicore.Dim.Render("Error: "+v.err)
	}
	footer := tuicore.RenderFooter(state.Registry, tuicore.PMConfig, wrapper.ContentWidth(), nil)
	return wrapper.Render(content, footer)
}
