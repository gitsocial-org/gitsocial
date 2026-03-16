// list_picker.go - List selection view for choosing or creating repository lists
package tuisocial

import (
	"fmt"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/extensions/social"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9-]+`)
var validIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ListPickerView displays and manages user's lists.
type ListPickerView struct {
	lists          []social.List
	cursor         int
	lastClickIdx   int
	loading        bool
	inputMode      bool
	input          textinput.Model
	idInput        textinput.Model
	idInputFocused bool
	idError        string
	followRepoURL  string
	workdir        string
	confirm        tuicore.ConfirmDialog
	zonePrefix     string
}

// Bindings returns keybindings for the list picker view.
func (v *ListPickerView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "n", Label: "new list", Contexts: []tuicore.Context{tuicore.ListPicker},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.CreateList()
			}},
		{Key: "D", Label: "delete list", Contexts: []tuicore.Context{tuicore.ListPicker},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.DeleteList()
			}},
		{Key: "m", Label: "my repo", Contexts: []tuicore.Context{tuicore.ListPicker},
			Handler: func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
				if ctx.Panel == nil {
					return false, nil
				}
				return true, ctx.Panel.OpenMyRepo()
			}},
		{Key: "a", Label: "create", Contexts: []tuicore.Context{tuicore.ListPicker}, Handler: noop},
		{Key: "enter", Label: "open/add", Contexts: []tuicore.Context{tuicore.ListPicker}, Handler: noop},
		{Key: "j", Label: "down", Contexts: []tuicore.Context{tuicore.ListPicker}, Handler: noop},
		{Key: "k", Label: "up", Contexts: []tuicore.Context{tuicore.ListPicker}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.ListPicker}, Handler: push},
	}
}

// NewListPickerView creates a new list picker view.
func NewListPickerView(workdir string) *ListPickerView {
	input := textinput.New()
	input.Placeholder = "List name..."
	input.CharLimit = 256
	input.Prompt = "name: "
	tuicore.StyleTextInput(&input, tuicore.Dim, lipgloss.NewStyle(), tuicore.Dim)

	idInput := textinput.New()
	idInput.Placeholder = ""
	idInput.CharLimit = 256
	idInput.Prompt = "  id: " // aligned with "name: "
	tuicore.StyleTextInput(&idInput, tuicore.Dim, lipgloss.NewStyle(), tuicore.Dim)

	return &ListPickerView{
		input:        input,
		idInput:      idInput,
		workdir:      workdir,
		lastClickIdx: -1,
		zonePrefix:   zone.NewPrefix(),
	}
}

// nameToID converts a list name to a valid ID (lowercase, hyphenated).
func nameToID(name string) string {
	id := strings.ToLower(name)
	id = strings.ReplaceAll(id, " ", "-")
	id = nonAlphanumeric.ReplaceAllString(id, "")
	id = strings.Trim(id, "-")
	// Collapse multiple hyphens
	for strings.Contains(id, "--") {
		id = strings.ReplaceAll(id, "--", "-")
	}
	return id
}

// isValidID checks if an ID matches the valid format (lowercase alphanumeric with hyphens).
func isValidID(id string) bool {
	if id == "" {
		return false
	}
	return validIDPattern.MatchString(id)
}

// SetSize sets the view dimensions.
func (v *ListPickerView) SetSize(width, height int) {
	// List picker uses text rendering, not CardList
}

// Activate loads lists when the view becomes active.
func (v *ListPickerView) Activate(state *tuicore.State) tea.Cmd {
	v.loading = true
	v.cursor = 0
	v.inputMode = false
	v.idInputFocused = false
	v.confirm.Reset()
	v.input.SetValue("")
	v.idInput.SetValue("")
	loc := state.Router.Location()
	v.followRepoURL = loc.Param("repoURL")
	return v.loadLists()
}

// loadLists fetches the user's lists.
func (v *ListPickerView) loadLists() tea.Cmd {
	workdir := v.workdir
	return func() tea.Msg {
		result := social.GetLists(workdir)
		if !result.Success {
			return ListsLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return ListsLoadedMsg{Lists: result.Data}
	}
}

// Update handles messages and returns commands.
func (v *ListPickerView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if v.inputMode || v.confirm.IsActive() || v.loading {
			return nil
		}
		return v.handleMouse(msg)
	case tea.KeyPressMsg:
		return v.handleKey(msg, state)
	case ListsLoadedMsg:
		v.handleLoaded(msg)
	case ListCreatedMsg:
		v.handleCreated(msg)
	case ListDeletedMsg:
		v.handleDeleted(msg)
	case RepoAddedMsg:
		return v.handleRepoAdded(msg, state)
	default:
		// Forward other messages (like blink) to focused input
		if v.inputMode {
			var cmd tea.Cmd
			if v.idInputFocused {
				v.idInput, cmd = v.idInput.Update(msg)
			} else {
				v.input, cmd = v.input.Update(msg)
			}
			return cmd
		}
	}
	return nil
}

// handleKey processes keyboard input.
func (v *ListPickerView) handleKey(msg tea.KeyPressMsg, _ *tuicore.State) tea.Cmd {
	key := msg.String()
	if handled, cmd := v.confirm.HandleKey(key); handled {
		return cmd
	}
	if v.inputMode {
		switch key {
		case "enter":
			name := v.input.Value()
			customID := strings.TrimSpace(v.idInput.Value())
			var id string
			if customID != "" {
				if !isValidID(customID) {
					v.idError = "invalid id: use lowercase letters, numbers, hyphens"
					return nil
				}
				id = customID
			} else {
				id = nameToID(name)
			}
			if name != "" && id != "" {
				v.inputMode = false
				v.idError = ""
				v.input.Blur()
				v.idInput.Blur()
				return v.createList(id, name)
			}
		case "esc":
			v.inputMode = false
			v.idError = ""
			v.input.Blur()
			v.idInput.Blur()
		case "tab":
			v.idError = ""
			if v.idInputFocused {
				v.idInputFocused = false
				v.idInput.Blur()
				return v.input.Focus()
			}
			v.idInputFocused = true
			v.input.Blur()
			// Pre-fill with auto-generated ID if empty
			if strings.TrimSpace(v.idInput.Value()) == "" {
				if name := v.input.Value(); name != "" {
					autoID := nameToID(name)
					if autoID != "" {
						v.idInput.SetValue(autoID)
						v.idInput.SetCursor(len(autoID))
					}
				}
			}
			return v.idInput.Focus()
		default:
			var cmd tea.Cmd
			if v.idInputFocused {
				v.idInput, cmd = v.idInput.Update(msg)
			} else {
				v.input, cmd = v.input.Update(msg)
			}
			return cmd
		}
		return nil
	}

	switch key {
	case "j", "down":
		if v.cursor < len(v.lists)-1 {
			v.cursor++
		}
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "enter":
		if len(v.lists) > 0 && v.cursor < len(v.lists) {
			list := v.lists[v.cursor]
			if v.followRepoURL != "" {
				// Add repo to list mode
				listName := list.Name
				if listName == "" {
					listName = list.ID
				}
				return v.addRepoToList(list.ID, listName)
			}
			return func() tea.Msg {
				return tuicore.NavigateMsg{
					Location: tuicore.LocList(list.ID),
					Action:   tuicore.NavPush,
				}
			}
		}
	case "a":
		v.inputMode = true
		v.idInputFocused = false
		v.idError = ""
		v.input.Blur()
		v.input.Reset()
		v.input.Placeholder = ""
		v.idInput.Blur()
		v.idInput.Reset()
		v.idInput.Placeholder = ""
		return v.input.Focus()
	case "D":
		if len(v.lists) > 0 && v.cursor < len(v.lists) {
			listID := v.lists[v.cursor].ID
			v.confirm.Show("Delete list '"+listID+"'?", true, func() tea.Cmd { return v.deleteList(listID) })
		}
	case "esc":
		v.followRepoURL = ""
		return func() tea.Msg {
			return tuicore.NavigateMsg{Action: tuicore.NavBack}
		}
	}
	return nil
}

// handleMouse processes mouse input.
func (v *ListPickerView) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg:
		idx := tuicore.ZoneClicked(msg, len(v.lists), v.zonePrefix)
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
			if v.cursor < len(v.lists)-1 {
				v.cursor++
			}
		}
	}
	return nil
}

// activateSelected navigates to the selected list or adds repo to it.
func (v *ListPickerView) activateSelected() tea.Cmd {
	if len(v.lists) == 0 || v.cursor >= len(v.lists) {
		return nil
	}
	list := v.lists[v.cursor]
	if v.followRepoURL != "" {
		listName := list.Name
		if listName == "" {
			listName = list.ID
		}
		return v.addRepoToList(list.ID, listName)
	}
	return func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocList(list.ID),
			Action:   tuicore.NavPush,
		}
	}
}

// handleLoaded processes the loaded lists data.
func (v *ListPickerView) handleLoaded(msg ListsLoadedMsg) {
	v.loading = false
	if msg.Err != nil {
		return
	}
	v.lists = msg.Lists
}

// handleCreated adds the newly created list to the view.
func (v *ListPickerView) handleCreated(msg ListCreatedMsg) {
	if msg.Err != nil {
		return
	}
	v.lists = append(v.lists, msg.List)
}

// handleDeleted removes the deleted list from the view.
func (v *ListPickerView) handleDeleted(msg ListDeletedMsg) {
	if msg.Err != nil {
		return
	}
	for i, list := range v.lists {
		if list.ID == msg.ListID {
			v.lists = append(v.lists[:i], v.lists[i+1:]...)
			if v.cursor >= len(v.lists) && v.cursor > 0 {
				v.cursor--
			}
			break
		}
	}
}

// handleRepoAdded clears the follow mode after adding a repo.
func (v *ListPickerView) handleRepoAdded(_ RepoAddedMsg, _ *tuicore.State) tea.Cmd {
	v.followRepoURL = ""
	// Navigation and messages handled by app.go
	return nil
}

// createList creates a new list with the given ID and name.
func (v *ListPickerView) createList(id, name string) tea.Cmd {
	workdir := v.workdir
	return func() tea.Msg {
		result := social.CreateList(workdir, id, name)
		if !result.Success {
			return ListCreatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return ListCreatedMsg{List: result.Data}
	}
}

// deleteList deletes the list with the given ID.
func (v *ListPickerView) deleteList(id string) tea.Cmd {
	workdir := v.workdir
	return func() tea.Msg {
		result := social.DeleteList(workdir, id)
		if !result.Success {
			return ListDeletedMsg{ListID: id, Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return ListDeletedMsg{ListID: id}
	}
}

// addRepoToList adds the follow repo URL to the specified list.
func (v *ListPickerView) addRepoToList(listID, listName string) tea.Cmd {
	workdir := v.workdir
	repoURL := v.followRepoURL
	return func() tea.Msg {
		result := social.AddRepositoryToList(workdir, listID, repoURL, "", false)
		if !result.Success {
			return RepoAddedMsg{ListID: listID, ListName: listName, RepoURL: repoURL, Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return RepoAddedMsg{ListID: listID, ListName: listName, RepoURL: result.Data}
	}
}

// Render renders the list picker view to a string.
func (v *ListPickerView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)

	var b strings.Builder
	// Show input form at top
	if v.inputMode {
		b.WriteString(v.input.View())
		b.WriteString("\n")
		b.WriteString(v.idInput.View())
		if v.idError != "" {
			errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.StatusError))
			b.WriteString("\n")
			b.WriteString(errorStyle.Render("        " + v.idError))
		}
	} else {
		keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.BorderFocused)).Bold(true)
		b.WriteString(keyStyle.Render("n") + tuicore.Dim.Render(":new list"))
	}
	b.WriteString("\n\n")

	if v.loading {
		b.WriteString(tuicore.Dim.Render("Loading lists..."))
	} else if len(v.lists) == 0 {
		b.WriteString(tuicore.Dim.Render("No lists"))
	} else {
		for i, list := range v.lists {
			prefix := "  "
			if i == v.cursor {
				prefix = tuicore.Title.Render("▸ ")
			}
			var line strings.Builder
			line.WriteString(prefix)
			name := list.Name
			if name == "" {
				name = list.ID
			}
			if i == v.cursor {
				line.WriteString(tuicore.Title.Render(name))
			} else {
				line.WriteString(name)
			}
			line.WriteString(tuicore.Dim.Render(" (" + list.ID + ")"))
			b.WriteString(tuicore.MarkZone(tuicore.ZoneID(v.zonePrefix, i), line.String()))
			b.WriteString("\n")
		}
	}

	if v.followRepoURL != "" {
		b.WriteString("\n\n")
		b.WriteString(tuicore.Dim.Render("Adding: " + v.followRepoURL))
	}

	var footer string
	if v.confirm.IsActive() {
		footer = v.confirm.Render()
	} else {
		footer = tuicore.RenderFooter(state.Registry, tuicore.ListPicker, wrapper.ContentWidth(), nil)
	}
	return wrapper.Render(b.String(), footer)
}

// IsInputActive returns true when the input field or confirmation is active.
func (v *ListPickerView) IsInputActive() bool {
	return v.inputMode || v.confirm.IsActive()
}

// GetSelectedList returns the currently selected list.
func (v *ListPickerView) GetSelectedList() *tuicore.SelectedList {
	if len(v.lists) == 0 || v.cursor >= len(v.lists) {
		return nil
	}
	list := v.lists[v.cursor]
	return &tuicore.SelectedList{
		ID:   list.ID,
		Name: list.Name,
	}
}

// CreateList starts the list creation input mode.
func (v *ListPickerView) CreateList() tea.Cmd {
	v.inputMode = true
	v.idInputFocused = false
	v.idError = ""
	v.input.Blur()
	v.input.Reset()
	v.input.Placeholder = ""
	v.idInput.Blur()
	v.idInput.Reset()
	v.idInput.Placeholder = ""
	return v.input.Focus()
}

// DeleteList shows confirmation before deleting the currently selected list.
func (v *ListPickerView) DeleteList() tea.Cmd {
	if len(v.lists) == 0 || v.cursor >= len(v.lists) {
		return nil
	}
	listID := v.lists[v.cursor].ID
	v.confirm.Show("Delete list '"+listID+"'?", true, func() tea.Cmd { return v.deleteList(listID) })
	return nil
}

// SetFollowRepoURL sets the repo URL to add to a list.
func (v *ListPickerView) SetFollowRepoURL(url string) {
	v.followRepoURL = url
}

// Lists returns the loaded lists.
func (v *ListPickerView) Lists() []social.List {
	return v.lists
}
