// milestone_form.go - Milestone creation form using Huh
package tuipm

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// MilestoneFormData holds the form field values.
type MilestoneFormData struct {
	Title string
	Body  string
	State string
	Due   string
}

// MilestoneForm wraps a Huh form for milestone creation/editing.
type MilestoneForm struct {
	workdir     string
	milestoneID string // Non-empty for edit mode
	form        *huh.Form
	bodyField   *huh.Text
	data        MilestoneFormData
	width       int
	height      int
	submitted   bool
	canceled    bool
}

// NewMilestoneForm creates a new milestone form.
func NewMilestoneForm(workdir string) *MilestoneForm {
	f := &MilestoneForm{workdir: workdir}
	f.buildForm()
	return f
}

// NewMilestoneEditForm creates a form pre-filled with milestone data.
func NewMilestoneEditForm(workdir string, milestone pm.Milestone) *MilestoneForm {
	f := &MilestoneForm{
		workdir:     workdir,
		milestoneID: milestone.ID,
	}
	f.data.Title = milestone.Title
	f.data.Body = milestone.Body
	f.data.State = string(milestone.State)
	if milestone.Due != nil {
		f.data.Due = milestone.Due.Format("2006-01-02")
	}
	f.buildForm()
	return f
}

// IsEditMode returns true if this is an edit form.
func (f *MilestoneForm) IsEditMode() bool {
	return f.milestoneID != ""
}

// buildForm constructs the Huh form.
func (f *MilestoneForm) buildForm() {
	pad := tuicore.PadLabel
	fields := make([]huh.Field, 0, 5)
	fields = append(fields,
		huh.NewInput().
			Key("title").
			Title(pad(tuicore.RequiredLabel("Title"))).
			Placeholder("Milestone title...").
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
				tuicore.CycleOption{Label: "open", Value: "open"},
				tuicore.CycleOption{Label: "closed", Value: "closed"},
				tuicore.CycleOption{Label: "canceled", Value: "canceled"},
			).
			Value(&f.data.State),
		huh.NewInput().
			Key("due").
			Title(pad("Due Date")).
			Placeholder("YYYY-MM-DD (optional)").
			Value(&f.data.Due).
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
func (f *MilestoneForm) SetSize(w, h int) {
	f.width = w
	f.height = h
	if f.form != nil {
		f.form.WithWidth(w).WithHeight(h - 2)
		if f.bodyField != nil {
			f.bodyField.WithHeight(max(5, h-6))
		}
	}
}

// Init initializes the form.
func (f *MilestoneForm) Init() tea.Cmd {
	return f.form.Init()
}

// Update handles form messages.
func (f *MilestoneForm) Update(msg tea.Msg) tea.Cmd {
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
func (f *MilestoneForm) View() string {
	return f.form.View()
}

// Errors returns the form's current validation errors.
func (f *MilestoneForm) Errors() []error { return f.form.Errors() }

// IsSubmitted returns true if form was submitted.
func (f *MilestoneForm) IsSubmitted() bool {
	return f.submitted
}

// IsCancelled returns true if form was canceled.
func (f *MilestoneForm) IsCancelled() bool {
	return f.canceled
}

// CreateMilestoneFromForm creates a milestone from form data.
func (f *MilestoneForm) CreateMilestoneFromForm() tea.Cmd {
	data := f.data
	workdir := f.workdir
	return func() tea.Msg {
		opts := pm.CreateMilestoneOptions{
			State: pm.State(data.State),
		}
		if data.Due != "" {
			if t, err := time.Parse("2006-01-02", data.Due); err == nil {
				opts.Due = &t
			}
		}
		result := pm.CreateMilestone(workdir, data.Title, data.Body, opts)
		if !result.Success {
			return MilestoneCreatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return MilestoneCreatedMsg{Milestone: result.Data}
	}
}

// MilestoneFormView wraps the form for integration with the TUI host.
type MilestoneFormView struct {
	form       *MilestoneForm
	width      int
	height     int
	submitting bool
}

// NewMilestoneFormView creates a new milestone form view.
func NewMilestoneFormView(workdir string) *MilestoneFormView {
	return &MilestoneFormView{
		form: NewMilestoneForm(workdir),
	}
}

// SetSize sets the view dimensions.
func (v *MilestoneFormView) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.form.SetSize(w, h)
}

// Activate initializes the form view.
func (v *MilestoneFormView) Activate(state *tuicore.State) tea.Cmd {
	v.form = NewMilestoneForm(state.Workdir)
	return v.form.Init()
}

// Update handles messages.
func (v *MilestoneFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	cmd := v.form.Update(msg)
	if v.form.IsCancelled() {
		return func() tea.Msg {
			return tuicore.NavigateMsg{Action: tuicore.NavBack}
		}
	}
	if v.form.IsSubmitted() && !v.submitting {
		v.submitting = true
		return tea.Batch(
			v.form.CreateMilestoneFromForm(),
			func() tea.Msg { return tuicore.NavigateMsg{Action: tuicore.NavBack} },
		)
	}
	return cmd
}

// Render renders the form view.
func (v *MilestoneFormView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	v.form.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())
	content := v.form.View()
	footer := tuicore.FormFooter("tab/shift+tab navigate · enter confirm · esc cancel", v.form.Errors())
	return wrapper.Render(content, footer)
}

// Title returns the view title.
func (v *MilestoneFormView) Title() string {
	return "◇  New Milestone"
}

// Bindings returns keybindings for this view.
func (v *MilestoneFormView) Bindings() []tuicore.Binding {
	return nil
}

// ViewName returns the view identifier.
func (v *MilestoneFormView) ViewName() string {
	return "pm.milestone_form"
}

// IsInputActive returns true (form always captures input).
func (v *MilestoneFormView) IsInputActive() bool {
	return true
}

// UpdateMilestoneFromForm updates an existing milestone from form data.
func (f *MilestoneForm) UpdateMilestoneFromForm() tea.Cmd {
	data := f.data
	workdir := f.workdir
	milestoneID := f.milestoneID
	return func() tea.Msg {
		state := pm.State(data.State)
		opts := pm.UpdateMilestoneOptions{
			Title: &data.Title,
			Body:  &data.Body,
			State: &state,
		}
		if data.Due != "" {
			if t, err := time.Parse("2006-01-02", data.Due); err == nil {
				opts.Due = &t
			}
		}
		result := pm.UpdateMilestone(workdir, milestoneID, opts)
		if !result.Success {
			return MilestoneUpdatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return MilestoneUpdatedMsg{Milestone: result.Data}
	}
}

// MilestoneUpdatedMsg signals that a milestone was updated.
type MilestoneUpdatedMsg struct {
	Milestone pm.Milestone
	Err       error
}

// MilestoneEditFormView wraps the form for editing an existing milestone.
type MilestoneEditFormView struct {
	workdir     string
	milestoneID string
	form        *MilestoneForm
	milestone   *pm.Milestone
	loaded      bool
	submitting  bool
	width       int
	height      int
}

// NewMilestoneEditFormView creates a new milestone edit form view.
func NewMilestoneEditFormView(workdir string) *MilestoneEditFormView {
	return &MilestoneEditFormView{
		workdir: workdir,
	}
}

// SetSize sets the view dimensions.
func (v *MilestoneEditFormView) SetSize(w, h int) {
	v.width = w
	v.height = h
	if v.form != nil {
		v.form.SetSize(w, h)
	}
}

// Activate loads the milestone and initializes the form.
func (v *MilestoneEditFormView) Activate(state *tuicore.State) tea.Cmd {
	v.milestoneID = state.Router.Location().Param("milestoneID")
	v.loaded = false
	v.form = nil
	return v.loadMilestone()
}

func (v *MilestoneEditFormView) loadMilestone() tea.Cmd {
	milestoneID := v.milestoneID
	return func() tea.Msg {
		result := pm.GetMilestone(milestoneID)
		if !result.Success {
			return MilestoneEditFormLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return MilestoneEditFormLoadedMsg{Milestone: &result.Data}
	}
}

// MilestoneEditFormLoadedMsg signals that the milestone for editing has been loaded.
type MilestoneEditFormLoadedMsg struct {
	Milestone *pm.Milestone
	Err       error
}

// Update handles messages.
func (v *MilestoneEditFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case MilestoneEditFormLoadedMsg:
		if msg.Err != nil {
			return func() tea.Msg {
				return tuicore.NavigateMsg{Action: tuicore.NavBack}
			}
		}
		v.milestone = msg.Milestone
		v.form = NewMilestoneEditForm(v.workdir, *v.milestone)
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
		return v.form.UpdateMilestoneFromForm()
	}

	return cmd
}

// Render renders the edit form view.
func (v *MilestoneEditFormView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)

	var content string
	if !v.loaded {
		content = "Loading milestone..."
	} else if v.form == nil {
		content = tuicore.Dim.Render("  Milestone not found")
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
func (v *MilestoneEditFormView) Title() string {
	if v.milestone != nil {
		return fmt.Sprintf("◇  Edit: %s", v.milestone.Title)
	}
	return "◇  Edit Milestone"
}

// Bindings returns keybindings for this view.
func (v *MilestoneEditFormView) Bindings() []tuicore.Binding {
	return nil
}

// ViewName returns the view identifier.
func (v *MilestoneEditFormView) ViewName() string {
	return "pm.milestone_edit_form"
}

// IsInputActive returns true (form always captures input).
func (v *MilestoneEditFormView) IsInputActive() bool {
	return true
}
