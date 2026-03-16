// component_tag_input.go - Inline tag input with filtered dropdown for Huh forms
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

// TagOption is a label-value pair for a TagField suggestion.
type TagOption struct {
	Label string
	Value string
}

// TagField is an inline tag input with filtered dropdown that implements huh.Field.
// It displays selected values as [tag ×] chips and shows a dropdown when typing.
type TagField struct {
	key             string
	title           string
	placeholder     string
	suggestions     []TagOption
	value           *[]string
	input           string
	filtered        []TagOption
	suggestionIndex int
	showSuggestions bool
	tagCursor       int                 // -1 = on input, >=0 = on a tag
	maxTags         int                 // 0 = unlimited
	labelFunc       func(string) string // optional display transform for tag chips
	focused         bool
	width           int
	height          int
	theme           huh.Theme
	keymap          huh.SelectKeyMap
}

// NewTagField creates a new inline tag input field.
func NewTagField() *TagField {
	return &TagField{tagCursor: -1}
}

// Key sets the field key.
func (t *TagField) Key(k string) *TagField {
	t.key = k
	return t
}

// Title sets the field title.
func (t *TagField) Title(title string) *TagField {
	t.title = title
	return t
}

// Placeholder sets the placeholder text.
func (t *TagField) Placeholder(p string) *TagField {
	t.placeholder = p
	return t
}

// Options sets the available suggestions.
func (t *TagField) Options(opts ...TagOption) *TagField {
	t.suggestions = opts
	return t
}

// AppendOptions adds more suggestions to the existing set.
func (t *TagField) AppendOptions(opts ...TagOption) {
	t.suggestions = append(t.suggestions, opts...)
}

// MaxTags limits how many tags can be added (0 = unlimited).
func (t *TagField) MaxTags(n int) *TagField {
	t.maxTags = n
	return t
}

// LabelFunc sets a display transform for tag chip labels.
func (t *TagField) LabelFunc(fn func(string) string) *TagField {
	t.labelFunc = fn
	return t
}

// Value sets the pointer to the string slice value.
func (t *TagField) Value(value *[]string) *TagField {
	t.value = value
	return t
}

// isSelected returns true if the value is already in the tags list.
func (t *TagField) isSelected(val string) bool {
	if t.value == nil {
		return false
	}
	for _, tag := range *t.value {
		if strings.EqualFold(tag, val) {
			return true
		}
	}
	return false
}

// filterSuggestions updates the filtered list based on current input.
func (t *TagField) filterSuggestions() {
	t.filtered = nil
	query := strings.ToLower(strings.TrimSpace(t.input))
	for _, s := range t.suggestions {
		if t.isSelected(s.Value) {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(s.Label), query) || strings.Contains(strings.ToLower(s.Value), query) {
			t.filtered = append(t.filtered, s)
		}
	}
	if t.suggestionIndex >= len(t.filtered) {
		t.suggestionIndex = max(0, len(t.filtered)-1)
	}
}

// maxReached returns true if the tag limit has been hit.
func (t *TagField) maxReached() bool {
	return t.maxTags > 0 && t.tagCount() >= t.maxTags
}

// addTag adds a value to the tags list if not already present.
func (t *TagField) addTag(val string) {
	val = strings.TrimSpace(val)
	if val == "" || t.value == nil || t.isSelected(val) || t.maxReached() {
		return
	}
	*t.value = append(*t.value, val)
}

// removeTag removes a tag by index.
func (t *TagField) removeTag(idx int) {
	if t.value == nil || idx < 0 || idx >= len(*t.value) {
		return
	}
	*t.value = append((*t.value)[:idx], (*t.value)[idx+1:]...)
}

// tagCount returns the number of current tags.
func (t *TagField) tagCount() int {
	if t.value == nil {
		return 0
	}
	return len(*t.value)
}

func (t *TagField) activeStyles() *huh.FieldStyles {
	theme := t.theme
	if theme == nil {
		theme = huh.ThemeFunc(huh.ThemeCharm)
	}
	styles := theme.Theme(true)
	if t.focused {
		return &styles.Focused
	}
	return &styles.Blurred
}

// Init initializes the field.
func (t *TagField) Init() tea.Cmd { return nil }

// Update handles key messages.
func (t *TagField) Update(msg tea.Msg) (huh.Model, tea.Cmd) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return t, nil
	}

	// Tag navigation mode
	if t.tagCursor >= 0 {
		return t.updateTagMode(km)
	}

	// Input mode
	return t.updateInputMode(km)
}

// updateTagMode handles keys when a tag is focused.
func (t *TagField) updateTagMode(km tea.KeyPressMsg) (huh.Model, tea.Cmd) {
	switch km.String() {
	case "left":
		if t.tagCursor > 0 {
			t.tagCursor--
		}
	case "right":
		if t.tagCursor < t.tagCount()-1 {
			t.tagCursor++
		} else {
			t.tagCursor = -1
		}
	case "backspace", "delete":
		t.removeTag(t.tagCursor)
		if t.tagCount() == 0 {
			t.tagCursor = -1
		} else if t.tagCursor >= t.tagCount() {
			t.tagCursor = t.tagCount() - 1
		}
	case "enter", "esc":
		t.tagCursor = -1
	}
	return t, nil
}

// updateInputMode handles keys when the text input is focused.
func (t *TagField) updateInputMode(km tea.KeyPressMsg) (huh.Model, tea.Cmd) {
	// When max tags reached, only allow navigation and tag deletion
	if t.maxReached() {
		switch {
		case km.String() == "backspace", km.String() == "left":
			if t.tagCount() > 0 {
				t.tagCursor = t.tagCount() - 1
			}
		case km.String() == "esc":
			return t, huh.PrevField
		case key.Matches(km, t.keymap.Prev):
			return t, huh.PrevField
		case key.Matches(km, t.keymap.Next, t.keymap.Submit):
			return t, huh.NextField
		}
		return t, nil
	}

	switch {
	case km.String() == "up":
		if t.showSuggestions && len(t.filtered) > 0 {
			t.suggestionIndex--
			if t.suggestionIndex < 0 {
				t.suggestionIndex = len(t.filtered) - 1
			}
		}
	case km.String() == "down":
		if t.showSuggestions && len(t.filtered) > 0 {
			t.suggestionIndex++
			if t.suggestionIndex >= len(t.filtered) {
				t.suggestionIndex = 0
			}
		}
	case km.String() == "enter":
		if t.showSuggestions && len(t.filtered) > 0 && t.suggestionIndex < len(t.filtered) {
			t.addTag(t.filtered[t.suggestionIndex].Value)
			t.input = ""
			t.showSuggestions = false
			t.filterSuggestions()
		} else if strings.TrimSpace(t.input) != "" {
			t.addTag(t.input)
			t.input = ""
			t.showSuggestions = false
			t.filterSuggestions()
		}
	case km.String() == ",":
		if strings.TrimSpace(t.input) != "" {
			t.addTag(t.input)
			t.input = ""
			t.showSuggestions = false
			t.filterSuggestions()
		}
	case km.String() == "backspace":
		if t.input == "" && t.tagCount() > 0 {
			t.tagCursor = t.tagCount() - 1
		} else if len(t.input) > 0 {
			t.input = t.input[:len(t.input)-1]
			t.showSuggestions = t.input != "" && len(t.suggestions) > 0
			t.filterSuggestions()
		}
	case km.String() == "left":
		if t.input == "" && t.tagCount() > 0 {
			t.tagCursor = t.tagCount() - 1
		}
	case km.String() == "esc":
		if t.showSuggestions {
			t.showSuggestions = false
		} else {
			return t, huh.PrevField
		}
	case key.Matches(km, t.keymap.Prev):
		if !t.showSuggestions || len(t.filtered) == 0 {
			return t, huh.PrevField
		}
	case key.Matches(km, t.keymap.Next, t.keymap.Submit):
		if !t.showSuggestions || len(t.filtered) == 0 {
			if strings.TrimSpace(t.input) != "" {
				t.addTag(t.input)
				t.input = ""
				t.showSuggestions = false
				t.filterSuggestions()
			}
			return t, huh.NextField
		}
	default:
		text := km.Text
		if text != "" && text != " " && !t.maxReached() {
			t.input += text
			t.showSuggestions = len(t.suggestions) > 0
			t.suggestionIndex = 0
			t.filterSuggestions()
		} else if km.String() == "space" {
			t.input += " "
			t.filterSuggestions()
		}
	}
	return t, nil
}

// View renders the field.
func (t *TagField) View() string {
	styles := t.activeStyles()
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(TextSecondary))
	pink := lipgloss.NewStyle().Foreground(lipgloss.Color(AccentPink))
	tagStyle := lipgloss.NewStyle().Foreground(FormGreen)
	tagSelectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(TextPrimary)).Background(lipgloss.Color(BgSelected))
	removeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(TextSecondary))
	removeSelectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(StatusError)).Background(lipgloss.Color(BgSelected))

	var sb strings.Builder
	sb.WriteString(styles.Title.Render(PadLabel(t.title)))
	sb.WriteString(pink.Render("> "))

	// Render tags
	for i := 0; i < t.tagCount(); i++ {
		tag := (*t.value)[i]
		displayTag := tag
		if t.labelFunc != nil {
			displayTag = t.labelFunc(tag)
		}
		if t.focused && t.tagCursor == i {
			sb.WriteString(tagSelectedStyle.Render("[" + displayTag + " "))
			sb.WriteString(removeSelectedStyle.Render("×"))
			sb.WriteString(tagSelectedStyle.Render("]"))
		} else {
			sb.WriteString(tagStyle.Render("[" + displayTag))
			sb.WriteString(removeStyle.Render(" ×"))
			sb.WriteString(tagStyle.Render("]"))
		}
		sb.WriteString(" ")
	}

	// Render input or placeholder
	if t.focused && t.tagCursor < 0 {
		if t.maxReached() {
			// Max tags reached: no input area, no cursor
		} else if t.input != "" {
			sb.WriteString(t.input)
			sb.WriteString(styles.TextInput.Cursor.Render(" "))
		} else if t.tagCount() == 0 && t.placeholder != "" {
			sb.WriteString(dim.Render(t.placeholder))
			sb.WriteString(styles.TextInput.Cursor.Render(" "))
		} else {
			sb.WriteString(styles.TextInput.Cursor.Render(" "))
		}
	} else if !t.focused && t.tagCount() == 0 && t.placeholder != "" {
		sb.WriteString(dim.Render(t.placeholder))
	}

	line := styles.Base.Width(t.width).Render(sb.String())

	// Render dropdown
	if t.focused && t.showSuggestions && len(t.filtered) > 0 {
		line += "\n" + t.renderDropdown(styles)
	}

	return line
}

// renderDropdown renders the suggestion dropdown below the input line.
func (t *TagField) renderDropdown(styles *huh.FieldStyles) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(TextSecondary))
	highlight := lipgloss.NewStyle().Foreground(lipgloss.Color(TextPrimary)).Bold(true)
	bar := dim.Render("┃ ")

	// Compute label offset to align with the input area
	offset := strings.Repeat(" ", FormLabelWidth+2) // +2 for "> "

	maxShow := 5
	showing := t.filtered
	if len(showing) > maxShow {
		showing = showing[:maxShow]
	}

	lines := make([]string, 0, len(showing))
	for i, opt := range showing {
		var line string
		if i == t.suggestionIndex {
			line = offset + bar + highlight.Render(opt.Label)
		} else {
			line = offset + bar + dim.Render(opt.Label)
		}
		lines = append(lines, styles.Base.Width(t.width).Render(line))
	}

	return strings.Join(lines, "\n")
}

// Focus focuses the field.
func (t *TagField) Focus() tea.Cmd {
	t.focused = true
	t.tagCursor = -1
	t.filterSuggestions()
	return nil
}

// Blur blurs the field.
func (t *TagField) Blur() tea.Cmd {
	t.focused = false
	t.showSuggestions = false
	// Commit any pending input
	if strings.TrimSpace(t.input) != "" {
		t.addTag(t.input)
		t.input = ""
	}
	t.tagCursor = -1
	return nil
}

// Error returns nil (no validation).
func (t *TagField) Error() error { return nil }

// Run runs the field standalone.
func (t *TagField) Run() error { return huh.Run(t) }

// RunAccessible runs the field in accessible mode.
func (t *TagField) RunAccessible(w io.Writer, _ io.Reader) error {
	tags := ""
	if t.value != nil {
		tags = strings.Join(*t.value, ", ")
	}
	_, err := fmt.Fprintf(w, "%s: %s\n", t.title, tags)
	return err
}

// Skip returns false.
func (t *TagField) Skip() bool { return false }

// Zoom returns false.
func (t *TagField) Zoom() bool { return false }

// KeyBinds returns the keybindings.
func (t *TagField) KeyBinds() []key.Binding {
	return []key.Binding{t.keymap.Next, t.keymap.Prev, t.keymap.Submit}
}

// WithTheme sets the theme.
func (t *TagField) WithTheme(theme huh.Theme) huh.Field {
	if t.theme != nil {
		return t
	}
	t.theme = theme
	return t
}

// WithKeyMap sets the keymap.
func (t *TagField) WithKeyMap(k *huh.KeyMap) huh.Field {
	t.keymap = k.Select
	return t
}

// WithWidth sets the width.
func (t *TagField) WithWidth(width int) huh.Field {
	t.width = width
	return t
}

// WithHeight sets the height.
func (t *TagField) WithHeight(height int) huh.Field {
	t.height = height
	return t
}

// WithPosition sets the field position.
func (t *TagField) WithPosition(p huh.FieldPosition) huh.Field {
	t.keymap.Prev.SetEnabled(!p.IsFirst())
	t.keymap.Next.SetEnabled(!p.IsLast())
	t.keymap.Submit.SetEnabled(p.IsLast())
	return t
}

// GetKey returns the field key.
func (t *TagField) GetKey() string { return t.key }

// GetValue returns the current tags.
func (t *TagField) GetValue() any {
	if t.value == nil {
		return []string{}
	}
	return *t.value
}
