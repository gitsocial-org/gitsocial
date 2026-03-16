// view_forks.go - Fork management view for registering/removing fork repositories
package tuireview

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// ForksView displays registered forks with add/remove support.
type ForksView struct {
	workdir      string
	forks        []string
	cursor       int
	lastClickIdx int
	inputMode    bool
	input        textinput.Model
	confirm      tuicore.ConfirmDialog
	zonePrefix   string
}

// NewForksView creates a new forks management view.
func NewForksView(workdir string) *ForksView {
	input := textinput.New()
	input.Placeholder = "Fork repository URL..."
	input.CharLimit = 512
	input.Prompt = "+ "
	tuicore.StyleTextInput(&input, tuicore.Title, lipgloss.NewStyle(), tuicore.Dim)
	return &ForksView{
		workdir:      workdir,
		input:        input,
		lastClickIdx: -1,
		zonePrefix:   zone.NewPrefix(),
	}
}

// SetSize sets the view dimensions.
func (v *ForksView) SetSize(width, height int) {}

// Activate loads forks when the view becomes active.
func (v *ForksView) Activate(state *tuicore.State) tea.Cmd {
	v.inputMode = false
	v.confirm.Reset()
	v.input.SetValue("")
	v.cursor = 0
	v.forks = review.GetForks(v.workdir)
	sort.Strings(v.forks)
	return nil
}

// Deactivate is called when the view is hidden.
func (v *ForksView) Deactivate() {}

// Update handles messages and returns commands.
func (v *ForksView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if v.inputMode || v.confirm.IsActive() {
			return nil
		}
		return v.handleMouse(msg)
	case tea.KeyPressMsg:
		return v.handleKey(msg)
	case ForkAddedMsg:
		if msg.Err != nil {
			return nil
		}
		v.forks = append(v.forks, msg.ForkURL)
		sort.Strings(v.forks)
		for i, f := range v.forks {
			if f == msg.ForkURL {
				v.cursor = i
				break
			}
		}
	case ForkRemovedMsg:
		if msg.Err != nil {
			return nil
		}
		for i, f := range v.forks {
			if f == msg.ForkURL {
				v.forks = append(v.forks[:i], v.forks[i+1:]...)
				if v.cursor >= len(v.forks) && v.cursor > 0 {
					v.cursor--
				}
				break
			}
		}
	}
	return nil
}

// handleMouse processes mouse input.
func (v *ForksView) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg:
		idx := tuicore.ZoneClicked(msg, len(v.forks), v.zonePrefix)
		if idx >= 0 {
			if idx == v.lastClickIdx && idx == v.cursor {
				v.lastClickIdx = -1
				return v.activateSelected()
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
			if v.cursor < len(v.forks)-1 {
				v.cursor++
			}
		}
	}
	return nil
}

// activateSelected navigates to the selected fork repository.
func (v *ForksView) activateSelected() tea.Cmd {
	if len(v.forks) == 0 || v.cursor >= len(v.forks) {
		return nil
	}
	repoURL := v.forks[v.cursor]
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocRepository(repoURL, ""),
			Action:   tuicore.NavPush,
		}
	}
}

// handleKey processes keyboard input.
func (v *ForksView) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if handled, cmd := v.confirm.HandleKey(key); handled {
		return cmd
	}
	if v.inputMode {
		switch key {
		case "enter":
			forkURL := strings.TrimSpace(v.input.Value())
			if forkURL != "" {
				v.inputMode = false
				v.input.Blur()
				return v.addFork(forkURL)
			}
		case "esc":
			v.inputMode = false
			v.input.Blur()
		default:
			var cmd tea.Cmd
			v.input, cmd = v.input.Update(msg)
			return cmd
		}
		return nil
	}
	switch key {
	case "j", "down":
		if v.cursor < len(v.forks)-1 {
			v.cursor++
		}
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "enter":
		return v.activateSelected()
	case "a":
		v.inputMode = true
		v.input.Blur()
		v.input.Reset()
		v.input.Placeholder = ""
		return v.input.Focus()
	case "x":
		if len(v.forks) > 0 && v.cursor < len(v.forks) {
			forkURL := v.forks[v.cursor]
			name := protocol.GetDisplayName(forkURL)
			v.confirm.Show("Remove fork '"+name+"'?", true, func() tea.Cmd { return v.removeFork(forkURL) })
		}
	}
	return nil
}

// addFork registers a fork URL.
func (v *ForksView) addFork(forkURL string) tea.Cmd {
	workdir := v.workdir
	return func() tea.Msg {
		normalized := protocol.NormalizeURL(forkURL)
		err := review.AddFork(workdir, forkURL)
		return ForkAddedMsg{ForkURL: normalized, Err: err}
	}
}

// removeFork removes a fork URL.
func (v *ForksView) removeFork(forkURL string) tea.Cmd {
	workdir := v.workdir
	return func() tea.Msg {
		err := review.RemoveFork(workdir, forkURL)
		return ForkRemovedMsg{ForkURL: forkURL, Err: err}
	}
}

// Render renders the forks view.
func (v *ForksView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)

	var b strings.Builder
	if v.inputMode {
		b.WriteString(v.input.View())
		b.WriteString("\n\n")
	}

	if len(v.forks) == 0 {
		b.WriteString(tuicore.Dim.Render("No forks registered"))
	} else {
		for i, forkURL := range v.forks {
			selected := i == v.cursor
			prefix := "  "
			if selected {
				prefix = tuicore.Title.Render("▸ ")
			}
			name := protocol.GetDisplayName(forkURL)
			var line strings.Builder
			line.WriteString(prefix)
			strippedURL := strings.TrimPrefix(strings.TrimPrefix(forkURL, "https://"), "http://")
			if selected {
				line.WriteString(tuicore.TitleSelected.Render(name))
				line.WriteString(tuicore.DimSelected.Render(" · "))
				line.WriteString(tuicore.Hyperlink(forkURL, strippedURL))
			} else {
				line.WriteString(name)
				line.WriteString(tuicore.Dim.Render(" · "))
				line.WriteString(tuicore.Hyperlink(forkURL, strippedURL))
			}
			b.WriteString(tuicore.MarkZone(tuicore.ZoneID(v.zonePrefix, i), line.String()))
			b.WriteString("\n")
		}
	}

	var footer string
	if v.confirm.IsActive() {
		footer = v.confirm.Render()
	} else {
		footer = tuicore.RenderFooter(state.Registry, tuicore.ReviewForks, wrapper.ContentWidth(), nil)
	}
	return wrapper.Render(b.String(), footer)
}

// IsInputActive returns true when input or confirmation is active.
func (v *ForksView) IsInputActive() bool {
	return v.inputMode || v.confirm.IsActive()
}

// Title returns the view title.
func (v *ForksView) Title() string {
	return fmt.Sprintf("⑂  Forks (%d)", len(v.forks))
}

// HeaderInfo returns position and total for the header.
func (v *ForksView) HeaderInfo() (position, total int) {
	if len(v.forks) == 0 {
		return 0, 0
	}
	return v.cursor + 1, len(v.forks)
}

// Bindings returns keybindings for the forks view.
func (v *ForksView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []tuicore.Binding{
		{Key: "a", Label: "add fork", Contexts: []tuicore.Context{tuicore.ReviewForks}, Handler: noop},
		{Key: "x", Label: "remove fork", Contexts: []tuicore.Context{tuicore.ReviewForks}, Handler: noop},
		{Key: "enter", Label: "open repo", Contexts: []tuicore.Context{tuicore.ReviewForks}, Handler: noop},
	}
}

// ForkAddedMsg is sent when a fork is added.
type ForkAddedMsg struct {
	ForkURL string
	Err     error
}

// ForkRemovedMsg is sent when a fork is removed.
type ForkRemovedMsg struct {
	ForkURL string
	Err     error
}
