// config.go - Extension configuration view for editing git config values
package tuicore

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
)

// configAddData holds the in-progress add-key form values.
type configAddData struct {
	Key   string
	Value string
}

// ConfigView displays and edits extension config.
type ConfigView struct {
	extension    string
	keys         []gitmsg.ConfigKeyValue
	cursor       int
	lastClickIdx int
	editMode     bool
	addMode      bool
	addForm      *huh.Form
	addData      configAddData
	input        textinput.Model
	err          string
	zonePrefix   string
}

// Bindings returns keybindings for the config view.
func (v *ConfigView) Bindings() []Binding {
	noop := func(ctx *HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []Binding{
		{Key: "e", Label: "edit", Contexts: []Context{Config}, Handler: noop},
		{Key: "a", Label: "add", Contexts: []Context{Config}, Handler: noop},
		{Key: "D", Label: "delete key", Contexts: []Context{Config}, Handler: noop},
		{Key: "j", Label: "down", Contexts: []Context{Config}, Handler: noop},
		{Key: "k", Label: "up", Contexts: []Context{Config}, Handler: noop},
		{Key: "home", Label: "first", Contexts: []Context{Config}, Handler: noop},
		{Key: "end", Label: "last", Contexts: []Context{Config}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []Context{Config}, Handler: push},
	}
}

// NewConfigView creates a new config view.
func NewConfigView() *ConfigView {
	input := textinput.New()
	input.CharLimit = 256
	input.Prompt = "> "
	StyleTextInput(&input, Dim, lipgloss.NewStyle(), Dim)

	return &ConfigView{
		input:        input,
		extension:    "core",
		lastClickIdx: -1,
		zonePrefix:   zone.NewPrefix(),
	}
}

// SetSize sets the view dimensions.
func (v *ConfigView) SetSize(width, height int) {
	// Config uses text rendering, not CardList
}

// SetExtension sets the extension to configure.
func (v *ConfigView) SetExtension(ext string) {
	v.extension = ext
	v.cursor = 0
	v.err = ""
}

// Extension returns the current extension being configured.
func (v *ConfigView) Extension() string {
	return v.extension
}

// Activate loads the config when the view becomes active.
func (v *ConfigView) Activate(state *State) tea.Cmd {
	if ext := state.Router.Location().Param("extension"); ext != "" {
		v.extension = ext
	}
	v.editMode = false
	v.addMode = false
	v.addForm = nil
	v.addData = configAddData{}
	return v.loadConfig(state.Workdir)
}

// keysHiddenByDedicatedViews are config keys that have their own dedicated views
// and should not appear in the raw config key-value list.
var keysHiddenByDedicatedViews = map[string]map[string]bool{
	"core": {"forks": true},
}

// loadConfig loads extension configuration from git config.
func (v *ConfigView) loadConfig(workdir string) tea.Cmd {
	ext := v.extension
	return func() tea.Msg {
		keys := gitmsg.ListExtConfig(workdir, ext)
		if hidden := keysHiddenByDedicatedViews[ext]; len(hidden) > 0 {
			filtered := keys[:0]
			for _, kv := range keys {
				if !hidden[kv.Key] {
					filtered = append(filtered, kv)
				}
			}
			keys = filtered
		}
		return ConfigViewLoadedMsg{Extension: ext, Keys: keys}
	}
}

// ConfigViewLoadedMsg is sent when config is loaded.
type ConfigViewLoadedMsg struct {
	Extension string
	Keys      []gitmsg.ConfigKeyValue
	Err       error
}

// HandleLoaded handles the loaded message.
func (v *ConfigView) HandleLoaded(msg ConfigViewLoadedMsg) {
	if msg.Err != nil {
		v.err = msg.Err.Error()
		return
	}
	v.keys = msg.Keys
	v.err = ""
}

// Update handles messages and returns commands.
func (v *ConfigView) Update(msg tea.Msg, state *State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if v.editMode || v.addMode {
			return nil
		}
		return v.handleMouse(msg)
	case tea.KeyPressMsg:
		return v.handleKey(msg, state.Workdir)
	case ConfigViewLoadedMsg:
		v.HandleLoaded(msg)
	default:
		switch {
		case v.editMode:
			var cmd tea.Cmd
			v.input, cmd = v.input.Update(msg)
			return cmd
		case v.addMode && v.addForm != nil:
			form, cmd := v.addForm.Update(msg)
			if f, ok := form.(*huh.Form); ok {
				v.addForm = f
			}
			if v.addForm.State == huh.StateCompleted {
				return v.submitAdd(state.Workdir)
			}
			return cmd
		}
	}
	return nil
}

// handleMouse processes mouse input.
func (v *ConfigView) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg:
		idx := ZoneClicked(msg, len(v.keys), v.zonePrefix)
		if idx >= 0 {
			if idx == v.lastClickIdx && idx == v.cursor {
				v.lastClickIdx = -1
				v.editMode = true
				v.input.SetValue(v.keys[v.cursor].Value)
				v.err = ""
				return v.input.Focus()
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
			if v.cursor < len(v.keys)-1 {
				v.cursor++
			}
		}
	}
	return nil
}

// handleKey processes keyboard input.
func (v *ConfigView) handleKey(msg tea.KeyPressMsg, workdir string) tea.Cmd {
	if v.addMode {
		return v.handleAddModeKey(msg, workdir)
	}
	if v.editMode {
		return v.handleEditModeKey(msg, workdir)
	}
	return v.handleNormalKey(msg)
}

// handleAddModeKey handles input in add mode.
func (v *ConfigView) handleAddModeKey(msg tea.KeyPressMsg, workdir string) tea.Cmd {
	if msg.String() == "esc" {
		v.addMode = false
		v.addForm = nil
		v.err = ""
		return nil
	}
	if v.addForm == nil {
		return nil
	}
	form, cmd := v.addForm.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		v.addForm = f
	}
	if v.addForm.State == huh.StateCompleted {
		return v.submitAdd(workdir)
	}
	return cmd
}

// startAddForm opens the inline huh form for a new config key+value.
func (v *ConfigView) startAddForm() tea.Cmd {
	v.addMode = true
	v.addData = configAddData{}
	v.err = ""

	keyField := huh.NewInput().
		Key("key").
		Title(PadLabel(RequiredLabel("Key"))).
		Placeholder("e.g. mykey").
		CharLimit(128).
		Value(&v.addData.Key).
		Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("key is required")
			}
			return nil
		})
	valueField := huh.NewInput().
		Key("value").
		Title(PadLabel("Value")).
		Placeholder("value").
		CharLimit(512).
		Value(&v.addData.Value)
	v.addForm = huh.NewForm(huh.NewGroup(keyField, valueField, NewSubmitField())).
		WithTheme(FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(FormKeyMap())
	return v.addForm.Init()
}

// submitAdd writes the new config entry and reloads.
func (v *ConfigView) submitAdd(workdir string) tea.Cmd {
	key := strings.TrimSpace(v.addData.Key)
	value := v.addData.Value
	v.addMode = false
	v.addForm = nil
	if key == "" {
		return nil
	}
	if err := gitmsg.SetExtConfigValue(workdir, v.extension, key, value); err != nil {
		v.err = err.Error()
		return nil
	}
	v.keys = gitmsg.ListExtConfig(workdir, v.extension)
	v.err = ""
	return nil
}

// handleEditModeKey handles input in edit mode.
func (v *ConfigView) handleEditModeKey(msg tea.KeyPressMsg, workdir string) tea.Cmd {
	switch msg.String() {
	case "esc":
		v.editMode = false
		v.input.Blur()
		v.err = ""
		return nil
	case "enter":
		if len(v.keys) > 0 && v.cursor < len(v.keys) {
			key := v.keys[v.cursor].Key
			value := v.input.Value()
			if err := gitmsg.SetExtConfigValue(workdir, v.extension, key, value); err != nil {
				v.err = err.Error()
				return nil
			}
			v.keys = gitmsg.ListExtConfig(workdir, v.extension)
			v.editMode = false
			v.input.Blur()
			v.err = ""
		}
		return nil
	}
	var cmd tea.Cmd
	v.input, cmd = v.input.Update(msg)
	return cmd
}

// handleNormalKey handles input in normal mode.
func (v *ConfigView) handleNormalKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		if len(v.keys) > 0 && v.cursor < len(v.keys)-1 {
			v.cursor++
		}
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "e", "enter":
		if len(v.keys) > 0 && v.cursor < len(v.keys) {
			v.editMode = true
			v.input.SetValue(v.keys[v.cursor].Value)
			v.err = ""
			return v.input.Focus()
		}
	case "a":
		return v.startAddForm()
	case "home":
		v.cursor = 0
	case "end":
		if len(v.keys) > 0 {
			v.cursor = len(v.keys) - 1
		}
	}
	return nil
}

// IsInputActive returns true if the view is handling text input.
func (v *ConfigView) IsInputActive() bool {
	return v.editMode || v.addMode
}

// Render renders the config view to a string.
func (v *ConfigView) Render(state *State) string {
	wrapper := NewViewWrapper(state)
	rs := DefaultRowStyles()

	var b strings.Builder
	if len(v.keys) == 0 && !v.addMode {
		b.WriteString(Dim.Render("No config set for this extension"))
		b.WriteString("\n\n")
		b.WriteString(Dim.Render("Press 'a' to add a new key"))
	} else {
		for i, kv := range v.keys {
			var line string
			if i == v.cursor {
				if v.editMode {
					line = RenderEditRow(rs, kv.Key, v.input.View())
				} else {
					line = RenderRow(rs, kv.Key, kv.Value, "", true)
				}
			} else {
				line = RenderRow(rs, kv.Key, kv.Value, "", false)
			}
			b.WriteString(MarkZone(ZoneID(v.zonePrefix, i), line))
			b.WriteString("\n")
		}
	}

	if v.addMode && v.addForm != nil {
		b.WriteString("\n")
		b.WriteString(v.addForm.View())
		b.WriteString("\n")
	}

	if v.err != "" {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(StatusError).Render("Error: " + v.err))
	}

	var footer string
	if v.addMode && v.addForm != nil {
		footer = FormFooter(false, v.addForm.Errors())
	} else {
		footer = RenderFooter(state.Registry, Config, nil)
	}
	return wrapper.Render(b.String(), footer)
}
