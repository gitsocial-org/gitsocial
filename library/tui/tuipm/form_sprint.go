// sprint_form.go - Sprint creation form using Huh
package tuipm

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// SprintFormData holds the form field values.
type SprintFormData struct {
	Title  string
	Body   string
	State  string
	Start  string
	End    string
	Labels []string
}

// SprintForm wraps a Huh form for sprint creation/editing.
type SprintForm struct {
	tuicore.FormBase
	workdir       string
	sprintID      string // Non-empty for edit mode
	bodyField     *huh.Text
	bodyOtherRows int // count of non-body field rows, for body sizing
	data          SprintFormData
	width         int
	height        int
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
	f.data.Labels = append([]string(nil), sprint.Labels...)
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
			Title(pad(tuicore.RequiredLabel("Subject"))).
			Placeholder("Sprint subject...").
			Value(&f.data.Title).
			Inline(true).
			Validate(func(s string) error {
				if s == "" {
					return fmt.Errorf("subject is required")
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
	fields = append(fields, f.bodyField, tuicore.NewLabelsField(&f.data.Labels, ""), tuicore.NewSubmitField())
	f.bodyOtherRows = len(fields)
	f.SetForm(huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(tuicore.FormKeyMap()))
}

// SetSize sets the form dimensions.
func (f *SprintForm) SetSize(w, h int) {
	f.width = w
	f.height = h
	if form := f.FormPtr(); form != nil {
		form.WithWidth(w).WithHeight(h + 1)
		if f.bodyField != nil {
			f.bodyField.WithHeight(tuicore.BodyHeight(h, f.bodyOtherRows))
		}
	}
}

// Update delegates the standard form lifecycle to FormBase.
func (f *SprintForm) Update(msg tea.Msg) tea.Cmd { return f.UpdateForm(msg) }

// Body returns the current body text (for the $EDITOR escape-hatch).
func (f *SprintForm) Body() string { return f.data.Body }

// SetBody writes the body and rebuilds the form so huh.Text refreshes.
func (f *SprintForm) SetBody(s string) {
	f.data.Body = s
	f.buildForm()
}

// Reset rebuilds the form, clearing huh-internal state while preserving data.
func (f *SprintForm) Reset() { f.buildForm() }

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
			State:  pm.SprintState(data.State),
			Labels: data.Labels,
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
	tuicore.FormViewBase
}

// NewSprintFormView creates a new sprint form view.
func NewSprintFormView(workdir string) *SprintFormView {
	v := &SprintFormView{}
	v.AttachForm(NewSprintForm(workdir))
	return v
}

// Activate initializes the form view.
func (v *SprintFormView) Activate(state *tuicore.State) tea.Cmd {
	form := NewSprintForm(state.Workdir)
	v.AttachForm(form)
	return form.Init()
}

// Update handles messages.
func (v *SprintFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if m, ok := msg.(SprintCreatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*SprintForm); ok {
			return form.CreateSprintFromForm()
		}
		return nil
	})
}

// Render renders the form view.
func (v *SprintFormView) Render(state *tuicore.State) string {
	return v.RenderForm(state)
}

// Title returns the view title.
func (v *SprintFormView) Title() string { return "◷  New Sprint" }

// ViewName returns the view identifier.
func (v *SprintFormView) ViewName() string { return "pm.sprint_form" }

// UpdateSprintFromForm updates an existing sprint from form data.
func (f *SprintForm) UpdateSprintFromForm() tea.Cmd {
	data := f.data
	workdir := f.workdir
	sprintID := f.sprintID
	return func() tea.Msg {
		state := pm.SprintState(data.State)
		labels := data.Labels
		opts := pm.UpdateSprintOptions{
			Title:  &data.Title,
			Body:   &data.Body,
			State:  &state,
			Labels: &labels,
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
	tuicore.FormViewBase
	workdir  string
	sprintID string
	sprint   *pm.Sprint
	loaded   bool
}

// NewSprintEditFormView creates a new sprint edit form view.
func NewSprintEditFormView(workdir string) *SprintEditFormView {
	return &SprintEditFormView{
		workdir: workdir,
	}
}

// Activate loads the sprint and initializes the form.
func (v *SprintEditFormView) Activate(state *tuicore.State) tea.Cmd {
	v.sprintID = state.Router.Location().Param("sprintID")
	v.loaded = false
	v.DetachForm()
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
		form := NewSprintEditForm(v.workdir, *v.sprint)
		v.AttachForm(form)
		v.loaded = true
		return form.Init()
	}

	if !v.loaded {
		return nil
	}

	if m, ok := msg.(SprintUpdatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*SprintForm); ok {
			return form.UpdateSprintFromForm()
		}
		return nil
	})
}

// Render renders the edit form view.
func (v *SprintEditFormView) Render(state *tuicore.State) string {
	if !v.loaded {
		wrapper := tuicore.NewViewWrapper(state)
		footer := tuicore.FormFooter(true, nil)
		return wrapper.Render("Loading sprint...", footer)
	}
	return v.RenderForm(state)
}

// Title returns the view title.
func (v *SprintEditFormView) Title() string {
	if v.sprint != nil {
		return fmt.Sprintf("◷  Edit: %s", v.sprint.Title)
	}
	return "◷  Edit Sprint"
}

// ViewName returns the view identifier.
func (v *SprintEditFormView) ViewName() string { return "pm.sprint_edit_form" }
