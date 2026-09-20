// view_settings.go - User settings view for editing application preferences
package tuiviews

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
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// SettingsView displays and edits user settings.
type SettingsView struct {
	data         *settings.Settings
	rows         []settingsRow
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

// settingsRow is one rendered row: the key it acts on, its loaded value and description, and the category header above it.
type settingsRow struct {
	key    string
	value  string
	desc   string
	header string
}

// Bindings returns keybindings for the settings view.
func (v *SettingsView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []tuicore.Binding{
		{Key: "e", Label: "edit", Contexts: []tuicore.Context{tuicore.Settings}, Handler: noop},
		{Key: "j", Label: "down", Contexts: []tuicore.Context{tuicore.Settings}, Handler: noop},
		{Key: "k", Label: "up", Contexts: []tuicore.Context{tuicore.Settings}, Handler: noop},
		{Key: "home", Label: "first", Contexts: []tuicore.Context{tuicore.Settings}, Handler: noop},
		{Key: "end", Label: "last", Contexts: []tuicore.Context{tuicore.Settings}, Handler: noop},
	}
}

// NewSettingsView creates a new settings view.
func NewSettingsView() *SettingsView {
	input := textinput.New()
	input.CharLimit = 256
	input.Prompt = "> "
	tuicore.StyleTextInput(&input, tuicore.Dim, lipgloss.NewStyle(), tuicore.Dim)

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
func (v *SettingsView) Activate(state *tuicore.State) tea.Cmd {
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
	v.rows = buildSettingsRows(msg.Keys)
	if v.cursor >= len(v.rows) {
		v.cursor = len(v.rows) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
	v.err = ""
	// Notify after data is updated so callbacks see the freshly-loaded values.
	// The notify calls in editOrCycleSetting/saveCurrentSetting fire before the
	// async reload completes and would otherwise pass stale data.
	v.notifyDisplayChange()
	v.notifyExtensionChange()
}

// Update handles messages and returns commands.
func (v *SettingsView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
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
		idx := tuicore.ZoneClicked(msg, len(v.rows), v.zonePrefix)
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
			if v.cursor < len(v.rows)-1 {
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
		if len(v.rows) > 0 && v.cursor < len(v.rows)-1 {
			v.cursor++
		}
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "e", "enter":
		return v.editOrCycleSetting()
	case "home":
		v.cursor = 0
	case "end":
		if len(v.rows) > 0 {
			v.cursor = len(v.rows) - 1
		}
	}
	return nil
}

// currentRow returns the row under the cursor, and false when there is none.
func (v *SettingsView) currentRow() (settingsRow, bool) {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return settingsRow{}, false
	}
	return v.rows[v.cursor], true
}

// editOrCycleSetting edits or cycles through setting values.
func (v *SettingsView) editOrCycleSetting() tea.Cmd {
	row, ok := v.currentRow()
	if !ok {
		return nil
	}
	key := row.key
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
	v.input.SetValue(row.value)
	v.err = ""
	return v.input.Focus()
}

// saveCurrentSetting saves the current setting value.
func (v *SettingsView) saveCurrentSetting() tea.Cmd {
	row, ok := v.currentRow()
	if !ok {
		return nil
	}
	value := v.input.Value()
	if err := v.writeSetting(row.key, value); err != nil {
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

// sectionTitles names and orders the settings sections; a section absent here sorts last under its own name.
var sectionTitles = []struct {
	section string
	title   string
}{
	{"fetch", "Fetch"},
	{"workspace", "Workspace"},
	{"output", "Output"},
	{"log", "Log"},
	{"display", "Display"},
	{"extensions", "Extensions"},
	{"s3", "S3"},
}

// keysOwnedElsewhere names the keys another view edits; the Identity view toggles DNS verification and applies it live.
var keysOwnedElsewhere = map[string]bool{
	"identity.dns_verification": true,
}

// sectionOf returns the section a key groups under: the part before the first dot, with the per-repo workspace mode split out.
func sectionOf(key string) string {
	if key == "fetch.workspace_mode" {
		return "workspace"
	}
	section, _, ok := settings.ParseKey(key)
	if !ok {
		return key
	}
	return section
}

// sectionTitle returns the display name for a section, falling back to the section itself.
func sectionTitle(section string) string {
	for _, st := range sectionTitles {
		if st.section == section {
			return st.title
		}
	}
	return section
}

// sectionOrder lists every section present in keys, the named ones in display order and the rest in first-seen order.
func sectionOrder(keys []settings.KeyValue) []string {
	pending := make(map[string]bool, len(keys))
	for _, kv := range keys {
		pending[sectionOf(kv.Key)] = true
	}
	order := make([]string, 0, len(pending))
	for _, st := range sectionTitles {
		if pending[st.section] {
			order = append(order, st.section)
			delete(pending, st.section)
		}
	}
	for _, kv := range keys {
		if section := sectionOf(kv.Key); pending[section] {
			order = append(order, section)
			delete(pending, section)
		}
	}
	return order
}

// buildSettingsRows groups every loaded key into the ordered rows the view renders, cursors and edits.
func buildSettingsRows(all []settings.KeyValue) []settingsRow {
	keys := make([]settings.KeyValue, 0, len(all))
	for _, kv := range all {
		if !keysOwnedElsewhere[kv.Key] {
			keys = append(keys, kv)
		}
	}
	rows := make([]settingsRow, 0, len(keys))
	for _, section := range sectionOrder(keys) {
		group := make([]settings.KeyValue, 0, len(keys))
		names := make([]string, 0, len(keys))
		for _, kv := range keys {
			if sectionOf(kv.Key) == section {
				group = append(group, kv)
				names = append(names, kv.Key)
			}
		}
		header := sectionTitle(section) + categoryScopeLabel(names)
		for i, kv := range group {
			row := settingsRow{key: kv.Key, value: kv.Value, desc: kv.Description}
			if i == 0 {
				row.header = header
			}
			rows = append(rows, row)
		}
	}
	return rows
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
		return "  " + tuicore.Dim.Render("· synced")
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
	"display.theme":       "applies on restart",
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
func (v *SettingsView) resolveWorkspaceMode(state *tuicore.State) string {
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
func (v *SettingsView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)

	if v.data == nil {
		content := tuicore.Dim.Render("Loading settings...")
		footer := tuicore.RenderFooter(state.Registry, tuicore.Settings, nil)
		return wrapper.Render(content, footer)
	}

	rs := tuicore.DefaultRowStyles()
	innerHeight := state.InnerHeight()

	var b strings.Builder
	lines := 0
	selectedDesc := ""
	for idx, row := range v.rows {
		if lines >= innerHeight-3 {
			break
		}
		if row.header != "" {
			if idx > 0 {
				b.WriteString("\n")
				lines++
			}
			b.WriteString(tuicore.RenderHeader(rs, row.header))
			b.WriteString("\n")
			lines++
		}

		value := row.value
		if row.key == "fetch.workspace_mode" {
			value = v.resolveWorkspaceMode(state)
		}
		if value == "" {
			value = "(not set)"
		}

		displayValue := value
		if settings.IsEnum(row.key) {
			opts := settings.EnumOptions[row.key]
			displayValue = value + "  " + tuicore.Dim.Render("("+strings.Join(opts, " · ")+")")
		}
		if suffix := rowSuffix[row.key]; suffix != "" {
			displayValue += "  " + tuicore.Dim.Render(suffix)
		}

		var line string
		if idx == v.cursor {
			selectedDesc = row.desc
			if v.editMode {
				line = tuicore.RenderEditRow(rs, row.key, v.input.View())
			} else {
				line = tuicore.RenderRow(rs, row.key, displayValue, "", true)
			}
		} else {
			line = tuicore.RenderRow(rs, row.key, displayValue, "", false)
		}
		b.WriteString(tuicore.MarkZone(tuicore.ZoneID(v.zonePrefix, idx), line))
		b.WriteString("\n")
		lines++
	}
	if lines > 0 {
		b.WriteString("\n")
		lines++
	}

	if selectedDesc != "" && lines < innerHeight-3 {
		b.WriteString(tuicore.Dim.Render(tuicore.TruncateToWidth(selectedDesc, state.InnerWidth())))
		b.WriteString("\n")
	}

	if v.err != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(tuicore.StatusError).Render("Error: " + v.err))
		b.WriteString("\n")
	}

	footer := tuicore.RenderFooter(state.Registry, tuicore.Settings, nil)
	return wrapper.Render(b.String(), footer)
}
