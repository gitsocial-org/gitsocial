// view_site.go - Site customization view for the static browser read-surface
package tuicore

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

var CoreSite = RegisterContext("core.site")

func init() {
	RegisterViewMeta(ViewMeta{Path: "/config/site", Context: CoreSite, Title: "Site", Icon: "◱", NavItemID: "config.site"})
}

// siteField identifies a single editable site-customization field.
type siteField struct {
	label string
	hint  string
	get   func(*objstore.SiteCustomization) *string
}

// siteFields lists the site customization fields in display order.
var siteFields = []siteField{
	{"title", "browser tab + header text", func(c *objstore.SiteCustomization) *string { return &c.Title }},
	{"accent", "hex color, e.g. #0a7", func(c *objstore.SiteCustomization) *string { return &c.Accent }},
	{"accentDark", "hex color for dark mode", func(c *objstore.SiteCustomization) *string { return &c.AccentDark }},
	{"favicon", "data:image/png|webp|svg+xml URI", func(c *objstore.SiteCustomization) *string { return &c.Favicon }},
	{"url", "absolute https:// base URL, e.g. https://example.com/", func(c *objstore.SiteCustomization) *string { return &c.URL }},
	{"description", "plain text, 300 chars max", func(c *objstore.SiteCustomization) *string { return &c.Description }},
	{"publish", "true/false: master switch for the static site (default false)", func(c *objstore.SiteCustomization) *string { return &c.Publish }},
	{"pages", "true/false: crawlable HTML pages (needs publish + url)", func(c *objstore.SiteCustomization) *string { return &c.Pages }},
}

// SiteView displays and edits the workspace's site customization (title, accent,
// accentDark, favicon, url, description). Values live in the core config's `site` sub-object, which
// `gitsocial site push` publishes as the static site's site-config.json.
type SiteView struct {
	config       objstore.SiteCustomization
	cursor       int
	lastClickIdx int
	editMode     bool
	input        textinput.Model
	err          string
	workdir      string
	zonePrefix   string
}

// NewSiteView creates a new site customization view.
func NewSiteView(workdir string) *SiteView {
	input := textinput.New()
	input.CharLimit = 512
	input.Prompt = "> "
	StyleTextInput(&input, Dim, lipgloss.NewStyle(), Dim)
	return &SiteView{
		input:        input,
		workdir:      workdir,
		lastClickIdx: -1,
		zonePrefix:   zone.NewPrefix(),
	}
}

// SetSize sets the view dimensions.
func (v *SiteView) SetSize(width, height int) {}

// Activate loads the site customization when the view becomes active.
func (v *SiteView) Activate(state *State) tea.Cmd {
	v.editMode = false
	v.input.Blur()
	v.err = ""
	v.cursor = 0
	if c, err := objstore.ReadWorkspaceSiteCustomization(v.workdir); err != nil {
		v.err = err.Error()
	} else {
		v.config = c
	}
	return nil
}

// Update handles messages and returns commands.
func (v *SiteView) Update(msg tea.Msg, state *State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if v.editMode {
			return nil
		}
		return v.handleMouse(msg)
	case tea.KeyPressMsg:
		return v.handleKey(msg)
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
func (v *SiteView) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg:
		idx := ZoneClicked(msg, len(siteFields), v.zonePrefix)
		if idx >= 0 {
			if idx == v.lastClickIdx && idx == v.cursor {
				v.lastClickIdx = -1
				return v.startEdit()
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
		} else if v.cursor < len(siteFields)-1 {
			v.cursor++
		}
	}
	return nil
}

// handleKey processes keyboard input.
func (v *SiteView) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if v.editMode {
		switch msg.String() {
		case "esc":
			v.editMode = false
			v.input.Blur()
			v.err = ""
			return nil
		case "enter":
			return v.saveCurrent()
		}
		var cmd tea.Cmd
		v.input, cmd = v.input.Update(msg)
		return cmd
	}
	switch msg.String() {
	case "j", "down":
		if v.cursor < len(siteFields)-1 {
			v.cursor++
		}
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "e", "enter":
		return v.startEdit()
	case "home":
		v.cursor = 0
	case "end":
		v.cursor = len(siteFields) - 1
	}
	return nil
}

// startEdit enters edit mode for the field under the cursor.
func (v *SiteView) startEdit() tea.Cmd {
	v.editMode = true
	v.input.SetValue(*siteFields[v.cursor].get(&v.config))
	v.err = ""
	return v.input.Focus()
}

// saveCurrent validates and persists the edited field, then reloads.
func (v *SiteView) saveCurrent() tea.Cmd {
	value := strings.TrimSpace(v.input.Value())
	label := siteFields[v.cursor].label
	if msg := validateSiteField(label, value); msg != "" {
		v.err = msg
		return nil
	}
	if label == "url" && value != "" {
		value, _ = objstore.NormalizeSiteURL(value)
	}
	updated := v.config
	*siteFields[v.cursor].get(&updated) = value
	if err := objstore.WriteWorkspaceSiteCustomization(v.workdir, updated); err != nil {
		v.err = err.Error()
		return nil
	}
	v.config = updated
	v.editMode = false
	v.input.Blur()
	v.err = ""
	return nil
}

// validateSiteField returns a human-readable error when value is invalid for the
// given field, mirroring the strict rules the push writer and site reader apply.
// Empty values are always allowed (they clear the field).
func validateSiteField(label, value string) string {
	if value == "" {
		return ""
	}
	switch label {
	case "accent", "accentDark":
		if !objstore.ValidSiteAccent(value) {
			return "invalid hex color (use #rgb or #rrggbb)"
		}
	case "favicon":
		if !objstore.ValidSiteFavicon(value) {
			return "invalid favicon (data:image/png|webp|svg+xml URI, max 32KB)"
		}
	case "url":
		if _, ok := objstore.NormalizeSiteURL(value); !ok {
			return "invalid URL (absolute https://, no query/fragment)"
		}
	case "description":
		if len(value) > objstore.SiteConfigMaxDescription {
			return "description too long (max 300 chars)"
		}
	case "publish", "pages":
		if value != "true" && value != "false" {
			return "must be true or false"
		}
	}
	return ""
}

// IsInputActive returns true if the view is handling text input.
func (v *SiteView) IsInputActive() bool { return v.editMode }

// Bindings returns keybindings for the site view.
func (v *SiteView) Bindings() []Binding {
	noop := func(ctx *HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []Binding{
		{Key: "e", Label: "edit", Contexts: []Context{CoreSite}, Handler: noop},
		{Key: "j", Label: "down", Contexts: []Context{CoreSite}, Handler: noop},
		{Key: "k", Label: "up", Contexts: []Context{CoreSite}, Handler: noop},
		{Key: "home", Label: "first", Contexts: []Context{CoreSite}, Handler: noop},
		{Key: "end", Label: "last", Contexts: []Context{CoreSite}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []Context{CoreSite}, Handler: push},
	}
}

// Render renders the site customization view.
func (v *SiteView) Render(state *State) string {
	wrapper := NewViewWrapper(state)
	rs := DefaultRowStyles()

	var b strings.Builder
	for i, f := range siteFields {
		value := *f.get(&v.config)
		display := value
		if display == "" {
			display = "(not set)"
		}
		if hint := f.hint; hint != "" {
			display += "  " + Dim.Render(hint)
		}
		var line string
		if i == v.cursor && v.editMode {
			line = RenderEditRow(rs, f.label, v.input.View())
		} else {
			line = RenderRow(rs, f.label, display, "", i == v.cursor)
		}
		b.WriteString(MarkZone(ZoneID(v.zonePrefix, i), line))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(Dim.Render("Saved to the core config; publish with 'p' or `gitsocial site push`."))
	b.WriteString("\n")

	if v.err != "" {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(StatusError).Render("Error: " + v.err))
	}

	footer := RenderFooter(state.Registry, CoreSite, nil)
	return wrapper.Render(b.String(), footer)
}
