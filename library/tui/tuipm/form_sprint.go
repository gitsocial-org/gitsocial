// form_sprint.go - Sprint creation form using Huh
package tuipm

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// sprintFormData holds the form field values.
type sprintFormData struct {
	Title  string
	Body   string
	State  string
	Start  string
	End    string
	Labels []string
}

// sprintForm wraps a Huh form for sprint creation/editing.
type sprintForm struct {
	tuicore.FormBase
	workdir       string
	sprintID      string // Non-empty for edit mode
	bodyField     *huh.Text
	bodyOtherRows int // count of non-body field rows, for body sizing
	data          sprintFormData
	width         int
	height        int
}

// newSprintForm creates a new sprint form.
func newSprintForm(workdir string) *sprintForm {
	f := &sprintForm{workdir: workdir}
	f.buildForm()
	return f
}

// newSprintEditForm creates a form pre-filled with sprint data.
func newSprintEditForm(workdir string, sprint pm.Sprint) *sprintForm {
	f := &sprintForm{
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

// buildForm constructs the Huh form.
func (f *sprintForm) buildForm() {
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
func (f *sprintForm) SetSize(w, h int) {
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
func (f *sprintForm) Update(msg tea.Msg) tea.Cmd { return f.UpdateForm(msg) }

// Body returns the current body text (for the $EDITOR escape-hatch).
func (f *sprintForm) Body() string { return f.data.Body }

// SetBody writes the body and rebuilds the form so huh.Text refreshes.
func (f *sprintForm) SetBody(s string) {
	f.data.Body = s
	f.buildForm()
}

// Reset rebuilds the form, clearing huh-internal state while preserving data.
func (f *sprintForm) Reset() { f.buildForm() }

// sprintCreatedMsg signals that a sprint was created.
type sprintCreatedMsg struct {
	Sprint pm.Sprint
	Err    error
}

// createSprintFromForm creates a sprint from form data.
func (f *sprintForm) createSprintFromForm() tea.Cmd {
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
			return sprintCreatedMsg{Err: fmt.Errorf("%s", result.Error.Text())}
		}
		return sprintCreatedMsg{Sprint: result.Data}
	}
}

// sprintFormView wraps the form for integration with the TUI host.
type sprintFormView struct {
	tuicore.FormViewBase
}

// newSprintFormView creates a new sprint form view.
func newSprintFormView(workdir string) *sprintFormView {
	v := &sprintFormView{}
	v.AttachForm(newSprintForm(workdir))
	return v
}

// Activate initializes the form view.
func (v *sprintFormView) Activate(state *tuicore.State) tea.Cmd {
	form := newSprintForm(state.Workdir)
	v.AttachForm(form)
	return form.Init()
}

// Update handles messages.
func (v *sprintFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if m, ok := msg.(sprintCreatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*sprintForm); ok {
			return form.createSprintFromForm()
		}
		return nil
	})
}

// Render renders the form view.
func (v *sprintFormView) Render(state *tuicore.State) string {
	return v.RenderForm(state)
}

// Title returns the view title.
func (v *sprintFormView) Title() string { return "◷  New Sprint" }

// updateSprintFromForm updates an existing sprint from form data.
func (f *sprintForm) updateSprintFromForm() tea.Cmd {
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
			return sprintUpdatedMsg{Err: fmt.Errorf("%s", result.Error.Text())}
		}
		return sprintUpdatedMsg{Sprint: result.Data}
	}
}

// sprintUpdatedMsg signals that a sprint was updated.
type sprintUpdatedMsg struct {
	Sprint pm.Sprint
	Err    error
}

// sprintEditFormView wraps the form for editing an existing sprint.
type sprintEditFormView struct {
	tuicore.FormViewBase
	workdir  string
	sprintID string
	sprint   *pm.Sprint
	loaded   bool
}

// newSprintEditFormView creates a new sprint edit form view.
func newSprintEditFormView(workdir string) *sprintEditFormView {
	return &sprintEditFormView{
		workdir: workdir,
	}
}

// Activate loads the sprint and initializes the form.
func (v *sprintEditFormView) Activate(state *tuicore.State) tea.Cmd {
	v.sprintID = state.Router.Location().Param("sprintID")
	v.loaded = false
	v.DetachForm()
	return v.loadSprint()
}

// loadSprint fetches the sprint the form edits.
func (v *sprintEditFormView) loadSprint() tea.Cmd {
	sprintID := v.sprintID
	return func() tea.Msg {
		result := pm.GetSprint(sprintID)
		if !result.Success {
			return sprintEditFormLoadedMsg{Err: fmt.Errorf("%s", result.Error.Text())}
		}
		return sprintEditFormLoadedMsg{Sprint: &result.Data}
	}
}

// sprintEditFormLoadedMsg signals that the sprint for editing has been loaded.
type sprintEditFormLoadedMsg struct {
	Sprint *pm.Sprint
	Err    error
}

// Update handles messages.
func (v *sprintEditFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case sprintEditFormLoadedMsg:
		if msg.Err != nil {
			state.SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
			return func() tea.Msg {
				return tuicore.NavigateMsg{Action: tuicore.NavBack}
			}
		}
		v.sprint = msg.Sprint
		form := newSprintEditForm(v.workdir, *v.sprint)
		v.AttachForm(form)
		v.loaded = true
		return form.Init()
	}

	if !v.loaded {
		return nil
	}

	if m, ok := msg.(sprintUpdatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*sprintForm); ok {
			return form.updateSprintFromForm()
		}
		return nil
	})
}

// Render renders the edit form view.
func (v *sprintEditFormView) Render(state *tuicore.State) string {
	if !v.loaded {
		wrapper := tuicore.NewViewWrapper(state)
		footer := tuicore.FormFooter(true, nil)
		return wrapper.Render("Loading sprint...", footer)
	}
	return v.RenderForm(state)
}

// Title returns the view title.
func (v *sprintEditFormView) Title() string {
	if v.sprint != nil {
		return fmt.Sprintf("◷  Edit: %s", v.sprint.Title)
	}
	return "◷  Edit Sprint"
}
