// view_inherits.go - Manage refs/gitmsg/memo/inherits/* (binding memo sources)
package tuimemo

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/library/extensions/memo"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// InheritsView lets the user view, add, and remove inherited (binding) memo
// source URLs for the workspace.
type InheritsView struct {
	workdir   string
	urls      []string
	cursor    int
	width     int
	height    int
	addForm   *huh.Form
	addURL    string
	inputMode bool
	confirm   tuicore.ConfirmDialog
	loaded    bool
}

// NewInheritsView creates a new inherits-management view.
func NewInheritsView(workdir string) *InheritsView {
	return &InheritsView{workdir: workdir}
}

// Title returns the panel header for the view.
func (v *InheritsView) Title() string {
	return fmt.Sprintf("☞  Inherited Sources · %d", len(v.urls))
}

// HeaderInfo returns the position indicator for the title bar.
func (v *InheritsView) HeaderInfo() (int, string) {
	if len(v.urls) == 0 {
		return 0, ""
	}
	return v.cursor + 1, fmt.Sprintf("%d", len(v.urls))
}

// SetSize stores panel dimensions.
func (v *InheritsView) SetSize(w, h int) { v.width, v.height = w, h }

// Activate (re)loads the URL list. Cursor is preserved across navigation
// (clamped to new bounds when the list shrinks).
func (v *InheritsView) Activate(state *tuicore.State) tea.Cmd {
	prev := v.cursor
	v.urls = memo.ListInherits(v.workdir)
	v.cursor = prev
	if v.cursor >= len(v.urls) {
		v.cursor = len(v.urls) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
	v.loaded = true
	v.inputMode = false
	v.addForm = nil
	v.addURL = ""
	return nil
}

// IsInputActive reports whether the URL input is taking text input.
func (v *InheritsView) IsInputActive() bool { return v.inputMode }

// Bindings returns the view's keybindings.
func (v *InheritsView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []tuicore.Binding{
		{Key: "n", Label: "add", Contexts: []tuicore.Context{tuicore.MemoInherits}, Handler: noop},
		{Key: "d", Label: "remove", Contexts: []tuicore.Context{tuicore.MemoInherits}, Handler: noop},
		{Key: "j", Label: "down", Contexts: []tuicore.Context{tuicore.MemoInherits}, Handler: noop},
		{Key: "k", Label: "up", Contexts: []tuicore.Context{tuicore.MemoInherits}, Handler: noop},
	}
}

// Update handles input mode, confirm dialog, and key dispatch.
func (v *InheritsView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if v.inputMode {
		return v.updateInput(msg)
	}
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		if handled, cmd := v.confirm.HandleKey(m.String()); handled {
			return cmd
		}
		switch m.String() {
		case "n":
			return v.startAdd()
		case "d", "X":
			if v.cursor < 0 || v.cursor >= len(v.urls) {
				return nil
			}
			url := v.urls[v.cursor]
			v.confirm.Show("Remove "+url+"?", true, func() tea.Cmd { return v.doRemove(url) })
			return nil
		case "j", "down":
			if v.cursor < len(v.urls)-1 {
				v.cursor++
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
		case "g", "home":
			v.cursor = 0
		case "G", "end":
			v.cursor = len(v.urls) - 1
		}
	}
	return nil
}

// startAdd opens the inline huh form for the new inherit URL.
func (v *InheritsView) startAdd() tea.Cmd {
	v.inputMode = true
	v.addURL = ""
	urlField := huh.NewInput().
		Key("url").
		Title(tuicore.PadLabel(tuicore.RequiredLabel("URL"))).
		Placeholder("https://example.com/owner/repo").
		CharLimit(200).
		Value(&v.addURL).
		Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("URL is required")
			}
			return nil
		})
	v.addForm = huh.NewForm(huh.NewGroup(urlField, tuicore.NewSubmitField())).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(tuicore.FormKeyMap())
	return v.addForm.Init()
}

func (v *InheritsView) updateInput(msg tea.Msg) tea.Cmd {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok && keyMsg.String() == "esc" {
		v.inputMode = false
		v.addForm = nil
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
		url := strings.TrimSpace(v.addURL)
		v.inputMode = false
		v.addForm = nil
		if url == "" {
			return nil
		}
		return v.doAdd(url)
	}
	return cmd
}

func (v *InheritsView) doAdd(url string) tea.Cmd {
	res := memo.AddInherit(v.workdir, url)
	if !res.Success {
		return func() tea.Msg { return inheritsErrMsg{err: fmt.Errorf("%s", res.Error.Message)} }
	}
	v.urls = memo.ListInherits(v.workdir)
	for i, u := range v.urls {
		if u == url {
			v.cursor = i
			break
		}
	}
	return nil
}

func (v *InheritsView) doRemove(url string) tea.Cmd {
	return func() tea.Msg {
		res := memo.RemoveInherit(v.workdir, url)
		if !res.Success {
			return inheritsErrMsg{err: fmt.Errorf("%s", res.Error.Message)}
		}
		return inheritsRemovedMsg{url: url}
	}
}

// Render renders the URL list, the input prompt, and the footer.
func (v *InheritsView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	v.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())

	var lines []string
	if !v.loaded {
		lines = append(lines, "Loading...")
	} else if len(v.urls) == 0 {
		lines = append(lines, tuicore.Dim.Render("(no inherited sources — press 'n' to add)"))
	} else {
		for i, u := range v.urls {
			prefix := "  "
			styled := u
			if i == v.cursor {
				prefix = tuicore.Title.Render("▏")
				styled = tuicore.Bold.Render(u)
			}
			lines = append(lines, prefix+" "+styled)
		}
	}

	body := strings.Join(lines, "\n")

	var footer string
	switch {
	case v.confirm.IsActive():
		footer = v.confirm.Render()
	case v.inputMode && v.addForm != nil:
		footer = tuicore.FormFooter(false, v.addForm.Errors())
		body = body + "\n\n" + v.addForm.View()
	default:
		footer = tuicore.RenderFooter(state.Registry, tuicore.MemoInherits, nil)
	}
	return wrapper.Render(body, footer)
}

type inheritsErrMsg struct{ err error }
type inheritsRemovedMsg struct{ url string }
