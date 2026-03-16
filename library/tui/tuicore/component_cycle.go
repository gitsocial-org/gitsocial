// component_cycle.go - Inline cycling select field for Huh forms
package tuicore

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// FormLabelWidth is the standard width for inline form field labels.
const FormLabelWidth = 16

// PadLabel pads a label string to FormLabelWidth for consistent alignment.
func PadLabel(s string) string {
	w := AnsiWidth(s)
	if w >= FormLabelWidth {
		return s
	}
	return s + strings.Repeat(" ", FormLabelWidth-w)
}

// RequiredLabel appends a red asterisk to a label for required fields.
func RequiredLabel(s string) string {
	return s + " " + lipgloss.NewStyle().Foreground(lipgloss.Color(StatusError)).Render("*")
}

// FormFooter renders a form footer with keybinding hints and any validation errors.
func FormFooter(hint string, errs []error) string {
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(StatusError))
	var parts []string
	for _, e := range errs {
		if e != nil {
			parts = append(parts, errStyle.Render(e.Error()))
		}
	}
	if len(parts) > 0 {
		return Dim.Render(hint) + "  " + strings.Join(parts, "  ")
	}
	return Dim.Render(hint)
}

// FormTheme returns a customized huh theme for all forms.
func FormTheme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		t := huh.ThemeCharm(isDark)
		dim := lipgloss.Color(TextSecondary)

		t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(dim)
		t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(dim)
		t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(dim)
		t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(dim)
		t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(FormGreen)
		t.Focused.SelectedPrefix = lipgloss.NewStyle().Foreground(FormGreen).SetString("✓ ")

		t.Focused.FocusedButton = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")).
			Background(FormGreen)

		t.Blurred.SelectedOption = t.Blurred.SelectedOption.Foreground(FormGreen)
		t.Blurred.SelectedPrefix = lipgloss.NewStyle().Foreground(FormGreen).SetString("✓ ")

		t.FieldSeparator = lipgloss.NewStyle().SetString("\n")

		return t
	})
}

// CycleOption is a label-value pair for a CycleField.
type CycleOption struct {
	Label string
	Value string
}

// CycleField is a compact inline cycling select that implements huh.Field.
// It displays: Title  selected_value  (opt1 · opt2 · opt3)
// and cycles through options with left/right arrows.
type CycleField struct {
	key     string
	title   string
	options []CycleOption
	value   *string
	focused bool
	width   int
	height  int
	theme   huh.Theme
	keymap  huh.SelectKeyMap
}

// NewCycleField creates a new inline cycling select field.
func NewCycleField() *CycleField {
	return &CycleField{}
}

// Key sets the field key.
func (c *CycleField) Key(key string) *CycleField {
	c.key = key
	return c
}

// Title sets the field title.
func (c *CycleField) Title(title string) *CycleField {
	c.title = title
	return c
}

// Options sets the available options.
func (c *CycleField) Options(opts ...CycleOption) *CycleField {
	c.options = opts
	return c
}

// Value sets the pointer to the string value.
func (c *CycleField) Value(value *string) *CycleField {
	c.value = value
	return c
}

// selectedIndex returns the index of the currently selected option.
func (c *CycleField) selectedIndex() int {
	if c.value == nil {
		return 0
	}
	for i, opt := range c.options {
		if opt.Value == *c.value {
			return i
		}
	}
	return 0
}

// cycle moves to the next or previous option.
func (c *CycleField) cycle(delta int) {
	if len(c.options) == 0 || c.value == nil {
		return
	}
	idx := c.selectedIndex() + delta
	if idx < 0 {
		idx = len(c.options) - 1
	} else if idx >= len(c.options) {
		idx = 0
	}
	*c.value = c.options[idx].Value
}

func (c *CycleField) activeStyles() *huh.FieldStyles {
	theme := c.theme
	if theme == nil {
		theme = huh.ThemeFunc(huh.ThemeCharm)
	}
	styles := theme.Theme(true)
	if c.focused {
		return &styles.Focused
	}
	return &styles.Blurred
}

// Init initializes the field.
func (c *CycleField) Init() tea.Cmd { return nil }

// Update handles key messages.
func (c *CycleField) Update(msg tea.Msg) (huh.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, c.keymap.Left):
			c.cycle(-1)
		case key.Matches(msg, c.keymap.Right):
			c.cycle(1)
		case key.Matches(msg, c.keymap.Prev):
			return c, huh.PrevField
		case key.Matches(msg, c.keymap.Next, c.keymap.Submit):
			return c, huh.NextField
		}
	}
	return c, nil
}

// View renders the field.
func (c *CycleField) View() string {
	styles := c.activeStyles()
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(TextSecondary))

	var sb strings.Builder
	sb.WriteString(styles.Title.Render(PadLabel(c.title)))

	pink := lipgloss.NewStyle().Foreground(lipgloss.Color(AccentPink))
	sb.WriteString(pink.Render("> "))

	selectedLabel := ""
	if c.value != nil {
		for _, opt := range c.options {
			if opt.Value == *c.value {
				selectedLabel = opt.Label
				break
			}
		}
	}

	var optParts []string
	for _, opt := range c.options {
		if opt.Label == selectedLabel {
			optParts = append(optParts, styles.SelectedOption.Render(opt.Label))
		} else {
			optParts = append(optParts, dim.Render(opt.Label))
		}
	}
	sb.WriteString(strings.Join(optParts, dim.Render(" · ")))

	return styles.Base.Width(c.width).Render(sb.String())
}

// Focus focuses the field.
func (c *CycleField) Focus() tea.Cmd {
	c.focused = true
	c.keymap.Left.SetEnabled(true)
	c.keymap.Right.SetEnabled(true)
	return nil
}

// Blur blurs the field.
func (c *CycleField) Blur() tea.Cmd {
	c.focused = false
	return nil
}

// Error returns nil (no validation).
func (c *CycleField) Error() error { return nil }

// Run runs the field standalone.
func (c *CycleField) Run() error { return huh.Run(c) }

// RunAccessible runs the field in accessible mode.
func (c *CycleField) RunAccessible(w io.Writer, _ io.Reader) error {
	_, err := fmt.Fprintf(w, "%s: %s\n", c.title, c.GetValue())
	return err
}

// Skip returns false.
func (c *CycleField) Skip() bool { return false }

// Zoom returns false.
func (c *CycleField) Zoom() bool { return false }

// KeyBinds returns the keybindings.
func (c *CycleField) KeyBinds() []key.Binding {
	return []key.Binding{c.keymap.Left, c.keymap.Right, c.keymap.Next, c.keymap.Prev, c.keymap.Submit}
}

// WithTheme sets the theme.
func (c *CycleField) WithTheme(theme huh.Theme) huh.Field {
	if c.theme != nil {
		return c
	}
	c.theme = theme
	return c
}

// WithKeyMap sets the keymap.
func (c *CycleField) WithKeyMap(k *huh.KeyMap) huh.Field {
	c.keymap = k.Select
	c.keymap.Left.SetEnabled(c.focused)
	c.keymap.Right.SetEnabled(c.focused)
	return c
}

// WithWidth sets the width.
func (c *CycleField) WithWidth(width int) huh.Field {
	c.width = width
	return c
}

// WithHeight sets the height.
func (c *CycleField) WithHeight(height int) huh.Field {
	c.height = height
	return c
}

// WithPosition sets the field position.
func (c *CycleField) WithPosition(p huh.FieldPosition) huh.Field {
	c.keymap.Prev.SetEnabled(!p.IsFirst())
	c.keymap.Next.SetEnabled(!p.IsLast())
	c.keymap.Submit.SetEnabled(p.IsLast())
	return c
}

// GetKey returns the field key.
func (c *CycleField) GetKey() string { return c.key }

// GetValue returns the current value.
func (c *CycleField) GetValue() any {
	if c.value == nil {
		return ""
	}
	return *c.value
}

// SubmitField is a simple submit button that implements huh.Field.
type SubmitField struct {
	key     string
	label   string
	focused bool
	width   int
	height  int
	theme   huh.Theme
	keymap  huh.ConfirmKeyMap
}

// NewSubmitField creates a new submit button field.
func NewSubmitField() *SubmitField {
	return &SubmitField{key: "submit", label: "Submit"}
}

// Label sets the button label.
func (s *SubmitField) Label(label string) *SubmitField {
	s.label = label
	return s
}

func (s *SubmitField) activeStyles() *huh.FieldStyles {
	theme := s.theme
	if theme == nil {
		theme = huh.ThemeFunc(huh.ThemeCharm)
	}
	styles := theme.Theme(true)
	if s.focused {
		return &styles.Focused
	}
	return &styles.Blurred
}

func (s *SubmitField) Init() tea.Cmd { return nil }

func (s *SubmitField) Update(msg tea.Msg) (huh.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, s.keymap.Prev):
			return s, huh.PrevField
		case key.Matches(msg, s.keymap.Next, s.keymap.Submit, s.keymap.Accept):
			return s, huh.NextField
		}
	}
	return s, nil
}

func (s *SubmitField) View() string {
	styles := s.activeStyles()
	padded := " " + s.label + " "
	if s.focused {
		btn := lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")).
			Background(FormGreenDark)
		return styles.Base.Width(s.width).Render(btn.Render(padded))
	}
	btn := lipgloss.NewStyle().
		Foreground(lipgloss.Color("250")).
		Background(lipgloss.Color("237"))
	return styles.Base.Width(s.width).Render(btn.Render(padded))
}

func (s *SubmitField) Focus() tea.Cmd {
	s.focused = true
	return nil
}

func (s *SubmitField) Blur() tea.Cmd {
	s.focused = false
	return nil
}

func (s *SubmitField) Error() error { return nil }
func (s *SubmitField) Run() error   { return huh.Run(s) }
func (s *SubmitField) RunAccessible(w io.Writer, _ io.Reader) error {
	_, err := fmt.Fprintf(w, "%s\n", s.label)
	return err
}
func (s *SubmitField) Skip() bool { return false }
func (s *SubmitField) Zoom() bool { return false }
func (s *SubmitField) KeyBinds() []key.Binding {
	return []key.Binding{s.keymap.Next, s.keymap.Prev, s.keymap.Submit, s.keymap.Accept}
}
func (s *SubmitField) WithTheme(theme huh.Theme) huh.Field {
	if s.theme != nil {
		return s
	}
	s.theme = theme
	return s
}
func (s *SubmitField) WithKeyMap(k *huh.KeyMap) huh.Field { s.keymap = k.Confirm; return s }
func (s *SubmitField) WithWidth(width int) huh.Field      { s.width = width; return s }
func (s *SubmitField) WithHeight(height int) huh.Field    { s.height = height; return s }
func (s *SubmitField) WithPosition(p huh.FieldPosition) huh.Field {
	s.keymap.Prev.SetEnabled(!p.IsFirst())
	s.keymap.Next.SetEnabled(!p.IsLast())
	s.keymap.Submit.SetEnabled(p.IsLast())
	return s
}
func (s *SubmitField) GetKey() string { return s.key }
func (s *SubmitField) GetValue() any  { return true }
