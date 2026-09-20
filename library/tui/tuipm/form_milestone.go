// form_milestone.go - Milestone creation form using Huh
package tuipm

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// milestoneFormData holds the form field values.
type milestoneFormData struct {
	Title  string
	Body   string
	State  string
	Due    string
	Labels []string
}

// milestoneForm wraps a Huh form for milestone creation/editing.
type milestoneForm struct {
	tuicore.FormBase
	workdir       string
	milestoneID   string // Non-empty for edit mode
	bodyField     *huh.Text
	bodyOtherRows int // count of non-body field rows, for body sizing
	data          milestoneFormData
	width         int
	height        int
}

// newMilestoneForm creates a new milestone form.
func newMilestoneForm(workdir string) *milestoneForm {
	f := &milestoneForm{workdir: workdir}
	f.buildForm()
	return f
}

// newMilestoneEditForm creates a form pre-filled with milestone data.
func newMilestoneEditForm(workdir string, milestone pm.Milestone) *milestoneForm {
	f := &milestoneForm{
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

// buildForm constructs the Huh form.
func (f *milestoneForm) buildForm() {
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
func (f *milestoneForm) SetSize(w, h int) {
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
func (f *milestoneForm) Update(msg tea.Msg) tea.Cmd { return f.UpdateForm(msg) }

// Body returns the current body text (for the $EDITOR escape-hatch).
func (f *milestoneForm) Body() string { return f.data.Body }

// SetBody writes the body and rebuilds the form so huh.Text refreshes.
func (f *milestoneForm) SetBody(s string) {
	f.data.Body = s
	f.buildForm()
}

// Reset rebuilds the form, clearing huh-internal state while preserving data.
func (f *milestoneForm) Reset() { f.buildForm() }

// createMilestoneFromForm creates a milestone from form data.
func (f *milestoneForm) createMilestoneFromForm() tea.Cmd {
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
			return milestoneCreatedMsg{Err: fmt.Errorf("%s", result.Error.Text())}
		}
		return milestoneCreatedMsg{Milestone: result.Data}
	}
}

// milestoneFormView wraps the form for integration with the TUI host.
type milestoneFormView struct {
	tuicore.FormViewBase
}

// newMilestoneFormView creates a new milestone form view.
func newMilestoneFormView(workdir string) *milestoneFormView {
	v := &milestoneFormView{}
	v.AttachForm(newMilestoneForm(workdir))
	return v
}

// Activate initializes the form view.
func (v *milestoneFormView) Activate(state *tuicore.State) tea.Cmd {
	form := newMilestoneForm(state.Workdir)
	v.AttachForm(form)
	return form.Init()
}

// Update handles messages.
func (v *milestoneFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if m, ok := msg.(milestoneCreatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*milestoneForm); ok {
			return form.createMilestoneFromForm()
		}
		return nil
	})
}

// Render renders the form view.
func (v *milestoneFormView) Render(state *tuicore.State) string {
	return v.RenderForm(state)
}

// Title returns the view title.
func (v *milestoneFormView) Title() string { return "◇  New Milestone" }

// updateMilestoneFromForm updates an existing milestone from form data.
func (f *milestoneForm) updateMilestoneFromForm() tea.Cmd {
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
			return milestoneUpdatedMsg{Err: fmt.Errorf("%s", result.Error.Text())}
		}
		return milestoneUpdatedMsg{Milestone: result.Data}
	}
}

// milestoneUpdatedMsg signals that a milestone was updated.
type milestoneUpdatedMsg struct {
	Milestone pm.Milestone
	Err       error
}

// milestoneEditFormView wraps the form for editing an existing milestone.
type milestoneEditFormView struct {
	tuicore.FormViewBase
	workdir     string
	milestoneID string
	milestone   *pm.Milestone
	loaded      bool
}

// newMilestoneEditFormView creates a new milestone edit form view.
func newMilestoneEditFormView(workdir string) *milestoneEditFormView {
	return &milestoneEditFormView{
		workdir: workdir,
	}
}

// Activate loads the milestone and initializes the form.
func (v *milestoneEditFormView) Activate(state *tuicore.State) tea.Cmd {
	v.milestoneID = state.Router.Location().Param("milestoneID")
	v.loaded = false
	v.DetachForm()
	return v.loadMilestone()
}

// loadMilestone fetches the milestone the form edits.
func (v *milestoneEditFormView) loadMilestone() tea.Cmd {
	milestoneID := v.milestoneID
	return func() tea.Msg {
		result := pm.GetMilestone(milestoneID)
		if !result.Success {
			return milestoneEditFormLoadedMsg{Err: fmt.Errorf("%s", result.Error.Text())}
		}
		return milestoneEditFormLoadedMsg{Milestone: &result.Data}
	}
}

// milestoneEditFormLoadedMsg signals that the milestone for editing has been loaded.
type milestoneEditFormLoadedMsg struct {
	Milestone *pm.Milestone
	Err       error
}

// Update handles messages.
func (v *milestoneEditFormView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case milestoneEditFormLoadedMsg:
		if msg.Err != nil {
			state.SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
			return func() tea.Msg {
				return tuicore.NavigateMsg{Action: tuicore.NavBack}
			}
		}
		v.milestone = msg.Milestone
		form := newMilestoneEditForm(v.workdir, *v.milestone)
		v.AttachForm(form)
		v.loaded = true
		return form.Init()
	}

	if !v.loaded {
		return nil
	}

	if m, ok := msg.(milestoneUpdatedMsg); ok && m.Err != nil {
		v.ClearSubmitting()
	}
	return v.UpdateForm(msg, func() tea.Cmd {
		if form, ok := v.CurrentForm().(*milestoneForm); ok {
			return form.updateMilestoneFromForm()
		}
		return nil
	})
}

// Render renders the edit form view.
func (v *milestoneEditFormView) Render(state *tuicore.State) string {
	if !v.loaded {
		wrapper := tuicore.NewViewWrapper(state)
		footer := tuicore.FormFooter(true, nil)
		return wrapper.Render("Loading milestone...", footer)
	}
	return v.RenderForm(state)
}

// Title returns the view title.
func (v *milestoneEditFormView) Title() string {
	if v.milestone != nil {
		return fmt.Sprintf("◇  Edit: %s", v.milestone.Title)
	}
	return "◇  Edit Milestone"
}
