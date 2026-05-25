// list_picker.go - List selection view for choosing or creating repository lists
package tuisocial

import (
	"fmt"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9-]+`)
var validIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// listCreateData holds the in-progress create-list form values.
type listCreateData struct {
	Name string
	ID   string
}

// ListPickerView displays and manages user's lists.
type ListPickerView struct {
	lists         []social.List
	cursor        int
	lastClickIdx  int
	loading       bool
	createForm    *huh.Form
	createData    listCreateData
	createMode    bool
	followRepoURL string
	workdir       string
	confirm       tuicore.ConfirmDialog
	zonePrefix    string
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
		{Key: "j", Label: "down", Contexts: []tuicore.Context{tuicore.ListPicker}, Handler: noop},
		{Key: "k", Label: "up", Contexts: []tuicore.Context{tuicore.ListPicker}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.ListPicker}, Handler: push},
	}
}

// NewListPickerView creates a new list picker view.
func NewListPickerView(workdir string) *ListPickerView {
	return &ListPickerView{
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
	v.createMode = false
	v.createForm = nil
	v.createData = listCreateData{}
	v.confirm.Reset()
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
		if v.createMode || v.confirm.IsActive() || v.loading {
			return nil
		}
		return v.handleMouse(msg)
	case tea.KeyPressMsg:
		return v.handleKey(msg, state)
	case ListsLoadedMsg:
		v.handleLoaded(msg)
		return nil
	case ListCreatedMsg:
		v.handleCreated(msg)
		return nil
	case ListDeletedMsg:
		v.handleDeleted(msg)
		return nil
	case RepoAddedMsg:
		return v.handleRepoAdded(msg, state)
	}
	if v.createMode && v.createForm != nil {
		form, cmd := v.createForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			v.createForm = f
		}
		if v.createForm.State == huh.StateCompleted {
			return v.submitCreate()
		}
		return cmd
	}
	return nil
}

// handleKey processes keyboard input.
func (v *ListPickerView) handleKey(msg tea.KeyPressMsg, _ *tuicore.State) tea.Cmd {
	key := msg.String()
	if handled, cmd := v.confirm.HandleKey(key); handled {
		return cmd
	}
	if v.createMode {
		if key == "esc" {
			v.createMode = false
			v.createForm = nil
			return nil
		}
		if v.createForm == nil {
			return nil
		}
		form, cmd := v.createForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			v.createForm = f
		}
		if v.createForm.State == huh.StateCompleted {
			return v.submitCreate()
		}
		return cmd
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
	if v.createMode && v.createForm != nil {
		b.WriteString(v.createForm.View())
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
	switch {
	case v.confirm.IsActive():
		footer = v.confirm.Render()
	case v.createMode && v.createForm != nil:
		footer = tuicore.FormFooter(false, v.createForm.Errors())
	default:
		footer = tuicore.RenderFooter(state.Registry, tuicore.ListPicker, nil)
	}
	return wrapper.Render(b.String(), footer)
}

// IsInputActive returns true when the create form or confirmation is active.
func (v *ListPickerView) IsInputActive() bool {
	return v.createMode || v.confirm.IsActive()
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
	v.createMode = true
	v.createData = listCreateData{}

	pad := tuicore.PadLabel
	nameField := huh.NewInput().
		Key("name").
		Title(pad(tuicore.RequiredLabel("Name"))).
		Placeholder("List name...").
		CharLimit(256).
		Value(&v.createData.Name).
		Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("name is required")
			}
			return nil
		})
	idField := huh.NewInput().
		Key("id").
		Title(pad("ID")).
		Placeholder("(auto from name)").
		CharLimit(256).
		Value(&v.createData.ID).
		Validate(func(s string) error {
			id := strings.TrimSpace(s)
			if id == "" {
				// Auto-derive from name when blank.
				v.createData.ID = nameToID(v.createData.Name)
				return nil
			}
			if !isValidID(id) {
				return fmt.Errorf("invalid id: use lowercase letters, numbers, hyphens")
			}
			return nil
		})

	v.createForm = huh.NewForm(huh.NewGroup(
		nameField,
		idField,
		tuicore.NewSubmitField(),
	)).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(tuicore.FormKeyMap())
	return v.createForm.Init()
}

// submitCreate finalizes the inline create form and dispatches the create command.
func (v *ListPickerView) submitCreate() tea.Cmd {
	name := strings.TrimSpace(v.createData.Name)
	id := strings.TrimSpace(v.createData.ID)
	if id == "" {
		id = nameToID(name)
	}
	v.createMode = false
	v.createForm = nil
	if name == "" || id == "" {
		return nil
	}
	return v.createList(id, name)
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
