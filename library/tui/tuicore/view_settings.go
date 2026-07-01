// settings.go - User settings view for editing application preferences
package tuicore

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

// SettingsView displays and edits user settings.
type SettingsView struct {
	data         *settings.Settings
	keys         []settings.KeyValue
	cursor       int
	lastClickIdx int
	editMode     bool
	input        textinput.Model
	err          string
	zonePrefix   string
	workdir      string

	// Callback to apply display settings
	onDisplayChange   func(showEmail bool)
	onExtensionChange func(ext string, enabled bool)
}

// Bindings returns keybindings for the settings view.
func (v *SettingsView) Bindings() []Binding {
	noop := func(ctx *HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []Binding{
		{Key: "e", Label: "edit", Contexts: []Context{Settings}, Handler: noop},
		{Key: "j", Label: "down", Contexts: []Context{Settings}, Handler: noop},
		{Key: "k", Label: "up", Contexts: []Context{Settings}, Handler: noop},
		{Key: "home", Label: "first", Contexts: []Context{Settings}, Handler: noop},
		{Key: "end", Label: "last", Contexts: []Context{Settings}, Handler: noop},
	}
}

// NewSettingsView creates a new settings view.
func NewSettingsView() *SettingsView {
	input := textinput.New()
	input.CharLimit = 256
	input.Prompt = "> "
	StyleTextInput(&input, Dim, lipgloss.NewStyle(), Dim)

	return &SettingsView{
		input:        input,
		lastClickIdx: -1,
		zonePrefix:   zone.NewPrefix(),
	}
}

// SetSize sets the view dimensions.
func (v *SettingsView) SetSize(width, height int) {
	// Settings uses text rendering, not CardList
}

// SetDisplayChangeCallback sets the callback for when display settings change.
func (v *SettingsView) SetDisplayChangeCallback(fn func(showEmail bool)) {
	v.onDisplayChange = fn
}

// SetExtensionChangeCallback sets the callback for when extension visibility changes.
func (v *SettingsView) SetExtensionChangeCallback(fn func(ext string, enabled bool)) {
	v.onExtensionChange = fn
}

// Activate loads settings when the view becomes active.
func (v *SettingsView) Activate(state *State) tea.Cmd {
	v.editMode = false
	v.workdir = state.Workdir
	return v.loadSettings()
}

// loadSettings loads settings from disk.
func (v *SettingsView) loadSettings() tea.Cmd {
	return func() tea.Msg {
		path, err := settings.DefaultPath()
		if err != nil {
			return SettingsViewLoadedMsg{Err: err}
		}
		s, err := settings.Load(path)
		if err != nil {
			return SettingsViewLoadedMsg{Err: err}
		}
		keys := settings.ListAll(s)
		return SettingsViewLoadedMsg{Settings: s, Keys: keys}
	}
}

// SettingsViewLoadedMsg is sent when settings are loaded.
type SettingsViewLoadedMsg struct {
	Settings *settings.Settings
	Keys     []settings.KeyValue
	Err      error
}

// HandleLoaded handles the loaded message.
func (v *SettingsView) HandleLoaded(msg SettingsViewLoadedMsg) {
	if msg.Err != nil {
		v.err = msg.Err.Error()
		return
	}
	v.data = msg.Settings
	v.keys = msg.Keys
	v.err = ""
	// Notify after data is updated so callbacks see the freshly-loaded values.
	// The notify calls in editOrCycleSetting/saveCurrentSetting fire before the
	// async reload completes and would otherwise pass stale data.
	v.notifyDisplayChange()
	v.notifyExtensionChange()
}

// Update handles messages and returns commands.
func (v *SettingsView) Update(msg tea.Msg, state *State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if v.editMode {
			return nil
		}
		return v.handleMouse(msg)
	case tea.KeyPressMsg:
		return v.handleKey(msg)
	case SettingsViewLoadedMsg:
		v.HandleLoaded(msg)
		if v.data != nil {
			state.ShowEmailOnCards = v.data.Display.ShowEmail
		}
	default:
		if v.editMode {
			var cmd tea.Cmd
			v.input, cmd = v.input.Update(msg)
			return cmd
		}
	}
	return nil
}

// handleMouse processes mouse input.
func (v *SettingsView) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg:
		idx := ZoneClicked(msg, len(v.keys), v.zonePrefix)
		if idx >= 0 {
			if idx == v.lastClickIdx && idx == v.cursor {
				v.lastClickIdx = -1
				return v.editOrCycleSetting()
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
func (v *SettingsView) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if v.editMode {
		switch msg.String() {
		case "esc":
			v.editMode = false
			v.input.Blur()
			v.err = ""
			return nil
		case "enter":
			return v.saveCurrentSetting()
		}
		var cmd tea.Cmd
		v.input, cmd = v.input.Update(msg)
		return cmd
	}

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
			return v.editOrCycleSetting()
		}
	case "home":
		v.cursor = 0
	case "end":
		if len(v.keys) > 0 {
			v.cursor = len(v.keys) - 1
		}
	}
	return nil
}

// editOrCycleSetting edits or cycles through setting values.
func (v *SettingsView) editOrCycleSetting() tea.Cmd {
	key := v.keys[v.cursor].Key
	if key == "fetch.workspace_mode" {
		originURL := protocol.NormalizeURL(git.GetOriginURL(v.workdir))
		if originURL == "" {
			return nil
		}
		current := settings.GetWorkspaceMode(originURL)
		if current == "" {
			current = "default"
		}
		next := settings.NextEnumValue(key, current)
		if err := settings.WriteWorkspaceMode(originURL, next); err != nil {
			log.Warn("failed to save workspace mode setting", "error", err)
			v.err = "Failed to save: " + err.Error()
		}
		return v.loadSettings()
	}
	if settings.IsEnum(key) {
		current, _ := settings.Get(v.data, key)
		next := settings.NextEnumValue(key, current)
		if err := v.writeSetting(key, next); err != nil {
			log.Warn("failed to save setting", "key", key, "error", err)
			v.err = "Failed to save: " + err.Error()
			return nil
		}
		v.notifyDisplayChange()
		v.notifyExtensionChange()
		return v.loadSettings()
	}
	v.editMode = true
	v.input.SetValue(v.keys[v.cursor].Value)
	v.err = ""
	return v.input.Focus()
}

// saveCurrentSetting saves the current setting value.
func (v *SettingsView) saveCurrentSetting() tea.Cmd {
	if len(v.keys) == 0 || v.cursor >= len(v.keys) {
		return nil
	}
	key := v.keys[v.cursor].Key
	value := v.input.Value()
	if err := v.writeSetting(key, value); err != nil {
		v.err = err.Error()
		return nil
	}
	v.notifyDisplayChange()
	v.notifyExtensionChange()
	v.editMode = false
	v.input.Blur()
	v.err = ""
	return v.loadSettings()
}

// writeSetting dispatches a settings write through the Manager so the value
// lands in the personal-config ref. The personal bare repo is auto-initialized
// on first write.
func (v *SettingsView) writeSetting(key, value string) error {
	return settings.NewManager().Write(key, value)
}

// categoryScopeLabel returns a dim suffix like " · synced" or " · local"
// describing where this category's keys live. Mixed-scope categories return
// the empty string so the header stays uncluttered.
func categoryScopeLabel(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	first, ok := settings.Lookup(keys[0])
	if !ok {
		return ""
	}
	for _, k := range keys[1:] {
		spec, ok := settings.Lookup(k)
		if !ok || spec.Scope != first.Scope {
			return ""
		}
	}
	switch first.Scope {
	case settings.ScopePersonalConfig:
		return "  " + Dim.Render("· synced")
	}
	return ""
}

// rowSuffix maps setting keys to a dim hint appended after the value (and any
// enum options). Used to surface units (e.g. "s" for seconds) and brief inline
// explainers for non-obvious settings.
var rowSuffix = map[string]string{
	"fetch.auto.enabled":  "pauses after 1h idle",
	"fetch.auto.interval": "s",
	"fetch.auto.backoff":  "doubles per empty cycle, max 30m",
}

// notifyDisplayChange notifies the callback of display setting changes.
func (v *SettingsView) notifyDisplayChange() {
	if v.onDisplayChange != nil && v.data != nil {
		v.onDisplayChange(v.data.Display.ShowEmail)
	}
}

// notifyExtensionChange notifies the callback of extension visibility changes.
func (v *SettingsView) notifyExtensionChange() {
	if v.onExtensionChange != nil && v.data != nil {
		v.onExtensionChange("social", v.data.Extensions.Social)
		v.onExtensionChange("pm", v.data.Extensions.PM)
		v.onExtensionChange("review", v.data.Extensions.Review)
		v.onExtensionChange("release", v.data.Extensions.Release)
		v.onExtensionChange("memo", v.data.Extensions.Memo)
	}
}

// resolveWorkspaceMode returns the workspace mode for the current workdir.
func (v *SettingsView) resolveWorkspaceMode(state *State) string {
	originURL := protocol.NormalizeURL(git.GetOriginURL(state.Workdir))
	if originURL == "" {
		return "(no origin)"
	}
	mode := settings.GetWorkspaceMode(originURL)
	if mode == "" {
		return "(not set)"
	}
	return mode
}

// IsInputActive returns true if the view is handling text input.
func (v *SettingsView) IsInputActive() bool {
	return v.editMode
}

// Render renders the settings view to a string.
func (v *SettingsView) Render(state *State) string {
	wrapper := NewViewWrapper(state)

	if v.data == nil {
		content := Dim.Render("Loading settings...")
		footer := RenderFooter(state.Registry, Settings, nil)
		return wrapper.Render(content, footer)
	}

	categories := []struct {
		name string
		keys []string
	}{
		{"Fetch", []string{"fetch.parallel", "fetch.timeout", "fetch.auto.enabled", "fetch.auto.interval", "fetch.auto.backoff"}},
		{"Workspace", []string{"fetch.workspace_mode"}},
		{"Output", []string{"output.color"}},
		{"Log", []string{"log.level"}},
		{"Display", []string{"display.show_email"}},
		{"Extensions", []string{"extensions.social", "extensions.pm", "extensions.review", "extensions.release", "extensions.memo"}},
	}

	rs := DefaultRowStyles()
	innerHeight := state.InnerHeight()

	var b strings.Builder
	lines := 0
	idx := 0
	for _, cat := range categories {
		if lines >= innerHeight-3 {
			break
		}
		header := cat.name + categoryScopeLabel(cat.keys)
		b.WriteString(RenderHeader(rs, header))
		b.WriteString("\n")
		lines++

		for _, key := range cat.keys {
			if lines >= innerHeight-3 {
				break
			}
			value := ""
			for _, kv := range v.keys {
				if kv.Key == key {
					value = kv.Value
					break
				}
			}
			if key == "fetch.workspace_mode" {
				value = v.resolveWorkspaceMode(state)
			}
			if value == "" {
				value = "(not set)"
			}

			displayValue := value
			if settings.IsEnum(key) {
				opts := settings.EnumOptions[key]
				displayValue = value + "  " + Dim.Render("("+strings.Join(opts, " · ")+")")
			}
			if suffix := rowSuffix[key]; suffix != "" {
				displayValue += "  " + Dim.Render(suffix)
			}

			var line string
			if idx == v.cursor {
				if v.editMode {
					line = RenderEditRow(rs, key, v.input.View())
				} else {
					line = RenderRow(rs, key, displayValue, "", true)
				}
			} else {
				line = RenderRow(rs, key, displayValue, "", false)
			}
			b.WriteString(MarkZone(ZoneID(v.zonePrefix, idx), line))
			b.WriteString("\n")
			lines++
			idx++
		}
		b.WriteString("\n")
		lines++
	}

	if v.err != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(StatusError).Render("Error: " + v.err))
		b.WriteString("\n")
	}

	footer := RenderFooter(state.Registry, Settings, nil)
	return wrapper.Render(b.String(), footer)
}
