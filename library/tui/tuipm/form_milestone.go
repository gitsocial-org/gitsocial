// milestone_form.go - Milestone creation form using Huh
package tuipm

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// MilestoneFormData holds the form field values.
type MilestoneFormData struct {
	Title  string
	Body   string
	State  string
	Due    string
	Labels []string
}

// MilestoneForm wraps a Huh form for milestone creation/editing.
type MilestoneForm struct {
	tuicore.FormBase
	workdir       string
	milestoneID   string // Non-empty for edit mode
	bodyField     *huh.Text
	bodyOtherRows int // count of non-body field rows, for body sizing
	data          MilestoneFormData
	width         int
	height        int
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
	f.data.Labels = append([]string(nil), milestone.Labels...)
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
			Title(pad(tuicore.RequiredLabel("Subject"))).
			Placeholder("Milestone subject...").
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
	fields = append(fields, f.bodyField, tuicore.NewLabelsField(&f.data.Labels, ""), tuicore.NewSubmitField())
	f.bodyOtherRows = len(fields)
	f.SetForm(huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(tuicore.FormTheme()).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(tuicore.FormKeyMap()))
}

// SetSize sets the form dimensions.
func (f *MilestoneForm) SetSize(w, h int) {
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
func (f *MilestoneForm) Update(msg tea.Msg) tea.Cmd { return f.UpdateForm(msg) }

// Body returns the current body text (for the $EDITOR escape-hatch).
func (f *MilestoneForm) Body() string { return f.data.Body }

// SetBody writes the body and rebuilds the form so huh.Text refreshes.
func (f *MilestoneForm) SetBody(s string) {
	f.data.Body = s
	f.buildForm()
}

// Reset rebuilds the form, clearing huh-internal state while preserving data.
func (f *MilestoneForm) Reset() { f.buildForm() }

// CreateMilestoneFromForm creates a milestone from form data.
func (f *MilestoneForm) CreateMilestoneFromForm() tea.Cmd {
	data := f.data
	workdir := f.workdir
	return func() tea.Msg {
		opts := pm.CreateMilestoneOptions{
			State:  pm.State(data.State),
			Labels: data.Labels,
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
	tuicore.FormViewBase
}

// NewMilestoneFormView creates a new milestone form view.
func NewMilestoneFormView(workdir string) *MilestoneFormView {
	v := &MilestoneFormView{}
	v.AttachForm(NewMilestoneForm(workdir))
	return v
}

// Activate initializes the form view.
func (v *MilestoneFormView) Activate(state *tuicore.State) tea.Cmd {
	form := NewMilestoneForm(state.Workdir)
	v.AttachForm(form)
	return form.Init()
}

// Update handles messages.
func (v *MilestoneFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if m, ok := msg.(MilestoneCreatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*MilestoneForm); ok {
			return form.CreateMilestoneFromForm()
		}
		return nil
	})
}

// Render renders the form view.
func (v *MilestoneFormView) Render(state *tuicore.State) string {
	return v.RenderForm(state)
}

// Title returns the view title.
func (v *MilestoneFormView) Title() string { return "◇  New Milestone" }

// ViewName returns the view identifier.
func (v *MilestoneFormView) ViewName() string { return "pm.milestone_form" }

// UpdateMilestoneFromForm updates an existing milestone from form data.
func (f *MilestoneForm) UpdateMilestoneFromForm() tea.Cmd {
	data := f.data
	workdir := f.workdir
	milestoneID := f.milestoneID
	return func() tea.Msg {
		state := pm.State(data.State)
		labels := data.Labels
		opts := pm.UpdateMilestoneOptions{
			Title:  &data.Title,
			Body:   &data.Body,
			State:  &state,
			Labels: &labels,
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
	tuicore.FormViewBase
	workdir     string
	milestoneID string
	milestone   *pm.Milestone
	loaded      bool
}

// NewMilestoneEditFormView creates a new milestone edit form view.
func NewMilestoneEditFormView(workdir string) *MilestoneEditFormView {
	return &MilestoneEditFormView{
		workdir: workdir,
	}
}

// Activate loads the milestone and initializes the form.
func (v *MilestoneEditFormView) Activate(state *tuicore.State) tea.Cmd {
	v.milestoneID = state.Router.Location().Param("milestoneID")
	v.loaded = false
	v.DetachForm()
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
		form := NewMilestoneEditForm(v.workdir, *v.milestone)
		v.AttachForm(form)
		v.loaded = true
		return form.Init()
	}

	if !v.loaded {
		return nil
	}

	if m, ok := msg.(MilestoneUpdatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*MilestoneForm); ok {
			return form.UpdateMilestoneFromForm()
		}
		return nil
	})
}

// Render renders the edit form view.
func (v *MilestoneEditFormView) Render(state *tuicore.State) string {
	if !v.loaded {
		wrapper := tuicore.NewViewWrapper(state)
		footer := tuicore.FormFooter(true, nil)
		return wrapper.Render("Loading milestone...", footer)
	}
	return v.RenderForm(state)
}

// Title returns the view title.
func (v *MilestoneEditFormView) Title() string {
	if v.milestone != nil {
		return fmt.Sprintf("◇  Edit: %s", v.milestone.Title)
	}
	return "◇  Edit Milestone"
}

// ViewName returns the view identifier.
func (v *MilestoneEditFormView) ViewName() string { return "pm.milestone_edit_form" }
