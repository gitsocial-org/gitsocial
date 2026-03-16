// sprint_form.go - Sprint creation form using Huh
package tuipm

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// SprintFormData holds the form field values.
type SprintFormData struct {
	Title string
	Body  string
	State string
	Start string
	End   string
}

// SprintForm wraps a Huh form for sprint creation/editing.
type SprintForm struct {
	workdir   string
	sprintID  string // Non-empty for edit mode
	form      *huh.Form
	bodyField *huh.Text
	data      SprintFormData
	width     int
	height    int
	submitted bool
	canceled  bool
}

// NewSprintForm creates a new sprint form.
func NewSprintForm(workdir string) *SprintForm {
	f := &SprintForm{workdir: workdir}
	f.buildForm()
	return f
}

// NewSprintEditForm creates a form pre-filled with sprint data.
func NewSprintEditForm(workdir string, sprint pm.Sprint) *SprintForm {
	f := &SprintForm{
		workdir:  workdir,
		sprintID: sprint.ID,
	}
	f.data.Title = sprint.Title
	f.data.Body = sprint.Body
	f.data.State = string(sprint.State)
	if !sprint.Start.IsZero() {
		f.data.Start = sprint.Start.Format("2006-01-02")
	}
	if !sprint.End.IsZero() {
		f.data.End = sprint.End.Format("2006-01-02")
	}
	f.buildForm()
	return f
}

// IsEditMode returns true if this is an edit form.
func (f *SprintForm) IsEditMode() bool {
	return f.sprintID != ""
}

// buildForm constructs the Huh form.
func (f *SprintForm) buildForm() {
	pad := tuicore.PadLabel
	fields := make([]huh.Field, 0, 6)
	fields = append(fields,
		huh.NewInput().
			Key("title").
			Title(pad(tuicore.RequiredLabel("Title"))).
			Placeholder("Sprint title...").
			Value(&f.data.Title).
			Inline(true).
			Validate(func(s string) error {
				if s == "" {
					return fmt.Errorf("title is required")
				}
				return nil
			}),
		tuicore.NewCycleField().
			Key("state").
			Title(pad("State")).
			Options(
				tuicore.CycleOption{Label: "(none)", Value: ""},
				tuicore.CycleOption{Label: "planned", Value: "planned"},
				tuicore.CycleOption{Label: "active", Value: "active"},
				tuicore.CycleOption{Label: "completed", Value: "completed"},
				tuicore.CycleOption{Label: "canceled", Value: "canceled"},
			).
			Value(&f.data.State),
		huh.NewInput().
			Key("start").
			Title(pad("Start Date")).
			Placeholder("YYYY-MM-DD").
			Value(&f.data.Start).
			Inline(true),
		huh.NewInput().
			Key("end").
			Title(pad("End Date")).
			Placeholder("YYYY-MM-DD").
			Value(&f.data.End).
			Inline(true),
	)
	f.bodyField = huh.NewText().
		Key("body").
		Title("Description").
		Placeholder("Optional description...").
		Value(&f.data.Body).
		CharLimit(2000).
		Lines(20)
	fields = append(fields, f.bodyField, tuicore.NewSubmitField())
	f.form = huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(formKeyMap())
}

// SetSize sets the form dimensions.
func (f *SprintForm) SetSize(w, h int) {
	f.width = w
	f.height = h
	if f.form != nil {
		f.form.WithWidth(w).WithHeight(h - 2)
		if f.bodyField != nil {
			f.bodyField.WithHeight(max(5, h-7))
		}
	}
}

// Init initializes the form.
func (f *SprintForm) Init() tea.Cmd {
	return f.form.Init()
}

// Update handles form messages.
func (f *SprintForm) Update(msg tea.Msg) tea.Cmd {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		if keyMsg.String() == "esc" {
			f.canceled = true
			return nil
		}
	}
	form, cmd := f.form.Update(msg)
	if m, ok := form.(*huh.Form); ok {
		f.form = m
	}
	if f.form.State == huh.StateCompleted {
		f.submitted = true
	}
	return cmd
}

// View renders the form.
func (f *SprintForm) View() string {
	return f.form.View()
}

// Errors returns the form's current validation errors.
func (f *SprintForm) Errors() []error { return f.form.Errors() }

// IsSubmitted returns true if form was submitted.
func (f *SprintForm) IsSubmitted() bool {
	return f.submitted
}

// IsCancelled returns true if form was canceled.
func (f *SprintForm) IsCancelled() bool {
	return f.canceled
}

// SprintCreatedMsg signals that a sprint was created.
type SprintCreatedMsg struct {
	Sprint pm.Sprint
	Err    error
}

// CreateSprintFromForm creates a sprint from form data.
func (f *SprintForm) CreateSprintFromForm() tea.Cmd {
	data := f.data
	workdir := f.workdir
	return func() tea.Msg {
		opts := pm.CreateSprintOptions{
			State: pm.SprintState(data.State),
		}
		if data.Start != "" {
			if t, err := time.Parse("2006-01-02", data.Start); err == nil {
				opts.Start = t
			}
		}
		if data.End != "" {
			if t, err := time.Parse("2006-01-02", data.End); err == nil {
				opts.End = t
			}
		}
		result := pm.CreateSprint(workdir, data.Title, data.Body, opts)
		if !result.Success {
			return SprintCreatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return SprintCreatedMsg{Sprint: result.Data}
	}
}

// SprintFormView wraps the form for integration with the TUI host.
type SprintFormView struct {
	form       *SprintForm
	width      int
	height     int
	submitting bool
}

// NewSprintFormView creates a new sprint form view.
func NewSprintFormView(workdir string) *SprintFormView {
	return &SprintFormView{
		form: NewSprintForm(workdir),
	}
}

// SetSize sets the view dimensions.
func (v *SprintFormView) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.form.SetSize(w, h)
}

// Activate initializes the form view.
func (v *SprintFormView) Activate(state *tuicore.State) tea.Cmd {
	v.form = NewSprintForm(state.Workdir)
	return v.form.Init()
}

// Update handles messages.
func (v *SprintFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	cmd := v.form.Update(msg)
	if v.form.IsCancelled() {
		return func() tea.Msg {
			return tuicore.NavigateMsg{Action: tuicore.NavBack}
		}
	}
	if v.form.IsSubmitted() && !v.submitting {
		v.submitting = true
		return tea.Batch(
			v.form.CreateSprintFromForm(),
			func() tea.Msg { return tuicore.NavigateMsg{Action: tuicore.NavBack} },
		)
	}
	return cmd
}

// Render renders the form view.
func (v *SprintFormView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	v.form.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
	content := v.form.View()
	footer := tuicore.FormFooter("tab/shift+tab navigate · enter confirm · esc cancel", v.form.Errors())
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *SprintFormView) Title() string {
	return "◷  New Sprint"
}

// Bindings returns keybindings for this view.
func (v *SprintFormView) Bindings() []tuicore.Binding {
	return nil
}

// ViewName returns the view identifier.
func (v *SprintFormView) ViewName() string {
	return "pm.sprint_form"
}

// IsInputActive returns true (form always captures input).
func (v *SprintFormView) IsInputActive() bool {
	return true
}

// UpdateSprintFromForm updates an existing sprint from form data.
func (f *SprintForm) UpdateSprintFromForm() tea.Cmd {
	data := f.data
	workdir := f.workdir
	sprintID := f.sprintID
	return func() tea.Msg {
		state := pm.SprintState(data.State)
		opts := pm.UpdateSprintOptions{
			Title: &data.Title,
			Body:  &data.Body,
			State: &state,
		}
		if data.Start != "" {
			if t, err := time.Parse("2006-01-02", data.Start); err == nil {
				opts.Start = &t
			}
		}
		if data.End != "" {
			if t, err := time.Parse("2006-01-02", data.End); err == nil {
				opts.End = &t
			}
		}
		result := pm.UpdateSprint(workdir, sprintID, opts)
		if !result.Success {
			return SprintUpdatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return SprintUpdatedMsg{Sprint: result.Data}
	}
}

// SprintUpdatedMsg signals that a sprint was updated.
type SprintUpdatedMsg struct {
	Sprint pm.Sprint
	Err    error
}

// SprintEditFormView wraps the form for editing an existing sprint.
type SprintEditFormView struct {
	workdir    string
	sprintID   string
	form       *SprintForm
	sprint     *pm.Sprint
	loaded     bool
	submitting bool
	width      int
	height     int
}

// NewSprintEditFormView creates a new sprint edit form view.
func NewSprintEditFormView(workdir string) *SprintEditFormView {
	return &SprintEditFormView{
		workdir: workdir,
	}
}

// SetSize sets the view dimensions.
func (v *SprintEditFormView) SetSize(w, h int) {
	v.width = w
	v.height = h
	if v.form != nil {
		v.form.SetSize(w, h)
	}
}

// Activate loads the sprint and initializes the form.
func (v *SprintEditFormView) Activate(state *tuicore.State) tea.Cmd {
	v.sprintID = state.Router.Location().Param("sprintID")
	v.loaded = false
	v.form = nil
	return v.loadSprint()
}

func (v *SprintEditFormView) loadSprint() tea.Cmd {
	sprintID := v.sprintID
	return func() tea.Msg {
		result := pm.GetSprint(sprintID)
		if !result.Success {
			return SprintEditFormLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return SprintEditFormLoadedMsg{Sprint: &result.Data}
	}
}

// SprintEditFormLoadedMsg signals that the sprint for editing has been loaded.
type SprintEditFormLoadedMsg struct {
	Sprint *pm.Sprint
	Err    error
}

// Update handles messages.
func (v *SprintEditFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case SprintEditFormLoadedMsg:
		if msg.Err != nil {
			return func() tea.Msg {
				return tuicore.NavigateMsg{Action: tuicore.NavBack}
			}
		}
		v.sprint = msg.Sprint
		v.form = NewSprintEditForm(v.workdir, *v.sprint)
		v.form.SetSize(v.width, v.height)
		v.loaded = true
		return v.form.Init()
	}

	if !v.loaded || v.form == nil {
		return nil
	}

	cmd := v.form.Update(msg)

	if v.form.IsCancelled() {
		return func() tea.Msg {
			return tuicore.NavigateMsg{Action: tuicore.NavBack}
		}
	}

	if v.form.IsSubmitted() && !v.submitting {
		v.submitting = true
		return v.form.UpdateSprintFromForm()
	}

	return cmd
}

// Render renders the edit form view.
func (v *SprintEditFormView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)

	var content string
	if !v.loaded {
		content = "Loading sprint..."
	} else if v.form == nil {
		content = tuicore.Dim.Render("  Sprint not found")
	} else {
		v.form.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
		content = v.form.View()
	}

	var footer string
	if v.form != nil {
		footer = tuicore.FormFooter("tab/shift+tab navigate · enter confirm · esc cancel", v.form.Errors())
	} else {
		footer = tuicore.Dim.Render("tab/shift+tab navigate · enter confirm · esc cancel")
	}
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *SprintEditFormView) Title() string {
	if v.sprint != nil {
		return fmt.Sprintf("◷  Edit: %s", v.sprint.Title)
	}
	return "◷  Edit Sprint"
}

// Bindings returns keybindings for this view.
func (v *SprintEditFormView) Bindings() []tuicore.Binding {
	return nil
}

// ViewName returns the view identifier.
func (v *SprintEditFormView) ViewName() string {
	return "pm.sprint_edit_form"
}

// IsInputActive returns true (form always captures input).
func (v *SprintEditFormView) IsInputActive() bool {
	return true
}
